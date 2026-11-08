package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// MQTT wraps the paho client with the bridge's topic helpers.
type MQTT struct {
	client    paho.Client
	cfg       *Config
	connected chan struct{}
	once      sync.Once

	mu         sync.Mutex // guards runHandler
	runHandler func(payload []byte)
}

// NewMQTTClient connects to the broker, retrying until it succeeds.
func NewMQTTClient(cfg *Config) (*MQTT, error) {
	m := &MQTT{cfg: cfg, connected: make(chan struct{})}
	opts := paho.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%d", cfg.MQTTHost, cfg.MQTTPort)).
		SetClientID("rclone-addon-mqtt-bridge").
		SetAutoReconnect(true).
		SetConnectTimeout(10 * time.Second).
		SetOnConnectHandler(m.onConnect).
		SetConnectionLostHandler(func(_ paho.Client, err error) {
			log.Printf("mqtt: connection lost: %v", err)
		})
	if cfg.MQTTUser != "" {
		opts.SetUsername(cfg.MQTTUser).SetPassword(cfg.MQTTPassword)
	}
	m.client = paho.NewClient(opts)
	log.Printf("mqtt: connecting to %s:%d", cfg.MQTTHost, cfg.MQTTPort)
	// Connect with retry: paho's SetConnectRetry only applies after the
	// first successful connection, so loop here for broker restarts.
	go func() {
		for {
			tok := m.client.Connect()
			if tok.WaitTimeout(15*time.Second) && tok.Error() != nil {
				log.Printf("mqtt: connect failed: %v (retrying in 5s)", tok.Error())
				time.Sleep(5 * time.Second)
				continue
			}
			return
		}
	}()
	return m, nil
}

func (m *MQTT) onConnect(c paho.Client) {
	log.Printf("mqtt: connected")
	m.once.Do(func() { close(m.connected) })
	m.mu.Lock()
	h := m.runHandler
	m.mu.Unlock()
	if h != nil {
		m.subscribeRun()
	}
}

// WaitConnected blocks until the first broker connection is established.
func (m *MQTT) WaitConnected(ctx context.Context) error {
	select {
	case <-m.connected:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Publish sends a retained message.
func (m *MQTT) Publish(topic string, payload string) {
	tok := m.client.Publish(topic, 1, true, payload)
	if tok.WaitTimeout(10*time.Second) && tok.Error() != nil {
		log.Printf("mqtt: publish %s: %v", topic, tok.Error())
	}
}

// SubscribeRun registers the handler for rclone/run.
func (m *MQTT) SubscribeRun(handler func(payload []byte)) {
	m.mu.Lock()
	m.runHandler = handler
	m.mu.Unlock()
	if m.client.IsConnected() {
		m.subscribeRun()
	}
}

func (m *MQTT) subscribeRun() {
	topic := m.runTopic()
	tok := m.client.Subscribe(topic, 1, func(_ paho.Client, msg paho.Message) {
		m.mu.Lock()
		h := m.runHandler
		m.mu.Unlock()
		if h != nil {
			h(msg.Payload())
		}
	})
	if tok.WaitTimeout(10*time.Second) && tok.Error() != nil {
		log.Printf("mqtt: subscribe %s: %v", topic, tok.Error())
	}
}

func (m *MQTT) runTopic() string {
	return m.cfg.TopicPrefix + "/run"
}

func (m *MQTT) statusTopic(name string) string {
	return fmt.Sprintf("%s/%s/status", m.cfg.TopicPrefix, slug(name))
}

func (m *MQTT) statsTopic(name string) string {
	return fmt.Sprintf("%s/%s/stats", m.cfg.TopicPrefix, slug(name))
}

func (m *MQTT) bridgeAvailabilityTopic() string {
	return m.cfg.TopicPrefix + "/bridge/availability"
}
