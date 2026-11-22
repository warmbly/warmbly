package wmail

import (
	"context"
	"errors"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/repository"
)

// mapPage is the message map's answers for one delta or history page, asked in batches.
type mapPage struct {
	// known holds every id the batch answered; a nil value is an id the map does not hold.
	known map[string]*repository.EmailMessageData
	// regular is a Graph item the map holds only under its regular id, still to be aliased.
	regular map[string]*repository.EmailMessageData
}

// prefetchMaps resolves a page's ids up front; a failure leaves each item to its own lookup.
func (w *WMail) prefetchMaps(ctx context.Context, ids []string) {
	w.mapPage = mapPage{}
	if w.EmailMessageMapRepository == nil {
		return
	}
	wanted := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" {
			wanted = append(wanted, id)
		}
	}
	if len(wanted) == 0 {
		return
	}
	found, err := repository.GetEmailMessageMaps(ctx, w.EmailMessageMapRepository, w.UserID, w.ID, wanted)
	if err != nil {
		log.Debug().Err(err).Str("email_id", w.ID.String()).Msg("sync: batch message lookup failed; looking up one at a time")
		return
	}
	known := make(map[string]*repository.EmailMessageData, len(wanted))
	for _, id := range wanted {
		known[id] = found[id]
	}
	w.mapPage.known = known
}

// graphPrefetch also resolves, in one translation, the regular ids of items the map does not hold under their immutable one.
func (w *WMail) graphPrefetch(ctx context.Context, ids []string) {
	w.prefetchMaps(ctx, ids)
	if w.mapPage.known == nil || !w.GraphData.Client.ImmutableIDMode() {
		return
	}
	var unknown []string
	for _, id := range ids {
		if known, ok := w.mapPage.known[id]; ok && known == nil {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) == 0 {
		return
	}
	// An item without an answer here takes the single lookup path, which reports its own failure.
	converted, err := w.GraphData.Client.RegularMessageIDs(ctx, unknown)
	if err != nil && !errors.Is(err, msgraph.ErrIncompleteIDConversion) {
		converted = nil
	}
	regularIDs := make([]string, 0, len(unknown))
	for _, id := range unknown {
		if converted[id] == "" {
			delete(w.mapPage.known, id)
			continue
		}
		regularIDs = append(regularIDs, converted[id])
	}
	if len(regularIDs) == 0 {
		return
	}
	found, err := repository.GetEmailMessageMaps(ctx, w.EmailMessageMapRepository, w.UserID, w.ID, regularIDs)
	if err != nil {
		for _, id := range unknown {
			delete(w.mapPage.known, id)
		}
		return
	}
	w.mapPage.regular = map[string]*repository.EmailMessageData{}
	for _, id := range unknown {
		if r := converted[id]; r != "" && found[r] != nil {
			w.mapPage.regular[id] = found[r]
		}
	}
}

// lookupMap answers from the current page when it can and asks the control plane otherwise.
func (w *WMail) lookupMap(ctx context.Context, id string) (*repository.EmailMessageData, error) {
	if known, ok := w.mapPage.known[id]; ok {
		return known, nil
	}
	return w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, id)
}

// forgetMap drops a page answer the caller is about to make stale.
func (w *WMail) forgetMap(id string) {
	delete(w.mapPage.known, id)
	delete(w.mapPage.regular, id)
}
