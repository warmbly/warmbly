package whatsapp

import (
	"crypto/subtle"
	"fmt"
	"strings"
)

// WebhookAuth validates inbound Evolution (or future provider) webhooks.
type WebhookAuth struct {
	// Secret is compared to Authorization: Bearer <secret> or X-Webhook-Secret.
	Secret string
	// InstanceAllow is the expected instance name (empty = any, then map later).
	InstanceAllow string
	// MaxBytes body size limit.
	MaxBytes int64
}

// AuthError is a webhook authentication / validation failure.
type AuthError struct {
	Code       string
	Message    string
	StatusCode int
}

func (e *AuthError) Error() string {
	if e == nil {
		return "webhook auth error"
	}
	return e.Message
}

// ValidateHeaders checks secret, content-type, and size before body parse.
func (a WebhookAuth) ValidateHeaders(authHeader, secretHeader, contentType string, contentLength int64) error {
	if a.MaxBytes <= 0 {
		a.MaxBytes = DefaultMaxWebhookBytes
	}
	if contentLength > a.MaxBytes {
		return &AuthError{Code: "payload_too_large", Message: "payload too large", StatusCode: 413}
	}
	if contentType != "" && !strings.Contains(strings.ToLower(contentType), "json") {
		return &AuthError{Code: "unsupported_media_type", Message: "content-type must be application/json", StatusCode: 415}
	}
	if a.Secret == "" {
		// Dev-only: allow empty secret when not configured (startup forbids in prod).
		return nil
	}
	token := bearerToken(authHeader)
	if token == "" {
		token = strings.TrimSpace(secretHeader)
	}
	if token == "" {
		return &AuthError{Code: "missing_secret", Message: "missing webhook secret", StatusCode: 401}
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(a.Secret)) != 1 {
		return &AuthError{Code: "invalid_secret", Message: "invalid webhook secret", StatusCode: 401}
	}
	return nil
}

// ValidateInstance ensures the event instance matches the configured mapping.
func (a WebhookAuth) ValidateInstance(instance string) error {
	if a.InstanceAllow == "" {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(instance), strings.TrimSpace(a.InstanceAllow)) {
		return &AuthError{Code: "instance_mismatch", Message: "instance not mapped", StatusCode: 404}
	}
	return nil
}

func bearerToken(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return ""
	}
	const p = "Bearer "
	if len(h) > len(p) && strings.EqualFold(h[:len(p)], p) {
		return strings.TrimSpace(h[len(p):])
	}
	return h
}

// MapInstanceToOrg is a simple static instance→org mapper for single-tenant deploys.
// Multi-tenant maps live in the repository when wired.
type InstanceMapping struct {
	Instance       string
	OrganizationID string // uuid string
	WebhookSecret  string
}

// ResolveInstance finds a mapping or errors.
func ResolveInstance(maps []InstanceMapping, instance string) (InstanceMapping, error) {
	instance = strings.TrimSpace(instance)
	for _, m := range maps {
		if strings.EqualFold(m.Instance, instance) {
			return m, nil
		}
	}
	return InstanceMapping{}, fmt.Errorf("instance not mapped: %s", instance)
}
