package models

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestWarmupSettingsRoundTrip(t *testing.T) {
	s := DefaultWarmupGenerationSettings()
	s.GenerationEnabled, s.Enabled, s.ScheduleEnabled, s.RefreshEnabled = false, false, false, false
	s.Model, s.CadenceHours, s.RefreshPerRun = "configured-provider-model", 24, 0
	s.DailyGenerationCap, s.AISelectionShare, s.MaxMessagesPerThread = 17, 0, 2
	s.Pools[0].Enabled, s.Pools[0].TargetActiveThreads = false, 12
	s.Pools[0].Segments = []string{"engineering", "sales"}
	before := s
	s.Normalize()
	if !reflect.DeepEqual(s, before) {
		t.Fatalf("supported settings changed: before=%+v after=%+v", before, s)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	got := DefaultWarmupGenerationSettings()
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	got.Normalize()
	if !reflect.DeepEqual(got, s) {
		t.Fatalf("round trip changed supported settings: %+v", got)
	}
}

func TestWarmupLegacySettingsDefaultsWithoutReplacingModel(t *testing.T) {
	s := DefaultWarmupGenerationSettings()
	if err := json.Unmarshal([]byte(`{"model":"previous-valid-model","enabled":false,"pools":[{"pool_type":"free","enabled":true,"target_active_threads":350,"segments":["engineering"]}]}`), &s); err != nil {
		t.Fatal(err)
	}
	s.Normalize()
	if !s.GenerationEnabled || s.Enabled || s.Model != "previous-valid-model" || s.RefreshPerRun != 10 {
		t.Fatalf("lost legacy/default policy: %+v", s)
	}
	if s.Pools[0].PoolType != "premium" || s.Pools[0].TargetActiveThreads != 350 || s.Pools[0].Segments[0] != "engineering" {
		t.Fatalf("shared-library fallback lost valid settings: %+v", s.Pools)
	}
}

func TestWarmupSettingsClampAndDefaultIdempotence(t *testing.T) {
	s := DefaultWarmupGenerationSettings()
	want := DefaultWarmupGenerationSettings()
	s.Normalize()
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("default settings changed: %+v", s)
	}
	s.CadenceHours, s.RefreshPerRun, s.AISelectionShare, s.MaxMessagesPerThread = -1, 100, 200, 0
	s.Engagement.MinDwellSeconds, s.Engagement.MaxDwellSeconds = 8000, -1
	s.Normalize()
	if s.CadenceHours != 1 || s.RefreshPerRun != 25 || s.AISelectionShare != 100 || s.MaxMessagesPerThread != 1 || s.Engagement.MinDwellSeconds != 3600 || s.Engagement.MaxDwellSeconds != 3600 {
		t.Fatalf("unsafe ranges: %+v", s)
	}
}
