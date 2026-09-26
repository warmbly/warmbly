package contact

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ResolveSelection turns a bulk action's selection into the contact ids it
// applies to. An explicit list passes through untouched; a "select all
// matching" resolves the same search the list ran, minus the rows unticked
// afterwards, so the action covers exactly what the user saw selected.
//
// The resolved set is capped: past models.MaxContactBulkSelection the action
// is refused rather than truncated, since acting on an arbitrary prefix of a
// selection is worse than acting on none of it.
func (s *contactService) ResolveSelection(ctx context.Context, orgID uuid.UUID, sel models.ContactSelection) ([]string, *errx.Error) {
	if !sel.All {
		return sel.Contacts, nil
	}
	if sel.Filters == nil {
		return nil, errx.New(errx.BadRequest, "a select-all request must carry the filters it applies to")
	}
	// The same contract as the list: a sort the list would have refused is
	// refused here too, rather than quietly acting on a different order.
	if xerr := validateSort(*sel.Filters); xerr != nil {
		return nil, xerr
	}
	if xerr := validateMailHosts(*sel.Filters); xerr != nil {
		return nil, xerr
	}

	ids, xerr := s.contactRepository.SearchIDs(ctx, orgID.String(), *sel.Filters, models.MaxContactBulkSelection)
	if xerr != nil {
		return nil, xerr
	}
	if len(ids) > models.MaxContactBulkSelection {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "selection_too_large",
			fmt.Sprintf("that selection matches more than %d contacts; narrow it with a filter and try again", models.MaxContactBulkSelection))
	}

	// The cap above bounds the resolved set, not the request body: an
	// exclusion list is caller-supplied and sized before anything is read.
	if len(sel.Exclude) > models.MaxContactBulkSelection {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "too_many_contacts",
			fmt.Sprintf("too many exclusions, maximum is %d", models.MaxContactBulkSelection))
	}
	excluded := make(map[uuid.UUID]struct{}, len(sel.Exclude))
	for _, raw := range sel.Exclude {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return nil, errx.ErrUuid
		}
		excluded[id] = struct{}{}
	}

	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, skip := excluded[id]; skip {
			continue
		}
		out = append(out, id.String())
	}
	return out, nil
}
