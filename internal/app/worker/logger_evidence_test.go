package worker

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/pkg/nodeevidence"
	"go.uber.org/zap"
)

func TestStructuredWorkerHookEmitsOnlyAllowlistedSeverity(t *testing.T) {
	var got []nodeevidence.Event
	nodeevidence.Install(func(e nodeevidence.Event) { got = append(got, e) })
	t.Cleanup(func() { nodeevidence.Install(nil) })
	logger, err := NewLoggerWithHandler(nil)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("private message should not be diagnostic evidence")
	logger.With(zap.String("password", "fixture-secret")).Error("raw provider error fixture")
	if len(got) != 1 || got[0].Name != nodeevidence.WorkerError {
		t.Fatal("hook did not capture a coarse event", got)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "fixture-secret") || strings.Contains(string(raw), "provider error") {
		t.Fatal("raw log fields were transported")
	}
}
