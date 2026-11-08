package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Config mirrors the add-on options in /data/options.json.
type Config struct {
	MQTTHost            string `json:"mqtt_host"`
	MQTTPort            int    `json:"mqtt_port"`
	MQTTUser            string `json:"mqtt_username"`
	MQTTPassword        string `json:"mqtt_password"`
	TopicPrefix         string `json:"topic_prefix"`
	RCAddr              string `json:"rc_addr"`
	JobExpireSeconds    int    `json:"job_expire_seconds"`
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
}

// LoadConfig reads and validates the add-on options file. With
// schema: false the Supervisor may not write options.json at all when the
// user has saved no options, so a missing file is treated as empty.
func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		raw = []byte(`{}`)
	} else if err != nil {
		return nil, err
	}
	cfg := &Config{}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	// The run script exports MQTT_HOST/PORT/USERNAME/PASSWORD from the
	// Supervisor-provided MQTT service when one is available; those take
	// priority over the manual mqtt_* options.
	if v := os.Getenv("MQTT_HOST"); v != "" {
		cfg.MQTTHost = v
	}
	if v := os.Getenv("MQTT_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MQTTPort = n
		}
	}
	if v := os.Getenv("MQTT_USERNAME"); v != "" {
		cfg.MQTTUser = v
	}
	if v := os.Getenv("MQTT_PASSWORD"); v != "" {
		cfg.MQTTPassword = v
	}
	if cfg.MQTTHost == "" {
		cfg.MQTTHost = "core-mosquitto"
	}
	if cfg.MQTTPort == 0 {
		cfg.MQTTPort = 1883
	}
	if cfg.TopicPrefix == "" {
		cfg.TopicPrefix = "rclone"
	}
	cfg.TopicPrefix = strings.Trim(cfg.TopicPrefix, "/")
	if cfg.TopicPrefix == "" || strings.ContainsAny(cfg.TopicPrefix, "+#") {
		return nil, fmt.Errorf("topic_prefix must be a non-empty MQTT topic prefix without wildcards")
	}
	if cfg.RCAddr == "" {
		cfg.RCAddr = "http://127.0.0.1:5572"
	}
	if cfg.JobExpireSeconds <= 0 {
		cfg.JobExpireSeconds = 3600
	}
	if cfg.PollIntervalSeconds < 5 {
		cfg.PollIntervalSeconds = 30
	}
	return cfg, nil
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// slug converts a display name to a topic-safe slug.
func slug(s string) string {
	s = nonSlugChars.ReplaceAllString(strings.ToLower(s), "_")
	return strings.Trim(s, "_")
}
