package customhost

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

type ownersMap map[string][]uuid.UUID

func (m ownersMap) Owners(_ context.Context, host string) ([]uuid.UUID, error) { return m[host], nil }

// An agency portal host is owned by every client, so each client's token answers there and nobody else's does.
func TestServesConfinesCustomHostToItsWorkspaces(t *testing.T) {
	clientA, clientB, stranger := uuid.New(), uuid.New(), uuid.New()
	c := New(ownersMap{
		"portal.agency.com": {clientA, clientB},
		"forms.client.com":  {clientA},
	}, nil, nil)
	ctx := context.Background()

	cases := []struct {
		host string
		org  uuid.UUID
		want bool
	}{
		{"portal.agency.com", clientA, true},
		{"portal.agency.com", clientB, true},
		{"Portal.Agency.com:443", stranger, false},
		{"forms.client.com", clientA, true},
		{"forms.client.com", clientB, false},
		{"unclaimed.example.com", stranger, true},
		{"", stranger, true},
	}
	for _, tc := range cases {
		got, err := c.Serves(ctx, tc.host, tc.org)
		if err != nil || got != tc.want {
			t.Errorf("Serves(%q, %s) = %v, %v; want %v", tc.host, tc.org, got, err, tc.want)
		}
	}
}

func TestValidHost(t *testing.T) {
	for _, h := range []string{"portal.agency.com", "a-b.example.io"} {
		if !ValidHost(h) {
			t.Errorf("ValidHost(%q) = false", h)
		}
	}
	for _, h := range []string{"localhost", "-a.example.com", "a..com", "exa mple.com", "a_b.example.com"} {
		if ValidHost(h) {
			t.Errorf("ValidHost(%q) = true", h)
		}
	}
}
