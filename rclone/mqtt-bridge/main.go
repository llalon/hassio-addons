// Command mqtt-bridge runs rclone commands requested over MQTT: Home
// Assistant publishes a run request to rclone/run, and the bridge runs it
// against the local rclone rc API and publishes status and stats back.
package main

import (
	"context"
	"log"
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

	b := NewBridge(cfg, rc, mqtt)
	if err := b.Start(); err != nil {
		log.Fatalf("bridge: %v", err)
	}

	log.Printf("started: listening on %s/run", cfg.TopicPrefix)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	b.Stop()
	log.Print("stopped")
}
