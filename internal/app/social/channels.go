package social

import (
	"context"
	"slices"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// catalogTTL is how long GraphKit's channel catalog is reused before it is read again.
const catalogTTL = 10 * time.Minute

// Channels is GET /v1/social/channels.
type Channels struct {
	Channels []graphkit.SocialChannel `json:"channels"`
}

// Channels is GraphKit's channel catalog, cached; the built-in matrix answers when GraphKit cannot.
func (s *Service) Channels(ctx context.Context) *Channels {
	return &Channels{Channels: s.catalog(ctx)}
}

func (s *Service) catalog(ctx context.Context) []graphkit.SocialChannel {
	s.mu.Lock()
	cached, at := s.channels, s.channelsAt
	s.mu.Unlock()
	if len(cached) > 0 && s.now().Sub(at) < catalogTTL {
		return cached
	}
	if s.available() {
		rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		p, err := s.api().Pricing(rctx)
		cancel()
		if err == nil && p != nil && p.Social != nil && len(p.Social.Channels) > 0 {
			s.mu.Lock()
			s.channels, s.channelsAt = p.Social.Channels, s.now()
			s.mu.Unlock()
			return p.Social.Channels
		}
		if err != nil {
			log.Debug().Err(err).Msg("social: reading the channel catalog failed; answering from the built-in one")
		}
	}
	if len(cached) > 0 {
		return cached
	}
	return builtinCatalog()
}

// builtinCatalog is the channel matrix Warmbly ships with, in GraphKit's shape.
func builtinCatalog() []graphkit.SocialChannel {
	out := make([]graphkit.SocialChannel, 0, len(models.SocialChannels))
	for _, ch := range models.SocialChannels {
		spec := models.SocialChannelSpecs[ch]
		c := graphkit.SocialChannel{Channel: ch, Label: spec.Label, AuthMethods: spec.AuthMethods, Actions: spec.Actions,
			Signals: spec.Signals, ConsentRequired: spec.ConsentRequired, ConsentBases: nonNil(spec.ConsentBases),
			MessageMaxChars: spec.MessageMaxChars, CommentMaxChars: spec.CommentMaxChars, VoiceNoteMaxSeconds: spec.VoiceNoteMaxSeconds,
			VoiceScriptMaxChars: spec.VoiceScriptMaxChars, InviteNoteMaxChars: spec.InviteNoteMaxChars, InmailSubjectMax: spec.InmailSubjectMax,
			InmailBodyMax: spec.InmailBodyMax, MaxPostAgeDays: spec.MaxPostAgeDays, Reactions: nonNil(spec.Reactions)}
		if len(spec.CookieRequired) > 0 {
			c.CookieNames = &graphkit.SocialCookieNames{Required: spec.CookieRequired, Optional: nonNil(spec.CookieOptional)}
		}
		out = append(out, c)
	}
	return out
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// channelSpec is one channel of the catalog, false when it has none of that name.
func (s *Service) channelSpec(ctx context.Context, channel string) (graphkit.SocialChannel, bool) {
	for _, c := range s.catalog(ctx) {
		if c.Channel == channel {
			return c, true
		}
	}
	return graphkit.SocialChannel{}, false
}

// offers reports whether a catalog entry lists v.
func offers(list []string, v string) bool { return slices.Contains(list, v) }

// BuiltinChannels is the catalog an instance without the add-on answers with.
func BuiltinChannels() *Channels {
	return &Channels{Channels: builtinCatalog()}
}
