package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	_ "time/tzdata"

	"github.com/robfig/cron/v3"
	"modernc.org/sqlite"
)

// NamedJob stores the same command JSON accepted by the MQTT run topic.
type NamedJob struct {
	Name     string          `json:"name"`
	Schedule string          `json:"schedule"`
	Payload  json.RawMessage `json:"payload"`
}

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func validateJob(j NamedJob) error {
	if strings.TrimSpace(j.Name) != j.Name || len(j.Name) > 128 || slug(j.Name) == "" || strings.ContainsAny(j.Name, "/\x00\r\n") {
		return errors.New("name must be 1–128 characters, contain letters or numbers, and have no slashes or surrounding whitespace")
	}
	if j.Schedule != "" {
		if _, err := cronParser.Parse(j.Schedule); err != nil {
			return fmt.Errorf("schedule: %w", err)
		}
	}
	var req RunRequest
	dec := json.NewDecoder(bytes.NewReader(j.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return fmt.Errorf("payload: %w", err)
	}
	if strings.TrimSpace(req.Command) == "" {
		return errors.New("payload must contain a command and optional command_args object")
	}
	if req.Name != "" {
		return errors.New("set the job name in the name field, not the payload")
	}
	return nil
}

// JobStore serializes durable writes and updates the in-process scheduler only
// after the new definitions have been saved successfully.
type JobStore struct {
	mu          sync.RWMutex
	db          *sql.DB
	jobs        map[string]NamedJob
	cron        *cron.Cron
	entries     map[string]cron.EntryID
	trigger     func(string) error
	runningJobs func() map[string][]int64
}

func OpenJobStore(path string, trigger func(string) error) (*JobStore, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	// Job payloads may contain credentials, so create the database privately.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	uri := &url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", uri.String()+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &JobStore{db: db, jobs: map[string]NamedJob{}, entries: map[string]cron.EntryID{}, trigger: trigger, cron: cron.New(cron.WithParser(cronParser))}
	if err := s.load(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *JobStore) load() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS jobs (
  name TEXT NOT NULL PRIMARY KEY,
  topic_slug TEXT NOT NULL UNIQUE,
  schedule TEXT NOT NULL,
  payload TEXT NOT NULL CHECK(json_valid(payload))
 )`); err != nil {
		return err
	}
	rows, err := s.db.Query("SELECT name, topic_slug, schedule, payload FROM jobs ORDER BY name")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var j NamedJob
		var topicSlug, payload string
		if err := rows.Scan(&j.Name, &topicSlug, &j.Schedule, &payload); err != nil {
			return err
		}
		j.Payload = json.RawMessage(payload)
		if err := validateJob(j); err != nil {
			return fmt.Errorf("stored job %q: %w", j.Name, err)
		}
		if topicSlug != slug(j.Name) {
			return fmt.Errorf("stored job %q has an invalid topic slug", j.Name)
		}
		s.jobs[j.Name] = j
		s.schedule(j)
	}
	return rows.Err()
}

// Close stops scheduled triggers before releasing the SQLite database.
func (s *JobStore) Close() error {
	<-s.cron.Stop().Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

func (s *JobStore) schedule(j NamedJob) {
	if j.Schedule == "" {
		return
	}
	id, _ := s.cron.AddFunc(j.Schedule, func() {
		if err := s.trigger(j.Name); err != nil {
			log.Printf("schedule %q: %v", j.Name, err)
		}
	})
	s.entries[j.Name] = id
}

func (s *JobStore) Get(name string) (NamedJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[name]
	return j, ok
}

func (s *JobStore) List() []NamedJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedJobs(s.jobs)
}

func sortedJobs(jobs map[string]NamedJob) []NamedJob {
	out := make([]NamedJob, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

var errNotFound = errors.New("job not found")
var errConflict = errors.New("a job with this name or status topic already exists")

func (s *JobStore) Save(j NamedJob, create bool) error {
	if err := validateJob(j); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.jobs[j.Name]
	if create && exists {
		return errConflict
	}
	if !create && !exists {
		return errNotFound
	}
	for name := range s.jobs {
		if name != j.Name && slug(name) == slug(j.Name) {
			return errConflict
		}
	}
	var err error
	if create {
		_, err = s.db.Exec("INSERT INTO jobs (name, topic_slug, schedule, payload) VALUES (?, ?, ?, ?)", j.Name, slug(j.Name), j.Schedule, string(j.Payload))
	} else {
		_, err = s.db.Exec("UPDATE jobs SET schedule = ?, payload = ? WHERE name = ?", j.Schedule, string(j.Payload), j.Name)
	}
	if err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code()&0xff == 19 {
			return errConflict
		}
		return err
	}
	if id, ok := s.entries[j.Name]; ok {
		s.cron.Remove(id)
		delete(s.entries, j.Name)
	}
	s.jobs[j.Name] = j
	s.schedule(j)
	return nil
}

func (s *JobStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[name]; !ok {
		return errNotFound
	}
	if _, err := s.db.Exec("DELETE FROM jobs WHERE name = ?", name); err != nil {
		return err
	}
	if id, ok := s.entries[name]; ok {
		s.cron.Remove(id)
		delete(s.entries, name)
	}
	delete(s.jobs, name)
	return nil
}

func (s *JobStore) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		type jobState struct {
			NamedJob
			RunningJobIDs []int64 `json:"running_job_ids"`
		}
		running := map[string][]int64{}
		if s.runningJobs != nil {
			running = s.runningJobs()
		}
		out := make([]jobState, 0)
		for _, j := range s.List() {
			ids := running[j.Name]
			if ids == nil {
				ids = []int64{}
			}
			out = append(out, jobState{NamedJob: j, RunningJobIDs: ids})
		}
		writeJSON(w, 200, out)
	})
	save := func(create bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var j NamedJob
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&j); err != nil {
				apiError(w, 400, err)
				return
			}
			if err := dec.Decode(new(any)); err != io.EOF {
				apiError(w, 400, errors.New("expected a single JSON object"))
				return
			}
			if !create && j.Name != r.PathValue("name") {
				apiError(w, 400, errors.New("job names cannot be changed"))
				return
			}
			if err := validateJob(j); err != nil {
				apiError(w, 400, err)
				return
			}
			if err := s.Save(j, create); err != nil {
				storeError(w, err)
				return
			}
			status := 200
			if create {
				status = 201
			}
			writeJSON(w, status, j)
		}
	}
	mux.HandleFunc("POST /api/jobs", save(true))
	mux.HandleFunc("PUT /api/jobs/{name}", save(false))
	mux.HandleFunc("DELETE /api/jobs/{name}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Delete(r.PathValue("name")); err != nil {
			storeError(w, err)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /api/jobs/{name}/run", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if _, ok := s.Get(name); !ok {
			apiError(w, 404, errNotFound)
			return
		}
		if err := s.trigger(name); err != nil {
			apiError(w, 503, err)
			return
		}
		writeJSON(w, 202, map[string]string{"state": "queued"})
	})
	return mux
}

func storeError(w http.ResponseWriter, err error) {
	status := 500
	if errors.Is(err, errNotFound) {
		status = 404
	}
	if errors.Is(err, errConflict) {
		status = 409
	}
	if status == 500 {
		log.Printf("jobs storage: %v", err)
		err = errors.New("could not save jobs")
	}
	apiError(w, status, err)
}
func apiError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	var b bytes.Buffer
	if err := json.NewEncoder(&b).Encode(value); err != nil {
		http.Error(w, "encoding response", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(b.Bytes())
}
