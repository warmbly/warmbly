// Package leadfinder finds local businesses on a map through pluggable place
// providers, reads their websites for contact details and turns them into
// contacts.
package leadfinder

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

// Query is one page of a provider search.
type Query struct {
	Text     string
	Category *Category
	Area     models.LeadArea
	// Cursor is the provider's own token for the next page; empty for the first.
	Cursor   string
	Limit    int
	Language string
}

// Page is one page of results and the cursor to the next, empty when done.
type Page struct {
	Leads      []models.Lead
	NextCursor string
}

// Provider searches one place database.
type Provider interface {
	ID() models.LeadProvider
	Search(ctx context.Context, q Query) (*Page, error)
}

// ProviderInfo describes a provider to the dashboard.
type ProviderInfo struct {
	ID          models.LeadProvider `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	// Available is true when a search can run: free, or a key is configured.
	Available bool `json:"available"`
	// KeySource is "none", "workspace" or "instance".
	KeySource   string   `json:"key_source"`
	NeedsKey    bool     `json:"needs_key"`
	PageSize    int      `json:"page_size"`
	MaxPages    int      `json:"max_pages"`
	CreditCost  int      `json:"credit_cost"`
	Fields      []string `json:"fields"`
	Integration string   `json:"integration,omitempty"`
}

// ErrProviderKey means the provider refused the API key.
var ErrProviderKey = errors.New("the provider refused the API key")

// ErrProviderQuota means the provider is rate limiting or out of quota.
var ErrProviderQuota = errors.New("the provider is rate limiting or out of quota; try again later")

// ErrAreaTooLarge means the area is wider than the provider can search at once.
var ErrAreaTooLarge = errors.New("zoom in: the area is too large to search at once")

// providerHTTP is the client every provider talks through. Provider hosts are
// fixed public APIs or operator configuration, never request input.
var providerHTTP = &http.Client{Timeout: 45 * time.Second}

func statusErr(code int) error {
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return ErrProviderKey
	case code == http.StatusTooManyRequests || code == http.StatusPaymentRequired:
		return ErrProviderQuota
	}
	return errors.New("the provider answered " + http.StatusText(code))
}
