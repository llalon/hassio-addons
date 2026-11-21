package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

func sampleJob() NamedJob {
	return NamedJob{Name: "Documents Backup", Schedule: "CRON_TZ=America/Toronto 0 3 * * *", Payload: json.RawMessage(`{"command":"sync/copy","command_args":{"srcFs":"/share/docs","dstFs":"remote:backup"}}`)}
}

func TestJobStoreLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.db")
	var triggered string
	s, err := OpenJobStore(path, func(name string) error { triggered = name; return nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	j := sampleJob()
	if err := s.Save(j, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(j, true); !errors.Is(err, errConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	collision := j
	collision.Name = "documents-backup"
	if err := s.Save(collision, true); !errors.Is(err, errConflict) {
		t.Fatalf("slug collision: %v", err)
	}
	entry := s.cron.Entry(s.entries[j.Name])
	entry.Job.Run()
	if triggered != j.Name {
		t.Fatalf("cron triggered %q", triggered)
	}
	next := entry.Schedule.Next(time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC))
	if next.Hour() != 8 {
		t.Fatalf("timezone not applied: %v", next)
	}
	reloaded, err := OpenJobStore(path, func(string) error { return nil })
	if err != nil || len(reloaded.List()) != 1 || len(reloaded.cron.Entries()) != 1 {
		t.Fatalf("reload: %v", err)
	}
	if err := reloaded.Close(); err != nil {
		t.Fatal(err)
	}
	j.Schedule = ""
	if err := s.Save(j, false); err != nil {
		t.Fatal(err)
	}
	if len(s.cron.Entries()) != 0 {
		t.Fatal("schedule survived update")
	}
	j.Schedule = "*/5 * * * *"
	if err := s.Save(j, false); err != nil {
		t.Fatal(err)
	}
	if len(s.cron.Entries()) != 1 {
		t.Fatal("updated schedule missing")
	}
	if err := s.Delete(j.Name); err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 0 || len(s.cron.Entries()) != 0 {
		t.Fatal("delete left job or schedule")
	}
	reloaded, err = OpenJobStore(path, nil)
	if err != nil || len(reloaded.List()) != 0 {
		t.Fatalf("delete was not persisted: %v", err)
	}
	defer reloaded.Close()
}

func TestInvalidJobs(t *testing.T) {
	for _, mutate := range []func(*NamedJob){
		func(j *NamedJob) { j.Name = " " },
		func(j *NamedJob) { j.Name = "foo/bar" },
		func(j *NamedJob) { j.Schedule = "0 0 0 * * *" },
		func(j *NamedJob) { j.Schedule = "*/0 * * * *" },
		func(j *NamedJob) { j.Payload = json.RawMessage(`{"command":"sync/copy","command_args":[]}`) },
		func(j *NamedJob) { j.Payload = json.RawMessage(`{"command":"sync/copy","name":"other"}`) },
		func(j *NamedJob) { j.Payload = json.RawMessage(`{"command":"sync/copy","args":{}}`) },
		func(j *NamedJob) { j.Payload = json.RawMessage(`null`) },
	} {
		j := sampleJob()
		mutate(&j)
		if err := validateJob(j); err == nil {
			t.Errorf("accepted invalid job: %+v", j)
		}
	}
}

func TestFailedWritePreservesJobAndSchedule(t *testing.T) {
	s, _ := OpenJobStore(filepath.Join(t.TempDir(), "jobs.db"), nil)
	t.Cleanup(func() { _ = s.Close() })
	j := sampleJob()
	if err := s.Save(j, true); err != nil {
		t.Fatal(err)
	}
	// SQLite rejects writes while the connection is in query-only mode.
	if _, err := s.db.Exec("PRAGMA query_only = ON"); err != nil {
		t.Fatal(err)
	}
	j.Schedule = ""
	if err := s.Save(j, false); err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	if got, _ := s.Get(j.Name); got.Schedule == "" || len(s.cron.Entries()) != 1 {
		t.Fatal("failed write changed state")
	}
	if err := s.Delete(j.Name); err == nil {
		t.Fatal("delete unexpectedly succeeded")
	}
	if _, ok := s.Get(j.Name); !ok {
		t.Fatal("failed delete changed state")
	}
}

func TestJobsAPI(t *testing.T) {
	var triggered string
	var triggerErr error
	s, _ := OpenJobStore(filepath.Join(t.TempDir(), "jobs.db"), func(name string) error { triggered = name; return triggerErr })
	t.Cleanup(func() { _ = s.Close() })
	h := s.Handler()
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != status {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		return w
	}
	request("GET", "/api/jobs", "", 200)
	j := sampleJob()
	data, _ := json.Marshal(j)
	request("POST", "/api/jobs", string(data), 201)
	request("POST", "/api/jobs", string(data), 409)
	request("POST", "/api/jobs", `{"name":"bad","schedule":"bad","payload":{}}`, 400)
	request("POST", "/api/jobs", string(data)+`{}`, 400)
	request("POST", "/api/jobs/Documents%20Backup/run", "", 202)
	if triggered != j.Name {
		t.Fatalf("API triggered %q", triggered)
	}
	triggerErr = errors.New("disconnected")
	request("POST", "/api/jobs/Documents%20Backup/run", "", 503)
	request("PUT", "/api/jobs/Documents%20Backup", string(data), 200)
	request("PUT", "/api/jobs/other", string(data), 400)
	request("DELETE", "/api/jobs/Documents%20Backup", "", 204)
	request("POST", "/api/jobs/Documents%20Backup/run", "", 404)
	request("DELETE", "/api/jobs/Documents%20Backup", "", 404)
	request("PUT", "/api/jobs/Documents%20Backup", string(data), 404)
	request("POST", "/api/jobs", strings.Repeat(" ", (1<<20)+1), 400)
}

type completeToken struct{}

func (completeToken) Wait() bool                     { return true }
func (completeToken) WaitTimeout(time.Duration) bool { return true }
func (completeToken) Done() <-chan struct{}          { c := make(chan struct{}); close(c); return c }
func (completeToken) Error() error                   { return nil }

type fakeMQTTClient struct {
	paho.Client
	connected bool
	topic     string
	retained  bool
	payload   interface{}
}

func (c *fakeMQTTClient) IsConnected() bool { return c.connected }
func (c *fakeMQTTClient) Publish(topic string, qos byte, retained bool, payload interface{}) paho.Token {
	c.topic, c.retained, c.payload = topic, retained, payload
	return completeToken{}
}

func TestNamedTriggerUsesNonRetainedMQTT(t *testing.T) {
	c := &fakeMQTTClient{connected: true}
	m := &MQTT{client: c, cfg: &Config{TopicPrefix: "custom"}}
	if err := m.TriggerNamed("Documents Backup"); err != nil {
		t.Fatal(err)
	}
	if c.topic != "custom/run" || c.retained {
		t.Fatal("trigger must use configured run topic without retention")
	}
	var req RunRequest
	if err := json.Unmarshal(c.payload.([]byte), &req); err != nil || req.Name != "Documents Backup" || req.Command != "" {
		t.Fatalf("trigger: %+v %v", req, err)
	}
	c.connected = false
	if err := m.TriggerNamed(req.Name); err == nil {
		t.Fatal("disconnected trigger succeeded")
	}
}

func TestNameOnlyRunResolvesSavedCommand(t *testing.T) {
	var got map[string]any
	rc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sync/copy" {
			t.Errorf("unexpected method: %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		writeJSON(w, 200, map[string]int{"jobid": 42})
	}))
	defer rc.Close()
	j := sampleJob()
	s, _ := OpenJobStore(filepath.Join(t.TempDir(), "jobs.db"), nil)
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Save(j, true); err != nil {
		t.Fatal(err)
	}
	c := &fakeMQTTClient{connected: true}
	b := NewBridge(&Config{}, NewRCClient(rc.URL), &MQTT{client: c, cfg: &Config{TopicPrefix: "rclone"}})
	defer b.cancel()
	b.jobs = s
	b.onRun([]byte(`{"name":"Documents Backup","command_args":{"srcFs":"injected"}}`))
	s.runningJobs = b.runningJobIDs
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/jobs", nil))
	var listed []struct {
		Name string  `json:"name"`
		IDs  []int64 `json:"running_job_ids"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Name != j.Name || len(listed[0].IDs) != 1 || listed[0].IDs[0] != 42 {
		t.Fatalf("missing runtime association: %s", w.Body.String())
	}
	if got["srcFs"] != "/share/docs" || got["_async"] != true || b.running[42].name != j.Name {
		t.Fatalf("resolved request: %+v", got)
	}
	b.onRun([]byte(`{"name":"Unknown"}`))
	if !strings.Contains(c.payload.(string), "unknown named job") {
		t.Fatalf("missing error status: %v", c.payload)
	}
	// Legacy inline commands remain supported.
	b.onRun([]byte(`{"name":"Inline","command":"sync/copy","command_args":{"srcFs":"inline"}}`))
	if got["srcFs"] != "inline" {
		t.Fatal("inline command broke")
	}
}

func TestSQLiteDatabaseSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rclone", "mqtt-bridge", "data.db")
	s, err := OpenJobStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	j := sampleJob()
	if err := s.Save(j, true); err != nil {
		t.Fatal(err)
	}
	j.Schedule = "15 4 * * *"
	j.Payload = json.RawMessage(`{"command":"sync/sync","command_args":{"srcFs":"/share/updated"}}`)
	if err := s.Save(j, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(data), "SQLite format 3\x00") {
		t.Fatalf("not a SQLite database: %v", err)
	}
	s, err = OpenJobStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, ok := s.Get(j.Name)
	if !ok || got.Schedule != j.Schedule || string(got.Payload) != string(j.Payload) {
		t.Fatalf("updated definition not restored: %+v", got)
	}
	if len(s.cron.Entries()) != 1 {
		t.Fatal("schedule not restored")
	}
	// The database also enforces status-topic uniqueness independently of the cache.
	if _, err := s.db.Exec("INSERT INTO jobs (name, topic_slug, schedule, payload) VALUES (?, ?, ?, ?)", "documents-backup", slug(j.Name), "", string(j.Payload)); err == nil {
		t.Fatal("SQLite accepted duplicate status topic")
	}
}
