package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

func TestArrivalDiagnosticsShutdownBetweenPassesFlushesReturnedWork(t *testing.T) {
	var output bytes.Buffer
	previous := log.Logger
	log.Logger = zerolog.New(&output)
	parent, cancel := context.WithCancel(t.Context())
	passes := make(chan context.Context, 2)
	stub := &diagnosticArrivalStub{delivery: func(ctx context.Context, _ func(context.Context, models.JobEventType, any) error) error {
		passes <- ctx
		return nil
	}}
	s := &JobsService{ArrivalOutbox: stub}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runArrivalOutbox(parent)
	}()
	defer func() {
		cancel()
		<-done
		log.Logger = previous
	}()
	for range 2 {
		select {
		case pass := <-passes:
			select {
			case <-pass.Done():
			case <-time.After(20 * time.Second):
				t.Fatal("returned pass context was not released")
			}
		case <-time.After(20 * time.Second):
			t.Fatal("normal five-second pass cadence did not return")
		}
	}
	if parent.Err() != nil {
		t.Fatal("parent cancelled before the returned-work observation")
	}
	cancel()
	<-done
	var summaries, attempts, cancelled float64
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record["message"] != "sync arrival delivery diagnostics" {
			continue
		}
		summaries++
		attempts += record["pass_attempts"].(float64)
		cancelled += record["cancelled_pass_attempts"].(float64)
	}
	// Two passes five seconds apart share one backlog reading (arrivalBacklogInterval).
	if summaries != 2 || attempts != 2 || cancelled != 0 || stub.backlogCalls != 1 {
		t.Fatalf("shutdown lost returned work or invented a cancelled pass: summaries=%v attempts=%v cancelled=%v backlog=%d", summaries, attempts, cancelled, stub.backlogCalls)
	}
}

func TestArrivalDiagnosticsCancelledInFlightWaitsForReturnAndFlushesOnce(t *testing.T) {
	var output bytes.Buffer
	previous := log.Logger
	log.Logger = zerolog.New(&output)
	parent, cancel := context.WithCancel(t.Context())
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	stub := &diagnosticArrivalStub{delivery: func(ctx context.Context, _ func(context.Context, models.JobEventType, any) error) error {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return ctx.Err()
	}}
	s := &JobsService{ArrivalOutbox: stub}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runArrivalOutbox(parent)
	}()
	defer func() {
		cancel()
		log.Logger = previous
	}()
	<-entered
	cancel()
	<-cancelled
	if output.Len() != 0 {
		close(release)
		<-done
		t.Fatal("unreturned pass was reported")
	}
	close(release)
	<-done
	var summaries float64
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record["message"] != "sync arrival delivery diagnostics" {
			continue
		}
		summaries++
		if record["pass_attempts"] != float64(1) || record["cancelled_pass_attempts"] != float64(1) || record["delivery_error_pass_attempts"] != float64(1) || record["deadline_pass_attempts"] != float64(0) {
			t.Fatalf("terminal pass facts changed: %v", record)
		}
	}
	if summaries != 1 || stub.backlogCalls != 1 {
		t.Fatalf("terminal flush duplicated a pass: summaries=%v backlog=%d", summaries, stub.backlogCalls)
	}
}
