package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// RunRequest is the message Home Assistant publishes to rclone/run to start
// a job.
type RunRequest struct {
	// Name identifies the job; it scopes the status/stats topics. Optional.
	Name string `json:"name"`
	// Command is the rc method, e.g. "sync/sync", "sync/bisync".
	Command string `json:"command"`
	// Args are the command's parameters.
	Args map[string]any `json:"command_args"`
}

// Bridge runs jobs requested over MQTT and publishes their status.
type Bridge struct {
	cfg  *Config
	rc   *RCClient
	mqtt *MQTT

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.Mutex
	running map[int64]*jobState // keyed by rclone jobid
}

func NewBridge(cfg *Config, rc *RCClient, mqtt *MQTT) *Bridge {
	ctx, cancel := context.WithCancel(context.Background())
	return &Bridge{cfg: cfg, rc: rc, mqtt: mqtt, ctx: ctx, cancel: cancel, running: map[int64]*jobState{}}
}

// Start subscribes to the run topic and begins polling.
func (b *Bridge) Start() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := b.mqtt.WaitConnected(ctx); err != nil {
		return fmt.Errorf("mqtt: %w", err)
	}
	b.mqtt.SubscribeRun(b.onRun)
	b.mqtt.Publish(b.mqtt.bridgeAvailabilityTopic(), "online")

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.pollLoop(b.ctx)
	}()
	return nil
}

// Stop marks the bridge offline and waits for the poller.
func (b *Bridge) Stop() {
	b.mqtt.Publish(b.mqtt.bridgeAvailabilityTopic(), "offline")
	b.cancel()
	b.wg.Wait()
}

// onRun handles a message on rclone/run.
func (b *Bridge) onRun(payload []byte) {
	var req RunRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		log.Printf("run: invalid JSON: %v", err)
		return
	}
	if req.Command == "" {
		log.Printf("run: missing command")
		return
	}
	if req.Name == "" {
		req.Name = req.Command
	}
	args, err := json.Marshal(req.Args)
	if err != nil {
		log.Printf("run %q: bad command_args: %v", req.Name, err)
		return
	}

	ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
	defer cancel()
	id, err := b.rc.StartJob(ctx, req.Command, args)
	if err != nil {
		log.Printf("run %q: start failed: %v", req.Name, err)
		b.publishStatus(req.Name, 0, "error", err.Error())
		return
	}
	log.Printf("run %q: started (jobid %d)", req.Name, id)

	s := &jobState{name: req.Name, jobID: id, b: b}
	b.mu.Lock()
	b.running[id] = s
	b.mu.Unlock()
	b.publishStatus(req.Name, id, "running", "")
}

// pollLoop periodically checks every running job.
func (b *Bridge) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(b.cfg.PollIntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.pollAll(ctx)
		}
	}
}

func (b *Bridge) pollAll(ctx context.Context) {
	b.mu.Lock()
	jobs := make([]*jobState, 0, len(b.running))
	for _, s := range b.running {
		jobs = append(jobs, s)
	}
	b.mu.Unlock()

	for _, s := range jobs {
		s.poll(ctx)
	}
}

func (b *Bridge) remove(id int64) {
	b.mu.Lock()
	delete(b.running, id)
	b.mu.Unlock()
}

// jobState tracks one running job.
type jobState struct {
	name  string
	jobID int64
	b     *Bridge
}

func (s *jobState) poll(ctx context.Context) {
	st, err := s.b.rc.Status(ctx, s.jobID)
	if err != nil {
		log.Printf("job %q (%d): status: %v", s.name, s.jobID, err)
		return
	}
	if st.Finished {
		state := "finished"
		errMsg := ""
		if !st.Success {
			state = "error"
			errMsg = st.Error
			if errMsg == "" {
				errMsg = "job failed"
			}
		}
		log.Printf("job %q (%d): %s%s", s.name, s.jobID, state, errSuffix(errMsg))
		s.b.publishStatus(s.name, s.jobID, state, errMsg)
		s.b.remove(s.jobID)
		return
	}
	s.b.publishStatus(s.name, s.jobID, "running", "")
	s.publishStats(ctx)
}

func (s *jobState) publishStats(ctx context.Context) {
	stats, err := s.b.rc.Stats(ctx, fmt.Sprintf("job/%d", s.jobID))
	if err != nil {
		return
	}
	out := map[string]any{
		"jobid":           s.jobID,
		"bytes":           stats.Bytes,
		"total_bytes":     stats.TotalBytes,
		"speed":           stats.Speed,
		"transfers":       stats.Transfers,
		"total_transfers": stats.TotalTransfers,
		"checks":          stats.Checks,
		"errors":          stats.Errors,
	}
	if stats.TotalBytes > 0 {
		out["progress"] = float64(stats.Bytes) / float64(stats.TotalBytes) * 100
	}
	if stats.ETA > 0 {
		out["eta"] = stats.ETA
	}
	data, err := json.Marshal(out)
	if err != nil {
		return
	}
	s.b.mqtt.Publish(s.b.mqtt.statsTopic(s.name), string(data))
}

func (b *Bridge) publishStatus(name string, jobID int64, state, errMsg string) {
	out := map[string]any{"state": state}
	if jobID != 0 {
		out["jobid"] = jobID
	}
	if errMsg != "" {
		out["error"] = errMsg
	}
	data, err := json.Marshal(out)
	if err != nil {
		return
	}
	b.mqtt.Publish(b.mqtt.statusTopic(name), string(data))
}

func errSuffix(msg string) string {
	if msg == "" {
		return ""
	}
	return ": " + strings.TrimSpace(msg)
}
