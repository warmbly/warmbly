package mailboximport

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestMergeSettingsWarmupPrecedence(t *testing.T) {
	on, off := true, false
	limit := 30
	for _, tc := range []struct {
		name       string
		batch, row *bool
		want       *bool
	}{
		{"unset batch keeps row on", nil, &on, &on},
		{"unset batch keeps row off", nil, &off, &off},
		{"batch on keeps row off", &on, &off, &off},
		{"batch on fills a row without a value", &on, nil, &on},
		{"batch off wins over row on", &off, &on, &off},
		{"batch off with no row value", &off, nil, &off},
		{"neither set", nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeSettings(models.MailboxImportSettings{Warmup: tc.batch}, models.MailboxImportSettings{Warmup: tc.row, DailyLimit: &limit})
			if (got.Warmup == nil) != (tc.want == nil) || got.Warmup != nil && *got.Warmup != *tc.want {
				t.Fatalf("warmup = %v, want %v", got.Warmup, tc.want)
			}
			if got.DailyLimit == nil || *got.DailyLimit != limit {
				t.Fatal("row value no longer wins for other settings")
			}
		})
	}
}

func TestStoredRowSettingsKeepBatchOffForAPreviousRunner(t *testing.T) {
	on, off := true, false
	// The previous release's merge: a row's own warmup value always won.
	previousMerge := func(batch, row models.MailboxImportSettings) *bool {
		if row.Warmup != nil {
			return row.Warmup
		}
		return batch.Warmup
	}
	for _, tc := range []struct {
		name       string
		batch, row *bool
		want       *bool
	}{
		{"batch off over row on", &off, &on, &off},
		{"batch off with no row value", &off, nil, &off},
		{"batch on keeps row off", &on, &off, &off},
		{"unset batch keeps row on", nil, &on, &on},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batch := models.MailboxImportSettings{Warmup: tc.batch}
			stored := storedRowSettings(batch, models.MailboxImportSettings{Warmup: tc.row})
			for name, got := range map[string]*bool{"previous": previousMerge(batch, stored), "current": mergeSettings(batch, stored).Warmup} {
				if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
					t.Fatalf("%s runner warmup = %v, want %v", name, got, tc.want)
				}
			}
		})
	}
}

func TestMergeSettingsBatchOffIgnoresRowWarmupValues(t *testing.T) {
	off := false
	batchStart, rowStart, rowMax, rowInc, rowRate := 8, 20, 60, 3, 50
	got := mergeSettings(
		models.MailboxImportSettings{Warmup: &off, WarmupStart: &batchStart},
		models.MailboxImportSettings{WarmupStart: &rowStart, WarmupMax: &rowMax, WarmupIncrease: &rowInc, WarmupReplyRate: &rowRate},
	)
	if got.WarmupStart == nil || *got.WarmupStart != batchStart || got.WarmupMax != nil || got.WarmupIncrease != nil || got.WarmupReplyRate != nil {
		t.Fatalf("batch Off let row warmup values through: %+v", got)
	}
	stored := storedRowSettings(models.MailboxImportSettings{Warmup: &off}, models.MailboxImportSettings{WarmupStart: &rowStart, WarmupMax: &rowMax})
	if stored.WarmupStart != nil || stored.WarmupMax != nil {
		t.Fatalf("stored row kept warmup values under batch Off: %+v", stored)
	}
}

type lifecycleRecorder struct {
	EmailConnector
	actions []string
}

func (r *lifecycleRecorder) Update(context.Context, string, string, string, *models.UpdateEmail) (*models.Email, *errx.Error) {
	return &models.Email{}, nil
}

func (r *lifecycleRecorder) SetWarmupLifecycle(_ context.Context, _, _, action string) (*models.Email, *errx.Error) {
	r.actions = append(r.actions, action)
	return &models.Email{}, nil
}

func TestClaimedRowAppliesBatchWarmupOffOverRowOn(t *testing.T) {
	on, off := true, false
	for _, tc := range []struct {
		name       string
		batch, row *bool
		want       []string
	}{
		{"batch off over row on", &off, &on, []string{"disable"}},
		{"unset batch keeps row on", nil, &on, []string{"start"}},
		{"batch on keeps row off", &on, &off, []string{"disable"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batch := models.MailboxImportSettings{Warmup: tc.batch}
			raw, _ := json.Marshal(batch)
			w := repository.ImportWorkRow{OrgID: uuid.New(), Settings: raw}
			row := models.MailboxImportSettings{Warmup: tc.row}
			// The previous release queued the row's own value untouched.
			for queuedBy, settings := range map[string]models.MailboxImportSettings{"this release": storedRowSettings(batch, row), "previous release": row} {
				p := payload{Settings: settings}
				rec := &lifecycleRecorder{}
				s := &Service{emails: rec}
				s.applySettings(t.Context(), w.OrgID, uuid.NewString(), uuid.New(), p, workSettings(w, p), false)
				if len(rec.actions) != len(tc.want) || rec.actions[0] != tc.want[0] {
					t.Fatalf("row queued by %s: lifecycle actions = %v, want %v", queuedBy, rec.actions, tc.want)
				}
			}
		})
	}
}
