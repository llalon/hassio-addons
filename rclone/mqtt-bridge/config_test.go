package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Bisync Laptop":   "bisync_laptop",
		"check (weekly)!": "check_weekly",
		"  spaces  ":      "spaces",
		"sync/copy":       "sync_copy",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	p := t.TempDir() + "/options.json"
	if err := writeFile(p, `{}`); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MQTTHost != "core-mosquitto" || cfg.MQTTPort != 1883 {
		t.Errorf("bad mqtt defaults: %+v", cfg)
	}
	if cfg.RCAddr != "http://127.0.0.1:5572" {
		t.Errorf("rc addr = %s", cfg.RCAddr)
	}
	if cfg.JobExpireSeconds != 3600 || cfg.PollIntervalSeconds != 30 {
		t.Errorf("bad intervals: %+v", cfg)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	// With schema: false the Supervisor may not write options.json.
	cfg, err := LoadConfig(t.TempDir() + "/does-not-exist.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MQTTHost != "core-mosquitto" {
		t.Errorf("mqtt host = %s", cfg.MQTTHost)
	}
}

func TestLoadConfigInvalidJSON(t *testing.T) {
	p := t.TempDir() + "/options.json"
	if err := writeFile(p, `{not json`); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(p); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestRunRequestParsing(t *testing.T) {
	var req RunRequest
	err := jsonUnmarshal([]byte(`{"name":"Documents Backup","command":"sync/sync","command_args":{"srcFs":"/share/a","dstFs":"/share/b"}}`), &req)
	if err != nil {
		t.Fatal(err)
	}
	if req.Name != "Documents Backup" || req.Command != "sync/sync" {
		t.Errorf("bad parse: %+v", req)
	}
	if req.Args["srcFs"] != "/share/a" {
		t.Errorf("args = %v", req.Args)
	}
}

func TestRunRequestNameDefaultsToCommand(t *testing.T) {
	var req RunRequest
	if err := jsonUnmarshal([]byte(`{"command":"sync/sync"}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.Name != "" {
		// Name defaulting happens in onRun, not unmarshalling.
		t.Errorf("name should be empty before defaulting, got %q", req.Name)
	}
}

func TestErrSuffix(t *testing.T) {
	if errSuffix("") != "" {
		t.Error("empty should be empty")
	}
	if !strings.Contains(errSuffix("boom"), "boom") {
		t.Error("should contain message")
	}
}

func TestLoadConfigEnvOverride(t *testing.T) {
	p := t.TempDir() + "/options.json"
	if err := writeFile(p, `{"mqtt_host":"manual-host","mqtt_port":1234}`); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MQTT_HOST", "service-host")
	t.Setenv("MQTT_PORT", "1883")
	t.Setenv("MQTT_USERNAME", "svcuser")
	t.Setenv("MQTT_PASSWORD", "svcpass")
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MQTTHost != "service-host" || cfg.MQTTPort != 1883 {
		t.Errorf("env override failed: %+v", cfg)
	}
	if cfg.MQTTUser != "svcuser" || cfg.MQTTPassword != "svcpass" {
		t.Errorf("env creds failed: %+v", cfg)
	}
}

func TestLoadConfigManualFallback(t *testing.T) {
	p := t.TempDir() + "/options.json"
	if err := writeFile(p, `{"mqtt_host":"manual-host","mqtt_port":1234}`); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MQTTHost != "manual-host" || cfg.MQTTPort != 1234 {
		t.Errorf("manual options not honored: %+v", cfg)
	}
}

func TestLoadConfigTopicPrefix(t *testing.T) {
	p := t.TempDir() + "/options.json"
	if err := writeFile(p, `{"topic_prefix":"/backups/rclone/"}`); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TopicPrefix != "backups/rclone" {
		t.Errorf("topic prefix = %q", cfg.TopicPrefix)
	}
}

func TestLoadConfigTopicPrefixDefault(t *testing.T) {
	p := t.TempDir() + "/options.json"
	if err := writeFile(p, `{}`); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TopicPrefix != "rclone" {
		t.Errorf("topic prefix default = %q", cfg.TopicPrefix)
	}
}

func TestLoadConfigTopicPrefixRejectsWildcard(t *testing.T) {
	p := t.TempDir() + "/options.json"
	if err := writeFile(p, `{"topic_prefix":"rclone/#"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(p); err == nil {
		t.Error("expected error for wildcard topic_prefix")
	}
}
