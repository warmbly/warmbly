package whatsapp

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MockProvider records sends for tests. Never reaches a real network.
type MockProvider struct {
	mu        sync.Mutex
	Sends     []MockSend
	Templates []MockSend
	HealthErr error
	SendErr   error
	Status    ConnectionStatus
}

// MockSend is one recorded outbound attempt.
type MockSend struct {
	Kind           string // text | template
	ToE164         string
	Body           string
	TemplateName   string
	Language       string
	Variables      []string
	IdempotencyKey string
	OrganizationID uuid.UUID
	At             time.Time
}

// NewMockProvider returns a ready mock.
func NewMockProvider() *MockProvider {
	return &MockProvider{
		Status: ConnectionStatus{Instance: "mock", State: "open", UpdatedAt: time.Now().UTC()},
	}
}

func (m *MockProvider) Name() string { return ProviderMock }

func (m *MockProvider) SendText(_ context.Context, req SendTextRequest) (*SendResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.SendErr != nil {
		return nil, m.SendErr
	}
	m.Sends = append(m.Sends, MockSend{
		Kind:           "text",
		ToE164:         req.ToE164,
		Body:           req.Body,
		IdempotencyKey: req.IdempotencyKey,
		OrganizationID: req.OrganizationID,
		At:             time.Now().UTC(),
	})
	id := uuid.New().String()
	return &SendResult{ProviderMessageID: id, Status: "sent", RawStatus: "MOCK_SENT", OccurredAt: time.Now().UTC()}, nil
}

func (m *MockProvider) SendTemplate(_ context.Context, req SendTemplateRequest) (*SendResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.SendErr != nil {
		return nil, m.SendErr
	}
	m.Templates = append(m.Templates, MockSend{
		Kind:           "template",
		ToE164:         req.ToE164,
		TemplateName:   req.TemplateName,
		Language:       req.Language,
		Variables:      append([]string(nil), req.Variables...),
		IdempotencyKey: req.IdempotencyKey,
		OrganizationID: req.OrganizationID,
		At:             time.Now().UTC(),
	})
	id := uuid.New().String()
	return &SendResult{ProviderMessageID: id, Status: "sent", RawStatus: "MOCK_TEMPLATE_SENT", OccurredAt: time.Now().UTC()}, nil
}

func (m *MockProvider) GetConnectionStatus(_ context.Context, instance string) (*ConnectionStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.Status
	if instance != "" {
		s.Instance = instance
	}
	return &s, nil
}

func (m *MockProvider) ConfigureWebhook(context.Context, string, WebhookConfig) error { return nil }

func (m *MockProvider) Health(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.HealthErr
}

// SendCount returns total text+template sends.
func (m *MockProvider) SendCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Sends) + len(m.Templates)
}

// Reset clears recorded sends.
func (m *MockProvider) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Sends = nil
	m.Templates = nil
	m.SendErr = nil
}
