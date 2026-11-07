package leadfinder

import (
	"context"
	"errors"
	"strings"

	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/models"
)

// KeyCheckRegistry is where Connect looks up the check for a pasted key.
type KeyCheckRegistry interface {
	SetCredentialCheck(provider models.IntegrationProvider, check integration.CredentialCheck)
}

// RegisterKeyChecks makes Connect prove a provider key works before saving it.
func RegisterKeyChecks(reg KeyCheckRegistry) {
	if reg == nil {
		return
	}
	check := func(name string, probe func(ctx context.Context, key string) error) integration.CredentialCheck {
		return func(ctx context.Context, config map[string]any) (map[string]any, error) {
			key, _ := config["api_key"].(string)
			key = strings.TrimSpace(key)
			if key == "" {
				return nil, errors.New("paste your " + name + " API key")
			}
			config["api_key"] = key
			if err := probe(ctx, key); err != nil {
				if errors.Is(err, ErrProviderKey) {
					return nil, errors.New(name + " refused this key")
				}
				return nil, err
			}
			return map[string]any{}, nil
		}
	}
	reg.SetCredentialCheck(models.IntegrationGooglePlaces, check("Google Places", CheckGoogleKey))
	reg.SetCredentialCheck(models.IntegrationFoursquare, check("Foursquare", CheckFoursquareKey))
	reg.SetCredentialCheck(models.IntegrationOutscraper, check("Outscraper", CheckOutscraperKey))
}
