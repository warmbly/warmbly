package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/companyenrich"
	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/models"
)

type companyEnrichConnectionRecorder struct {
	org, id uuid.UUID
	calls   int
}

func (f *companyEnrichConnectionRecorder) ConnectionConfig(_ context.Context, org, id uuid.UUID) (map[string]any, *models.IntegrationConnection, error) {
	f.org, f.id = org, id
	f.calls++
	return nil, nil, integration.ErrConnectionNotFound
}
func (f *companyEnrichConnectionRecorder) MarkConnectionHealth(context.Context, uuid.UUID, models.IntegrationStatus, models.IntegrationHealth, string) {
	panic("missing connection must not change health")
}

func companyEnrichContext(body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/integrations/companyenrich/id/query", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(middleware.OrganizationIDKey, uuid.New())
	c.Set(middleware.UserIDKey, uuid.NewString())
	c.Params = gin.Params{{Key: "id", Value: uuid.NewString()}}
	return c, w
}

func TestCompanyEnrichHandlerUsesAuthenticatedOrganization(t *testing.T) {
	f := &companyEnrichConnectionRecorder{}
	h := &Handler{CompanyEnrichService: companyenrich.NewService(companyenrich.Deps{Integrations: f})}
	c, w := companyEnrichContext(`{"operation":"count_companies"}`)
	wantOrg := *middleware.GetOrganizationID(c)
	wantID := uuid.MustParse(c.Param("id"))
	h.CompanyEnrichQuery(c)
	if w.Code != http.StatusNotFound || f.calls != 1 || f.org != wantOrg || f.id != wantID {
		t.Fatalf("wrong tenant dispatch status=%d calls=%d", w.Code, f.calls)
	}
}

func TestCompanyEnrichHandlerRejectsInvalidEnvelopesBeforeConnection(t *testing.T) {
	for _, body := range []string{``, `{}`, `{"operation":"count_companies","organization_id":"foreign"}`, `{"operation":"count_companies","body":[]}`, `{"operation":"count_companies"} {}`, `{"operation":"count_companies","body":{"x":"` + strings.Repeat("x", companyenrich.MaxRequestBytes) + `"}}`} {
		// An empty object is syntactically valid; operation validation belongs
		// to the service, so test it separately from the binding boundary.
		if body == `{}` {
			continue
		}
		f := &companyEnrichConnectionRecorder{}
		h := &Handler{CompanyEnrichService: companyenrich.NewService(companyenrich.Deps{Integrations: f})}
		c, w := companyEnrichContext(body)
		h.CompanyEnrichQuery(c)
		if w.Code != http.StatusBadRequest || f.calls != 0 {
			t.Fatalf("invalid envelope reached connection: status=%d calls=%d", w.Code, f.calls)
		}
	}
}

func TestCompanyEnrichHandlerRequiresActorAndConnectionID(t *testing.T) {
	for _, missing := range []string{"org", "actor", "connection"} {
		t.Run(missing, func(t *testing.T) {
			f := &companyEnrichConnectionRecorder{}
			h := &Handler{CompanyEnrichService: companyenrich.NewService(companyenrich.Deps{Integrations: f})}
			c, w := companyEnrichContext(`{"operation":"count_companies"}`)
			switch missing {
			case "org":
				delete(c.Keys, middleware.OrganizationIDKey)
			case "actor":
				delete(c.Keys, middleware.UserIDKey)
			case "connection":
				c.Params[0].Value = "bad"
			}
			h.CompanyEnrichQuery(c)
			if w.Code < 400 || f.calls != 0 {
				t.Fatalf("missing actor accepted: status=%d calls=%d", w.Code, f.calls)
			}
		})
	}
}

func TestCompanyEnrichBindingPreservesInt64AndStrictBoolean(t *testing.T) {
	c, w := companyEnrichContext(`{"operation":"get_person","query":{"id":9223372036854775807}}`)
	var in models.CompanyEnrichQuery
	if !bindCompanyEnrich(c, &in) || in.Query["id"] != json.Number("9223372036854775807") {
		t.Fatalf("ID lost: %v status=%d", in, w.Code)
	}
	for _, value := range []string{`"true"`, `1`} {
		c, w := companyEnrichContext(`{"contact_id":"47c09930-62db-4281-8627-61d92920c471","kind":"person","overwrite":` + value + `}`)
		var in models.CompanyEnrichContactRequest
		if bindCompanyEnrich(c, &in) || w.Code != 400 {
			t.Fatal("coerced overwrite")
		}
	}
}

func TestCompanyEnrichEnrichmentRejectsCallerIdentityFields(t *testing.T) {
	f := &companyEnrichConnectionRecorder{}
	h := &Handler{CompanyEnrichService: companyenrich.NewService(companyenrich.Deps{Integrations: f})}
	c, w := companyEnrichContext(`{"contact_id":"47c09930-62db-4281-8627-61d92920c471","kind":"person","email":"other@example.com"}`)
	h.EnrichCompanyEnrichContact(c)
	if w.Code != 400 || f.calls != 0 {
		t.Fatal("caller supplied a contact lookup identity")
	}
}
