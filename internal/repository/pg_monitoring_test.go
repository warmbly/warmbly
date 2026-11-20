package repository

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMonitoringStatementUsesOneDatabaseObservation(t *testing.T) {
	for id, section := range monitoringSections {
		t.Run(id, func(t *testing.T) {
			query := monitoringStatement(section)
			if !strings.HasPrefix(query, "WITH observation AS MATERIALIZED (SELECT clock_timestamp() AS at) ") || strings.Count(query, "clock_timestamp()") != 1 || strings.Contains(query, "$1") || strings.Contains(query, "$stale") || strings.Count(query, " UNION ALL ") != len(section.queries)-1 {
				t.Fatal("source lost its single statement/database reference", query)
			}
			for _, metric := range section.queries {
				if !strings.Contains(query, "SELECT '"+metric.id+"', (SELECT at FROM observation), ") {
					t.Fatal("metric does not return the source reference", metric.id)
				}
			}
		})
	}
}

// Sync reads as stale only past twice the longest configured full check interval.
func TestSyncStaleCutoffFollowsTheFullCheckInterval(t *testing.T) {
	r := NewMonitoringRepository(nil)
	if got := r.staleAfter(context.Background()); got != 2*time.Hour {
		t.Fatalf("default cutoff %v, want twice the 60 minute default", got)
	}
	r.WireSyncStaleAfter(func(context.Context) time.Duration { return 6 * time.Hour })
	query := monitoringStatementWith(monitoringSections["sync"], r.staleAfter(context.Background()))
	if strings.Count(query, "INTERVAL '21600 seconds'") != 3 {
		t.Fatalf("every stale metric must use the wired cutoff: %s", query)
	}
}
