package evolution

import (
	"context"
	"fmt"
	"time"

	"github.com/warmbly/warmbly/internal/app/whatsapp"
)

// Provider implements whatsapp.Provider using Evolution API (Cloud API first).
type Provider struct {
	client   *Client
	instance string
}

// NewProvider wraps a Client as a domain Provider.
func NewProvider(client *Client, defaultInstance string) *Provider {
	return &Provider{client: client, instance: defaultInstance}
}

// Name implements whatsapp.Provider.
func (p *Provider) Name() string { return whatsapp.ProviderEvolution }

func (p *Provider) resolveInstance(instance string) string {
	if instance != "" {
		return instance
	}
	return p.instance
}

// SendText implements whatsapp.Provider.
func (p *Provider) SendText(ctx context.Context, req whatsapp.SendTextRequest) (*whatsapp.SendResult, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("evolution provider not configured")
	}
	number := whatsapp.DigitsOnly(req.ToE164)
	res, err := p.client.SendText(ctx, p.resolveInstance(req.Instance), number, req.Body)
	if err != nil {
		return nil, err
	}
	id := res.Key.ID
	if id == "" {
		id = res.MessageID
	}
	return &whatsapp.SendResult{
		ProviderMessageID: id,
		Status:            "sent",
		RawStatus:         res.Status,
		OccurredAt:        time.Now().UTC(),
	}, nil
}

// SendTemplate implements whatsapp.Provider.
func (p *Provider) SendTemplate(ctx context.Context, req whatsapp.SendTemplateRequest) (*whatsapp.SendResult, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("evolution provider not configured")
	}
	number := whatsapp.DigitsOnly(req.ToE164)
	res, err := p.client.SendTemplate(ctx, p.resolveInstance(req.Instance), number, req.TemplateName, req.Language, req.Variables)
	if err != nil {
		return nil, err
	}
	id := res.Key.ID
	if id == "" {
		id = res.MessageID
	}
	return &whatsapp.SendResult{
		ProviderMessageID: id,
		Status:            "sent",
		RawStatus:         res.Status,
		OccurredAt:        time.Now().UTC(),
	}, nil
}

// GetConnectionStatus implements whatsapp.Provider.
func (p *Provider) GetConnectionStatus(ctx context.Context, instance string) (*whatsapp.ConnectionStatus, error) {
	st, err := p.client.GetConnectionState(ctx, p.resolveInstance(instance))
	if err != nil {
		return nil, err
	}
	return &whatsapp.ConnectionStatus{
		Instance:  st.Instance,
		State:     st.State,
		UpdatedAt: time.Now().UTC(),
	}, nil
}

// ConfigureWebhook implements whatsapp.Provider.
func (p *Provider) ConfigureWebhook(ctx context.Context, instance string, cfg whatsapp.WebhookConfig) error {
	events := cfg.Events
	if len(events) == 0 {
		events = []string{
			"MESSAGES_UPSERT",
			"MESSAGES_UPDATE",
			"SEND_MESSAGE",
			"CONNECTION_UPDATE",
		}
	}
	return p.client.SetWebhook(ctx, p.resolveInstance(instance), cfg.URL, cfg.Secret, events, cfg.Enabled)
}

// Health implements whatsapp.Provider.
func (p *Provider) Health(ctx context.Context) error {
	return p.client.Health(ctx)
}
