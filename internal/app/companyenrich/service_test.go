package companyenrich

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type fakeConnections struct {
	org, id  uuid.UUID
	provider models.IntegrationProvider
	health   []models.IntegrationHealth
	status   []models.IntegrationStatus
	foreign  bool
}

func (f *fakeConnections) ConnectionConfig(_ context.Context, org, id uuid.UUID) (map[string]any, *models.IntegrationConnection, error) {
	if org != f.org || id != f.id {
		return nil, nil, integration.ErrConnectionNotFound
	}
	owner := f.org
	if f.foreign {
		owner = uuid.New()
	}
	return map[string]any{"api_key": "private-key"}, &models.IntegrationConnection{ID: f.id, OrganizationID: owner, Provider: f.provider, Status: models.IntegrationStatusConnected}, nil
}
func (f *fakeConnections) MarkConnectionHealth(_ context.Context, _ uuid.UUID, status models.IntegrationStatus, health models.IntegrationHealth, _ string) {
	f.health = append(f.health, health)
	f.status = append(f.status, status)
}

type fakeContacts struct {
	org           uuid.UUID
	row           models.Contact
	reads, writes int
	changeOnRead  int
	conflict      bool
	patch         *models.CompanyEnrichProfilePatch
}

func (f *fakeContacts) GetByIDsAndOrganization(_ context.Context, org uuid.UUID, ids []uuid.UUID) ([]models.Contact, *errx.Error) {
	f.reads++
	if org != f.org || len(ids) != 1 || ids[0] != f.row.ID {
		return nil, nil
	}
	if f.reads == f.changeOnRead {
		f.row.Email = "changed@example.org"
		f.row.UpdatedAt = f.row.UpdatedAt.Add(time.Second)
	}
	return []models.Contact{f.row}, nil
}
func (f *fakeContacts) ApplyCompanyEnrichProfile(_ context.Context, org, id uuid.UUID, expected time.Time, email string, p *models.CompanyEnrichProfilePatch) *errx.Error {
	f.writes++
	f.patch = p
	if f.conflict || org != f.org || id != f.row.ID || expected != f.row.UpdatedAt || email != f.row.Email {
		return errx.NewWithIdentifier(errx.Conflict, "companyenrich_contact_changed", "contact changed")
	}
	if p.FirstName != nil {
		f.row.FirstName = *p.FirstName
	}
	if p.LastName != nil {
		f.row.LastName = *p.LastName
	}
	if p.Company != nil {
		f.row.Company = *p.Company
	}
	for k, v := range p.CustomFields {
		f.row.CustomFields[k] = v
	}
	return nil
}

func serviceFixture(t *testing.T, body string) (*Service, *fakeConnections, *fakeContacts, *int) {
	t.Helper()
	connections := &fakeConnections{org: uuid.New(), id: uuid.New(), provider: models.IntegrationCompanyEnrich}
	contacts := &fakeContacts{org: connections.org, row: models.Contact{ID: uuid.New(), Email: "person@example.com", FirstName: "Existing", Company: "Existing Company", Subscribed: false, VerificationStatus: "invalid", Phone: "123", CustomFields: map[string]string{"preserved": "value"}, UpdatedAt: time.Now()}}
	calls := 0
	s := NewService(Deps{Integrations: connections, Contacts: contacts})
	s.newClient = func(key string) *Client {
		if key != "private-key" {
			t.Fatal("wrong credential")
		}
		return mockClient(t, func(req *http.Request) (*http.Response, error) {
			calls++
			if req.URL.Host != "api.companyenrich.com" {
				t.Fatal("wrong host")
			}
			return response(200, body), nil
		})
	}
	return s, connections, contacts, &calls
}

func TestCompanyEnrichCredentialCheckIsFreeAndPrivate(t *testing.T) {
	s := NewService(Deps{})
	calls := 0
	s.newClient = func(key string) *Client {
		if key != "secret" {
			t.Fatal("untrimmed key")
		}
		return mockClient(t, func(req *http.Request) (*http.Response, error) {
			calls++
			if req.Method != "GET" || req.URL.Path != "/me" || req.URL.RawQuery != "" {
				t.Fatalf("paid credential check: %s", req.URL)
			}
			return response(200, `{"userId":"47c09930-62db-4281-8627-61d92920c471","apiKey":"secret","credits":{"balance":123},"private":{"token":"hidden"}}`), nil
		})
	}
	cfg := map[string]any{"api_key": " secret "}
	facts, err := s.CredentialCheck(context.Background(), cfg)
	if err != nil || calls != 1 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
	if len(facts) != 2 || facts["account_name"] != "CompanyEnrich" || cfg["api_key"] != "secret" {
		t.Fatalf("facts=%v", facts)
	}
	raw, _ := json.Marshal(facts)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "credits") {
		t.Fatal("private credential-check response leaked")
	}
}

func TestCompanyEnrichConnectionOwnershipBeforeProviderRequest(t *testing.T) {
	for _, scenario := range []string{"foreign-org", "foreign-id", "foreign-provider", "defensive-org", "missing-org"} {
		t.Run(scenario, func(t *testing.T) {
			s, c, _, calls := serviceFixture(t, `0`)
			org, id := c.org, c.id
			switch scenario {
			case "foreign-org":
				org = uuid.New()
			case "foreign-id":
				id = uuid.New()
			case "foreign-provider":
				c.provider = models.IntegrationApollo
			case "defensive-org":
				c.foreign = true
			case "missing-org":
				org = uuid.Nil
			}
			_, err := s.Query(context.Background(), org, id, models.CompanyEnrichQuery{Operation: "count_companies"})
			var xe *errx.Error
			if !errors.As(err, &xe) || xe.Code != errx.NotFound || *calls != 0 || len(c.health) != 0 {
				t.Fatalf("tenant check err=%v calls=%d", err, *calls)
			}
		})
	}
}

func TestCompanyEnrichMarksHealthWithoutRetrying(t *testing.T) {
	s, c, _, calls := serviceFixture(t, `0`)
	s.newClient = func(string) *Client {
		return mockClient(t, func(*http.Request) (*http.Response, error) {
			*calls++
			return response(401, `{"error":"private-key"}`), nil
		})
	}
	_, err := s.Query(context.Background(), c.org, c.id, models.CompanyEnrichQuery{Operation: "count_companies"})
	var xe *errx.Error
	if !errors.As(err, &xe) || xe.Identifier != "companyenrich_key_rejected" || *calls != 1 || len(c.status) != 1 || c.status[0] != models.IntegrationStatusReauthRequired {
		t.Fatalf("unexpected status/error %v %v", c.status, err)
	}
}

func TestCompanyEnrichContactOwnershipBeforePaidLookup(t *testing.T) {
	s, c, contacts, calls := serviceFixture(t, `{"id":1}`)
	_, err := s.EnrichContact(context.Background(), c.org, c.id, uuid.New(), models.CompanyEnrichContactRequest{ContactID: uuid.New(), Kind: "person"})
	if err == nil || *calls != 0 || contacts.writes != 0 {
		t.Fatalf("foreign contact: err=%v calls=%d", err, *calls)
	}
}

func TestCompanyEnrichContactPreservesIdentityConsentAndMembership(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "fill blanks", true: "overwrite profile"}[overwrite], func(t *testing.T) {
			s, c, contacts, calls := serviceFixture(t, `{"id":9223372036854775807,"first_name":"New","last_name":"Person","position":"CTO","email":"different@example.org","subscribed":true,"experiences":[{"isCurrent":true,"company":{"id":"company-id","name":"New Company","domain":"example.org"}}]}`)
			campaigns := []models.MiniCampaign{{ID: uuid.NewString(), Name: "Existing campaign"}}
			contacts.row.Campaigns = campaigns
			out, err := s.EnrichContact(context.Background(), c.org, c.id, uuid.New(), models.CompanyEnrichContactRequest{ContactID: contacts.row.ID, Kind: "person", Overwrite: overwrite})
			if err != nil || *calls != 1 || contacts.writes != 1 {
				t.Fatalf("calls=%d writes=%d err=%v", *calls, contacts.writes, err)
			}
			wantName, wantCompany := "Existing", "Existing Company"
			if overwrite {
				wantName, wantCompany = "New", "New Company"
			}
			if out.Contact.FirstName != wantName || out.Contact.Company != wantCompany || out.Contact.LastName != "Person" {
				t.Fatalf("wrong merge %+v", out.Contact)
			}
			if out.Contact.Email != "person@example.com" || out.Contact.Subscribed || out.Contact.VerificationStatus != "invalid" || out.Contact.Phone != "123" || !reflect.DeepEqual(out.Contact.Campaigns, campaigns) {
				t.Fatal("protected contact fields changed")
			}
			if out.Contact.CustomFields["preserved"] != "value" || out.Contact.CustomFields["companyenrich_person_id"] != "9223372036854775807" {
				t.Fatal("custom data or exact ID lost")
			}
		})
	}
}

func TestCompanyEnrichRefusesChangedContactWithoutPaidRetry(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(map[bool]string{false: "during lookup", true: "during save"}[concurrent], func(t *testing.T) {
			s, c, contacts, calls := serviceFixture(t, `{"first_name":"New","last_name":"Person","id":1}`)
			if concurrent {
				contacts.conflict = true
			} else {
				contacts.changeOnRead = 2
			}
			_, err := s.EnrichContact(context.Background(), c.org, c.id, uuid.New(), models.CompanyEnrichContactRequest{ContactID: contacts.row.ID, Kind: "person"})
			var xe *errx.Error
			if !errors.As(err, &xe) || xe.Code != errx.Conflict || *calls != 1 || contacts.row.LastName != "" {
				t.Fatalf("changed contact err=%v calls=%d", err, *calls)
			}
		})
	}
}

func TestCompanyEnrichCompanyMappingAndDomainPriority(t *testing.T) {
	s, c, contacts, calls := serviceFixture(t, `{"id":"47c09930-62db-4281-8627-61d92920c471","name":"Example","domain":"example.org","website":"https://example.org","industry":"Software","employees":"11-50","founded_year":2001,"socials":{"linkedin_url":"javascript:alert(1)"}}`)
	contacts.row.CustomFields["company_domain"] = "https://www.example.org/about"
	s.newClient = func(string) *Client {
		return mockClient(t, func(req *http.Request) (*http.Response, error) {
			*calls++
			if req.URL.Query().Get("domain") != "www.example.org" {
				t.Fatal("did not use stored company domain")
			}
			return response(200, `{"name":"Example","domain":"example.org","industry":"Software","founded_year":2001}`), nil
		})
	}
	out, err := s.EnrichContact(context.Background(), c.org, c.id, uuid.New(), models.CompanyEnrichContactRequest{ContactID: contacts.row.ID, Kind: "company"})
	if err != nil || *calls != 1 || out.Contact.Company != "Existing Company" || out.Contact.CustomFields["companyenrich_founded_year"] != "2001" {
		t.Fatalf("mapping err=%v", err)
	}
	if safeProfileURL("javascript:alert(1)") != "" {
		t.Fatal("unsafe stored URL")
	}
	for _, raw := range []string{"http://user:pass@example.com", "127.0.0.1", "localhost", "example.com:123"} {
		if companyDomain(raw) != "" {
			t.Fatalf("accepted domain %s", raw)
		}
	}
}
