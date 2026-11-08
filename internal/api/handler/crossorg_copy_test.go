package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/crossorgcopy"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type copyMemberships []models.OrganizationMember

func (m copyMemberships) GetUserOrganizations(context.Context, uuid.UUID) ([]models.OrganizationMember, *errx.Error) {
	return m, nil
}

// A key or OAuth token reaches one workspace: it needs the write scope to copy
// at all, and it never copies into another workspace.
func TestCopyCampaignToWorkspacesKeyBounds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	src, dst, user := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()
	members := copyMemberships{
		{OrganizationID: src, Role: "owner", AcceptedAt: &now},
		{OrganizationID: dst, Role: "owner", AcceptedAt: &now},
	}
	h := &Handler{CrossOrgCopy: crossorgcopy.NewService(crossorgcopy.Deps{Orgs: members})}

	cases := []struct {
		name     string
		auth     string
		perms    uint64
		wantCode string
	}{
		{"key without write scope", middleware.AuthTypeAPIKey, models.APIPermReadCampaigns, "forbidden"},
		{"key to another workspace", middleware.AuthTypeAPIKey, models.APIPermReadCampaigns | models.APIPermWriteCampaigns, crossorgcopy.CodeRequiresSession},
		{"oauth to another workspace", middleware.AuthTypeOAuth, models.APIPermReadCampaigns | models.APIPermWriteCampaigns, crossorgcopy.CodeRequiresSession},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			body := `{"organization_ids":["` + dst.String() + `"]}`
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/campaigns/x/copy-to", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "id", Value: uuid.NewString()}}
			c.Set(middleware.OrganizationIDKey, src)
			c.Set(middleware.UserIDKey, user.String())
			c.Set(middleware.AuthTypeKey, tc.auth)
			c.Set(middleware.APIKeyPermissionsKey, tc.perms)
			h.CopyCampaignToWorkspaces(c)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
			}
			var resp struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			if resp.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q", resp.Code, tc.wantCode)
			}
		})
	}
}
