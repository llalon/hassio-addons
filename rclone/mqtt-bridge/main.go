// Command mqtt-bridge runs rclone commands requested over MQTT: Home
// Assistant publishes a run request to rclone/run, and the bridge runs it
// against the local rclone rc API and publishes status and stats back.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[mqtt-bridge] ")

	optionsPath := os.Getenv("OPTIONS_FILE")
	if optionsPath == "" {
		optionsPath = "/data/options.json"
	}
	cfg, err := LoadConfig(optionsPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	rc := NewRCClient(cfg.RCAddr)
	if err := rc.WaitReady(context.Background(), 60*time.Second); err != nil {
		log.Printf("warning: rclone rc not ready yet: %v (will retry in background)", err)
	}

	mqtt, err := NewMQTTClient(cfg)
	if err != nil {
		log.Fatalf("mqtt: %v", err)
	}

	jobsPath := os.Getenv("JOBS_FILE")
	if jobsPath == "" {
		jobsPath = "/data/jobs.db"
	}
	jobs, err := OpenJobStore(jobsPath, mqtt.TriggerNamed)
	if err != nil {
		log.Fatalf("jobs: %v", err)
	}
	b := NewBridge(cfg, rc, mqtt)
	b.jobs = jobs
	if err := b.Start(); err != nil {
		log.Fatalf("bridge: %v", err)
	}

	jobs.cron.Start()
	server := &http.Server{Addr: "127.0.0.1:5573", Handler: jobs.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("jobs API: %v", err)
		}
	}()

	log.Printf("started: listening on %s/run", cfg.TopicPrefix)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	if err := jobs.Close(); err != nil {
		log.Printf("closing jobs database: %v", err)
	}
	b.Stop()
	log.Print("stopped")
}
