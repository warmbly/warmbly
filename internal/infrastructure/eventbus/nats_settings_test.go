package eventbus

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestNATSReconnectPreservesManagedStreamSettings(t *testing.T) {
	url := startEmbeddedNATS(t)
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	_, err = js.CreateStream(t.Context(), jetstream.StreamConfig{
		Name: "managed", Subjects: []string{"warmbly.>"}, Storage: jetstream.FileStorage,
		Replicas: 1, MaxAge: 48 * time.Hour, MaxBytes: 8192, Discard: jetstream.DiscardNew,
		Duplicates: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewNATS(NATSConfig{URL: url, StreamName: "managed"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	s, err := js.Stream(t.Context(), "managed")
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c := info.Config
	if c.Replicas != 1 || c.MaxBytes != 8192 || c.MaxAge != 48*time.Hour || c.Discard != jetstream.DiscardNew || c.Duplicates != 30*time.Second {
		t.Fatalf("reconnect changed managed settings: %+v", c)
	}
}

func TestNATSDiscardNewProtectsQueuedCommands(t *testing.T) {
	b, err := NewNATS(NATSConfig{URL: startEmbeddedNATS(t), MaxBytes: 1024, Replicas: 1, DiscardNew: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	ctx := context.Background()
	payload := make([]byte, 600)
	if err := b.Publish(ctx, "commands", "first", payload); err != nil {
		t.Fatal(err)
	}
	if err := b.Publish(ctx, "commands", "second", payload); err == nil {
		t.Fatal("capacity exhaustion must reject the new command")
	}
	s, err := b.js.Stream(ctx, b.stream)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.GetMsg(ctx, 1)
	if err != nil || m.Header.Get("Warmbly-Key") != "first" {
		t.Fatalf("old queued command lost: %v", err)
	}
}

func TestNATSTLSRejectsInvalidTrustBundle(t *testing.T) {
	for _, value := range []string{"invalid-base64!", "bm90IGEgY2VydGlmaWNhdGU="} {
		t.Setenv("NATS_TLS_CA_B64", value)
		if _, err := natsTLSOption(); err == nil {
			t.Fatal("invalid trust bundle accepted")
		}
	}
}
