package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RCClient is a minimal client for the rclone remote control API.
type RCClient struct {
	base string
	hc   *http.Client
}

func NewRCClient(base string) *RCClient {
	return &RCClient{base: base, hc: &http.Client{Timeout: 30 * time.Second}}
}

// Call invokes an rc method with a JSON object body and decodes the response.
func (c *RCClient) Call(ctx context.Context, method string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d: %s", method, resp.StatusCode, truncate(string(data), 200))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s: decoding response: %w", method, err)
	}
	return nil
}

// WaitReady blocks until the rc API answers or the timeout elapses.
func (c *RCClient) WaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var out map[string]any
		if err := c.Call(ctx, "rc/noop", map[string]any{}, &out); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// StartJob runs an rc command asynchronously and returns its job ID.
func (c *RCClient) StartJob(ctx context.Context, command string, args json.RawMessage) (int64, error) {
	var merged map[string]any
	if err := json.Unmarshal(args, &merged); err != nil {
		return 0, fmt.Errorf("command_args: %w", err)
	}
	if merged == nil {
		merged = map[string]any{}
	}
	merged["_async"] = true
	var out struct {
		JobID int64 `json:"jobid"`
	}
	if err := c.Call(ctx, command, merged, &out); err != nil {
		return 0, err
	}
	return out.JobID, nil
}

// JobStatus is the subset of job/status we need.
type JobStatus struct {
	Finished  bool    `json:"finished"`
	Success   bool    `json:"success"`
	Error     string  `json:"error"`
	Duration  float64 `json:"duration"`
	StartTime string  `json:"startTime"`
	EndTime   string  `json:"endTime"`
}

// Status returns the status of a job ID.
func (c *RCClient) Status(ctx context.Context, jobID int64) (*JobStatus, error) {
	var out JobStatus
	err := c.Call(ctx, "job/status", map[string]any{"jobid": jobID}, &out)
	return &out, err
}

// StopJob stops a running job.
func (c *RCClient) StopJob(ctx context.Context, jobID int64) error {
	return c.Call(ctx, "job/stop", map[string]any{"jobid": jobID}, nil)
}

// CoreStats is the subset of core/stats we publish as sensor state.
type CoreStats struct {
	Bytes          int64   `json:"bytes"`
	TotalBytes     int64   `json:"totalBytes"`
	Speed          float64 `json:"speed"`
	Transfers      int     `json:"transfers"`
	TotalTransfers int     `json:"totalTransfers"`
	Checks         int     `json:"checks"`
	TotalChecks    int     `json:"totalChecks"`
	Deletes        int     `json:"deletes"`
	Errors         int     `json:"errors"`
	FatalError     bool    `json:"fatalError"`
	RetryError     bool    `json:"retryError"`
	ElapsedTime    float64 `json:"elapsedTime"`
	ETA            float64 `json:"eta"`
}

// Stats returns the stats for a job group ("job/<id>").
func (c *RCClient) Stats(ctx context.Context, group string) (*CoreStats, error) {
	var out CoreStats
	err := c.Call(ctx, "core/stats", map[string]any{"group": group}, &out)
	return &out, err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
