package repository

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

func TestAPIKeyCanAuthenticate(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	cases := []struct {
		name string
		key  models.APIKey
		want bool
	}{
		{"active, no expiry", models.APIKey{Status: models.APIKeyStatusActive}, true},
		{"active, expires later", models.APIKey{Status: models.APIKeyStatusActive, ExpiresAt: &future}, true},
		{"active, already expired", models.APIKey{Status: models.APIKeyStatusActive, ExpiresAt: &past}, false},
		{"revoked", models.APIKey{Status: models.APIKeyStatusRevoked}, false},
		{"revoked, expires later", models.APIKey{Status: models.APIKeyStatusRevoked, ExpiresAt: &future}, false},
		{"expired", models.APIKey{Status: models.APIKeyStatusExpired}, false},
	}
	for _, tc := range cases {
		if got := tc.key.CanAuthenticate(); got != tc.want {
			t.Errorf("%s: CanAuthenticate() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
