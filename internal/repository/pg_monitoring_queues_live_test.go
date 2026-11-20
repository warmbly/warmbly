package repository

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

func TestMonitoringNilDatabaseCollectorsRemainUnavailable(t *testing.T) {
	var r *MonitoringRepository
	for _, source := range r.Sources() {
		out, err := source.Collect(t.Context(), time.Now())
		if err != nil || out.Availability != models.MonitoringUnavailable || out.Reason != "dependency_missing" {
			t.Fatal(source.ID, out, err)
		}
	}
}
