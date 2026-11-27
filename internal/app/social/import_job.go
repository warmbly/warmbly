package social

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
)

// EmailFinder is the lead finder's person email search, charged per address it finds.
type EmailFinder interface {
	FindWorkEmail(ctx context.Context, orgID uuid.UUID, first, last, website, key string) (string, error)
	RefundWorkEmail(ctx context.Context, orgID uuid.UUID, key string)
}

const (
	// importStale is how long a running import may go unsaved before another pass takes it over.
	importStale = 10 * time.Minute
	// importSaveEvery is how many people pass between progress saves.
	importSaveEvery = 10
)

var errFinderUnavailable = newErr(errx.ServiceUnavailable, "linkedin_email_finder_unavailable", "Finding work emails is not available on this instance.")

// finderBox holds a finder wired after construction.
type finderBox struct{ f EmailFinder }

// WireFinder attaches the work email finder once the lead finder is built.
func (s *Service) WireFinder(f EmailFinder) {
	s.finderRef.Store(&finderBox{f: f})
}

func (s *Service) finder() EmailFinder {
	if b := s.finderRef.Load(); b != nil && b.f != nil {
		return b.f
	}
	return s.Finder
}

// importsRunning keeps one goroutine per import in this process.
var importsRunning sync.Map

// StartImport queues an import that looks up work emails and runs it in the background.
func (s *Service) StartImport(ctx context.Context, orgID, userID uuid.UUID, in ImportRequest) (*models.LinkedInImport, error) {
	if err := s.checkImport(ctx, orgID, in); err != nil {
		return nil, err
	}
	if s.finder() == nil {
		return nil, errFinderUnavailable
	}
	uid := userID
	im := &models.LinkedInImport{OrganizationID: orgID, UserID: &uid, Status: models.LinkedInImportQueued, Request: mustJSON(in), ContactIDs: []uuid.UUID{}}
	if err := s.Repo.CreateImport(ctx, im); err != nil {
		return nil, err
	}
	s.publishImport(ctx, im)
	id := im.ID
	go s.runImport(context.WithoutCancel(ctx), &id)
	return im, nil
}

// GetImport is one import of the workspace.
func (s *Service) GetImport(ctx context.Context, orgID, id uuid.UUID) (*models.LinkedInImport, error) {
	im, err := s.Repo.GetImport(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if im == nil {
		return nil, errNotFound
	}
	return im, nil
}

// resumeImports takes over an import nobody is running, such as one a restart cut short.
func (s *Service) resumeImports(ctx context.Context) error {
	if s.Repo == nil || s.finder() == nil {
		return nil
	}
	go s.runImport(context.WithoutCancel(ctx), nil)
	return nil
}

func (s *Service) runImport(ctx context.Context, id *uuid.UUID) {
	if !s.available() {
		return
	}
	im, err := s.Repo.ClaimImport(ctx, id, s.now().Add(-importStale))
	if err != nil {
		log.Warn().Err(err).Msg("social: claiming an import failed")
		return
	}
	if im == nil {
		return
	}
	if _, busy := importsRunning.LoadOrStore(im.ID, true); busy {
		return
	}
	defer importsRunning.Delete(im.ID)
	if err := s.processImport(ctx, im); err != nil {
		msg := "The import stopped before it finished."
		var xe *errx.Error
		if errors.As(err, &xe) && xe.Public {
			msg = xe.Message
		}
		log.Warn().Err(err).Str("import_id", im.ID.String()).Msg("social: an import failed")
		im.Status, im.Error = models.LinkedInImportFailed, &msg
		s.saveImport(ctx, im)
	}
}

func (s *Service) processImport(ctx context.Context, im *models.LinkedInImport) error {
	var in ImportRequest
	if err := json.Unmarshal(im.Request, &in); err != nil {
		return err
	}
	userID := uuid.Nil
	if im.UserID != nil {
		userID = *im.UserID
	}
	finder := s.finder()
	if finder == nil {
		return errFinderUnavailable
	}
	hits, err := s.collect(ctx, im.OrganizationID, in)
	if err != nil {
		return err
	}
	people, emails, res, err := s.importPeople(ctx, im.OrganizationID, hits)
	if err != nil {
		return err
	}
	im.Total, im.Processed, im.Skipped, im.NoEmail, im.EmailsFound = len(people), 0, res.Skipped, 0, 0
	s.saveImport(ctx, im)
	var cells [][]string
	// keys holds the charge behind each found address, by cell, so a row the importer refuses is given back.
	keys := map[int]string{}
	var stopped error
	for i := range people {
		h := &people[i]
		email := emails[i]
		if email == "" && stopped == nil && h.Company != nil && h.Company.Website != "" {
			key := "linkedin_import:" + im.ID.String() + ":" + hitPublicID(h)
			found, ferr := finder.FindWorkEmail(ctx, im.OrganizationID, h.Person.FirstName, h.Person.LastName, h.Company.Website, key)
			switch {
			case ferr != nil:
				stopped = ferr
			case found != "":
				email = found
				keys[len(cells)] = key
				im.EmailsFound++
			}
		}
		if email == "" {
			im.Skipped++
			im.NoEmail++
		} else {
			cells = append(cells, hitCells(h, email))
		}
		im.Processed++
		if im.Processed%importSaveEvery == 0 {
			s.saveImport(ctx, im)
		}
	}
	res.Skipped, res.NoEmail = 0, 0
	outcomes, err := s.importCells(ctx, im.OrganizationID, userID, in, cells, res)
	if err != nil {
		for _, k := range keys {
			finder.RefundWorkEmail(ctx, im.OrganizationID, k)
		}
		return err
	}
	for i, k := range keys {
		if o, ok := outcomes[i]; !ok || o.ContactID == nil || o.Status == models.ContactImportRowFailed {
			finder.RefundWorkEmail(ctx, im.OrganizationID, k)
		}
	}
	im.Created, im.Updated = res.Created, res.Updated
	im.Skipped += res.Skipped
	im.ContactIDs = res.ContactIDs
	im.Status = models.LinkedInImportDone
	if stopped != nil {
		msg := "Looking up work emails stopped early; the rest were counted as no email."
		var xe *errx.Error
		if errors.As(stopped, &xe) && xe.Message != "" {
			msg = xe.Message + " The rest were counted as no email."
		}
		im.Error = &msg
	}
	s.saveImport(ctx, im)
	return nil
}

func (s *Service) saveImport(ctx context.Context, im *models.LinkedInImport) {
	if im.ContactIDs == nil {
		im.ContactIDs = []uuid.UUID{}
	}
	if err := s.Repo.SaveImport(ctx, im); err != nil {
		log.Warn().Err(err).Str("import_id", im.ID.String()).Msg("social: saving import progress failed")
		return
	}
	s.publishImport(ctx, im)
}

func (s *Service) publishImport(ctx context.Context, im *models.LinkedInImport) {
	s.publish(ctx, im.OrganizationID, pubsub.SocialEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventSocialImportUpdated}, Channel: models.SocialLinkedIn,
		ImportID: im.ID.String(), Status: im.Status})
}
