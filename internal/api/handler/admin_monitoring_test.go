package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/monitoring"
	"github.com/warmbly/warmbly/internal/models"
)

func TestMonitoringAdminHandlerPermissionsAndWireCompatibility(t *testing.T) {
	n := int64(42)
	h := &Handler{Monitoring: monitoring.New([]monitoring.Source{{ID: "mailboxes", Permission: models.AdminPermViewUsers, Collect: func(context.Context, time.Time) (models.MonitoringSource, error) {
		return models.MonitoringSource{Metrics: []models.MonitoringMetric{{ID: "private", Count: &n, Availability: models.MonitoringFresh}}}, nil
	}}})}
	for _, permissions := range []models.AdminPermission{0, models.AdminPermViewUsers, models.AdminPermViewAnalytics, models.AdminPermViewAnalytics | models.AdminPermViewUsers} {
		t.Run(fmt.Sprint(permissions), func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/admin/instance/monitoring", nil)
			c.Set(middleware.AdminPermissionsKey, permissions)
			h.AdminInstanceMonitoring(c)
			if !permissions.HasPermission(models.AdminPermViewAnalytics) {
				if w.Code != http.StatusForbidden {
					t.Fatal(w.Code)
				}
				return
			}
			var snapshot models.MonitoringSnapshot
			if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || snapshot.Version != "1" || len(snapshot.Sources) != 1 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Body.String())
			}
			source := snapshot.Sources[0]
			if !permissions.HasPermission(models.AdminPermViewUsers) {
				if len(source.Metrics) != 0 || source.Reason != "permission_denied" || source.Availability != models.MonitoringUnavailable || source.Coverage != "unavailable" || source.ObservedAt != nil || source.MeasuredScopes != nil || source.ExpectedScopes != nil {
					t.Fatal("permission leak", w.Body.String())
				}
			} else if len(source.Metrics) != 1 || source.Metrics[0].Count == nil || *source.Metrics[0].Count != n {
				t.Fatal("authorized measurement missing", w.Body.String())
			}
		})
	}
	h.Monitoring = nil
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/instance/monitoring", nil)
	c.Set(middleware.AdminPermissionsKey, models.AdminPermViewAnalytics)
	h.AdminInstanceMonitoring(c)
	var snapshot models.MonitoringSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || snapshot.Version != "1" || snapshot.Coverage != "unavailable" || len(snapshot.Sources) != 0 {
		t.Fatal(w.Body.String())
	}
}
