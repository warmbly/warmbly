package warmupcontent

import (
	"testing"

	"github.com/warmbly/warmbly/internal/pkg/generation"
)

func TestEndedBatchOutcome(t *testing.T) {
	tests := []struct {
		name    string
		state   generation.BatchState
		results string
		err     string
	}{
		{
			name:    "completed reads the output file",
			state:   generation.BatchState{Status: "completed", OutputFileID: "file-out"},
			results: "file-out",
		},
		{
			name:    "completed with every request refused reads the error file",
			state:   generation.BatchState{Status: "completed", ErrorFileID: "file-err"},
			results: "file-err",
		},
		{
			name:  "completed with neither file says so",
			state: generation.BatchState{Status: "completed"},
			err:   "batch completed with no output file",
		},
		{
			name:    "expired ingests what the window did finish",
			state:   generation.BatchState{Status: "expired", OutputFileID: "file-out", ErrorFileID: "file-err"},
			results: "file-out",
			err:     "batch expired",
		},
		{
			name:    "expired with nothing finished reads the error file",
			state:   generation.BatchState{Status: "expired", ErrorFileID: "file-err"},
			results: "file-err",
			err:     "batch expired",
		},
		{
			name:    "cancelled ingests what was already done",
			state:   generation.BatchState{Status: "cancelled", OutputFileID: "file-out"},
			results: "file-out",
			err:     "batch cancelled",
		},
		{
			name:  "failed never ran, so there is nothing to ingest",
			state: generation.BatchState{Status: "failed", FailureReason: "quota exceeded"},
			err:   "batch failed: quota exceeded",
		},
		{
			name:  "failed without a provider reason still names the status",
			state: generation.BatchState{Status: "failed"},
			err:   "batch failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := endedBatchOutcome(tc.state)
			if got.ResultsFileID != tc.results {
				t.Errorf("results file = %q, want %q", got.ResultsFileID, tc.results)
			}
			if got.Error != tc.err {
				t.Errorf("error = %q, want %q", got.Error, tc.err)
			}
		})
	}
}

func TestBatchEnded(t *testing.T) {
	ended := []string{"completed", "failed", "expired", "cancelled"}
	running := []string{"validating", "in_progress", "finalizing", "cancelling", "submitted", ""}

	for _, s := range ended {
		if !batchEnded(s) {
			t.Errorf("batchEnded(%q) = false, want true", s)
		}
	}
	for _, s := range running {
		if batchEnded(s) {
			t.Errorf("batchEnded(%q) = true, want false", s)
		}
	}
}
