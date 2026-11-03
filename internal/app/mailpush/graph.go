package mailpush

import (
	"encoding/json"

	"github.com/google/uuid"
)

// graphLifecycleRenew are the lifecycle events that need the subscription
// renewed or reopened; "missed" only needs the sync pass every wake runs.
var graphLifecycleRenew = map[string]bool{
	"reauthorizationRequired": true,
	"subscriptionRemoved":     true,
}

// GraphPost is what one Graph notification POST for a mailbox asks for.
type GraphPost struct {
	// Authentic is set when at least one item carries the mailbox's client state.
	Authentic bool
	// Renew is set when an authentic item is a lifecycle event that needs the subscription refreshed.
	Renew bool
}

// ParseGraph reads a Graph notification POST for emailID. Items with the wrong
// client state are ignored, so a forged POST wakes nothing.
func (s *Settings) ParseGraph(emailID uuid.UUID, body []byte) (GraphPost, error) {
	var envelope struct {
		Value []struct {
			ClientState    string `json:"clientState"`
			LifecycleEvent string `json:"lifecycleEvent"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return GraphPost{}, err
	}
	var out GraphPost
	for _, item := range envelope.Value {
		if !s.VerifyGraphClientState(emailID, item.ClientState) {
			continue
		}
		out.Authentic = true
		if graphLifecycleRenew[item.LifecycleEvent] {
			out.Renew = true
		}
	}
	return out, nil
}
