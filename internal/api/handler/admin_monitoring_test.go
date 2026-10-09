package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
			if w.Code != http.StatusOK || snapshot.Version != "1" || snapshot.Sources == nil || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Body.String())
			}
			if !permissions.HasPermission(models.AdminPermViewUsers) && (strings.Contains(w.Body.String(), "42") || len(snapshot.Sources[0].Metrics) != 0 || snapshot.Sources[0].Reason != "permission_denied") {
				t.Fatal("permission leak", w.Body.String())
			}
		})
	}
	h.Monitoring = nil
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/instance/monitoring", nil)
	c.Set(middleware.AdminPermissionsKey, models.AdminPermViewAnalytics)
	h.AdminInstanceMonitoring(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"coverage":"unavailable"`) {
		t.Fatal(w.Body.String())
	}
}
