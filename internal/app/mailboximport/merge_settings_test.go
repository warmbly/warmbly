package mailboximport

import (
	"testing"

	"github.com/warmbly/warmbly/internal/models"
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
