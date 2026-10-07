package handler

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func TestWarmupSettingsUpdatePreservesOmittedFields(t *testing.T) {
	current := models.DefaultWarmupGenerationSettings()
	current.Model, current.CadenceHours = "stored-configured-model", 36
	got, err := decodeWarmupGenerationSettings(strings.NewReader(`{"generation_enabled":false,"ai_selection_share":0}`), current)
	if err != nil {
		t.Fatal(err)
	}
	if got.GenerationEnabled || got.AISelectionShare != 0 || got.Model != current.Model || got.CadenceHours != 36 || !got.ScheduleEnabled {
		t.Fatalf("omitted/explicit fields lost: %+v", got)
	}
}

func TestWarmupSettingsUpdateRejectsInvalidDocuments(t *testing.T) {
	for _, input := range []string{"", "null", "[]", `{"unknown":true}`, `{} {}`, `{"enabled":"false"}`, `{"pools":[{"unexpected":true}]}`, `{"model":"` + strings.Repeat("x", 64*1024) + `"}`} {
		if _, err := decodeWarmupGenerationSettings(strings.NewReader(input), models.DefaultWarmupGenerationSettings()); err == nil {
			t.Errorf("accepted invalid settings document (length %d)", len(input))
		}
	}
}
