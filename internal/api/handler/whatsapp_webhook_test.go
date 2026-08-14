package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/whatsapp"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type memWARepo struct {
	instances map[string]*models.WhatsAppInstance
	events    map[string]bool
	messages  int
	states    int
}

func (m *memWARepo) GetInstanceByName(_ context.Context, provider, instance string) (*models.WhatsAppInstance, *errx.Error) {
	if m.instances == nil {
		return nil, nil
	}
	return m.instances[provider+":"+instance], nil
}
func (m *memWARepo) InsertWebhookEvent(_ context.Context, orgID uuid.UUID, provider, idemKey, eventType, externalMsgID, payloadHash string) (bool, *errx.Error) {
	if m.events == nil {
		m.events = map[string]bool{}
	}
	k := orgID.String() + ":" + idemKey
	if m.events[k] {
		return false, nil
	}
	m.events[k] = true
	return true, nil
}
func (m *memWARepo) UpsertContactState(context.Context, *models.WhatsAppContactState) *errx.Error {
	m.states++
	return nil
}
func (m *memWARepo) GetContactStateByPhone(context.Context, uuid.UUID, string) (*models.WhatsAppContactState, *errx.Error) {
	return nil, nil
}
func (m *memWARepo) GetContactStateByContact(context.Context, uuid.UUID, uuid.UUID) (*models.WhatsAppContactState, *errx.Error) {
	return nil, nil
}
func (m *memWARepo) InsertMessage(context.Context, *models.WhatsAppMessage) (bool, *errx.Error) {
	m.messages++
	return true, nil
}
func (m *memWARepo) GetTemplateStatus(context.Context, uuid.UUID, string, string) (string, *errx.Error) {
	return "MISSING", nil
}
func (m *memWARepo) ListMessagesByThread(context.Context, uuid.UUID, string, int) ([]models.WhatsAppMessage, *errx.Error) {
	return nil, nil
}

func TestEvolutionWebhookUnmappedInstance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wa := whatsapp.NewService(whatsapp.Config{
		Enabled: true, WebhookSecret: "sec", MaxWebhookBytes: 1 << 20,
	}, whatsapp.NewMockProvider(), nil)
	h := &Handler{
		WhatsAppService: wa,
		WhatsAppRepo:    &memWARepo{},
	}
	r := gin.New()
	r.POST("/api/v1/webhooks/evolution/:instance", h.EvolutionWebhook)

	body := []byte(`{"event":"messages.upsert","instance":"unknown","data":{"key":{"id":"1","remoteJid":"5548@s.whatsapp.net","fromMe":false},"message":{"conversation":"hi"}}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/evolution/unknown", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sec")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"] != "instance_mismatch" {
		t.Fatalf("resp=%v", resp)
	}
}

func TestEvolutionWebhookMappedIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	org := uuid.New()
	repo := &memWARepo{instances: map[string]*models.WhatsAppInstance{
		"evolution:confenge": {
			ID: uuid.New(), OrganizationID: org, Provider: "evolution",
			InstanceName: "confenge", WebhookSecret: "sec", IntegrationMode: "WHATSAPP-BUSINESS",
		},
	}}
	wa := whatsapp.NewService(whatsapp.Config{
		Enabled: true, WebhookSecret: "sec", MaxWebhookBytes: 1 << 20, ServiceWindow: 24 * time.Hour,
	}, whatsapp.NewMockProvider(), nil)
	h := &Handler{WhatsAppService: wa, WhatsAppRepo: repo}
	r := gin.New()
	r.POST("/api/v1/webhooks/evolution/:instance", h.EvolutionWebhook)

	body := []byte(`{"event":"messages.upsert","instance":"confenge","data":{"key":{"id":"MSG99","remoteJid":"5548999887766@s.whatsapp.net","fromMe":false},"message":{"conversation":"Olá"},"messageTimestamp":1710000000}}`)
	do := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/evolution/confenge", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer sec")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	w1 := do()
	if w1.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", w1.Code, w1.Body.String())
	}
	if repo.messages != 1 {
		t.Fatalf("messages=%d", repo.messages)
	}
	w2 := do()
	if w2.Code != http.StatusOK {
		t.Fatalf("second status=%d", w2.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp["duplicate"] != true {
		t.Fatalf("expected duplicate resp=%v", resp)
	}
	if repo.messages != 1 {
		t.Fatalf("duplicate must not insert again messages=%d", repo.messages)
	}
}

func TestEvolutionWebhookInvalidSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	org := uuid.New()
	repo := &memWARepo{instances: map[string]*models.WhatsAppInstance{
		"evolution:confenge": {OrganizationID: org, Provider: "evolution", InstanceName: "confenge", WebhookSecret: "sec"},
	}}
	h := &Handler{
		WhatsAppService: whatsapp.NewService(whatsapp.Config{Enabled: true, MaxWebhookBytes: 1 << 20}, nil, nil),
		WhatsAppRepo:    repo,
	}
	r := gin.New()
	r.POST("/api/v1/webhooks/evolution/:instance", h.EvolutionWebhook)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/evolution/confenge", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
}
