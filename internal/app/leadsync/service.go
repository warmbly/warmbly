// Package leadsync implements the on-demand Google Sheets -> leads sync.
//
// A "lead sync source" is a saved binding between a Google Sheet and Warmbly's
// contact importer that the user re-runs with a "Sync now" button. There is no
// background scheduler and no worker involvement: this is pure control-plane
// work. SyncNow reads the sheet, encodes the rows as CSV in memory, and hands
// them to the existing contact ImportCommit path so contact creation/dedupe is
// never reimplemented here.
package leadsync

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// previewRows is how many sheet rows the preview reads (header + sample). It
// mirrors the contacts import preview cap so the dashboard's column mapper sees
// the same amount of data either way.
const previewRows = models.MaxContactImportPreviewRows + 1

// fullColumnSpan is the widest A1 column range we read. Google Sheets caps at
// far more, but contact imports are narrow; A:Z is plenty for typical lead
// sheets and keeps the read cheap.
const fullColumnSpan = "A:Z"

// Service is the on-demand lead-sync control-plane surface.
type Service interface {
	// Connection returns the org's google_sheets OAuth connection (or nil).
	Connection(ctx context.Context, orgID uuid.UUID) (*models.IntegrationConnection, error)
	// SpreadsheetMeta returns the sheet title + tabs for the connect step.
	SpreadsheetMeta(ctx context.Context, orgID, connID uuid.UUID, sheetID string) (*integration.SheetMeta, error)
	// Preview reads the top rows of a tab and returns an ImportPreview-shaped
	// payload so the frontend reuses its contact-import column mapper verbatim.
	Preview(ctx context.Context, orgID, connID uuid.UUID, sheetID, tabTitle string) (*models.ContactImportPreview, *errx.Error)

	// Source CRUD.
	List(ctx context.Context, orgID uuid.UUID, campaignID *uuid.UUID) ([]models.LeadSyncSource, error)
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.LeadSyncSource, *errx.Error)
	Create(ctx context.Context, orgID, userID uuid.UUID, in *models.CreateLeadSyncSource) (*models.LeadSyncSource, *errx.Error)
	Update(ctx context.Context, orgID, id uuid.UUID, in *models.UpdateLeadSyncSource) (*models.LeadSyncSource, *errx.Error)
	Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error

	// SyncNow reads the source's sheet and upserts contacts via ImportCommit.
	// triggeringUserID scopes the contact upsert (contacts are per-user).
	SyncNow(ctx context.Context, triggeringUserID, orgID, sourceID uuid.UUID) (*models.LeadSyncResult, *errx.Error)
}

type service struct {
	repo        repository.LeadSyncRepository
	integration integration.Service
	contacts    contact.ContactService
}

// NewService wires the lead-sync service to the integration service (Google
// token + sheet reads), the contact service (ImportCommit), and the lead-sync
// repository.
func NewService(repo repository.LeadSyncRepository, integrationSvc integration.Service, contactSvc contact.ContactService) Service {
	return &service{repo: repo, integration: integrationSvc, contacts: contactSvc}
}

func (s *service) Connection(ctx context.Context, orgID uuid.UUID) (*models.IntegrationConnection, error) {
	return s.integration.GoogleConnection(ctx, orgID)
}

func (s *service) SpreadsheetMeta(ctx context.Context, orgID, connID uuid.UUID, sheetID string) (*integration.SheetMeta, error) {
	return s.integration.SpreadsheetMeta(ctx, orgID, connID, sheetID)
}

func (s *service) Preview(ctx context.Context, orgID, connID uuid.UUID, sheetID, tabTitle string) (*models.ContactImportPreview, *errx.Error) {
	sheetID = strings.TrimSpace(sheetID)
	if sheetID == "" {
		return nil, errx.New(errx.BadRequest, "sheet_id is required")
	}
	a1 := buildA1Range(tabTitle, previewRows)
	values, err := s.integration.SpreadsheetValues(ctx, orgID, connID, sheetID, a1)
	if err != nil {
		return nil, errx.New(errx.BadRequest, "failed to read sheet: "+err.Error())
	}
	if len(values) == 0 {
		return nil, errx.New(errx.BadRequest, "the selected sheet/tab is empty")
	}

	// The first row is treated as the header (sheets almost always have one);
	// the user can flip has_header in the UI, exactly like a CSV import.
	headers := normalizeHeaders(values[0])
	width := len(headers)

	sample := make([][]string, 0, len(values)-1)
	for i := 1; i < len(values); i++ {
		sample = append(sample, padRow(values[i], width))
	}

	return &models.ContactImportPreview{
		Filename:         "google-sheets-sync.csv",
		Format:           "csv",
		TotalRows:        len(values) - 1,
		Columns:          headers,
		HasHeader:        true,
		SampleRows:       sample,
		SuggestedMapping: suggestMapping(headers),
	}, nil
}

func (s *service) List(ctx context.Context, orgID uuid.UUID, campaignID *uuid.UUID) ([]models.LeadSyncSource, error) {
	return s.repo.List(ctx, orgID, campaignID)
}

func (s *service) Get(ctx context.Context, orgID, id uuid.UUID) (*models.LeadSyncSource, *errx.Error) {
	src, err := s.repo.Get(ctx, orgID, id)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to load sync source")
	}
	if src == nil {
		return nil, errx.New(errx.NotFound, "sync source not found")
	}
	return src, nil
}

func (s *service) Create(ctx context.Context, orgID, userID uuid.UUID, in *models.CreateLeadSyncSource) (*models.LeadSyncSource, *errx.Error) {
	if in == nil {
		return nil, errx.New(errx.BadRequest, "missing payload")
	}
	if in.ConnectionID == uuid.Nil {
		return nil, errx.New(errx.BadRequest, "connection_id is required")
	}
	if strings.TrimSpace(in.SheetID) == "" {
		return nil, errx.New(errx.BadRequest, "sheet_id is required")
	}
	if len(in.ColumnMapping) == 0 {
		return nil, errx.New(errx.BadRequest, "column_mapping is required")
	}
	if xerr := validateDedup(in.Dedup); xerr != nil {
		return nil, xerr
	}
	if xerr := validateMappingHasEmail(in.ColumnMapping); xerr != nil {
		return nil, xerr
	}

	// The connection must be this org's google_sheets OAuth connection.
	conn, err := s.integration.GoogleConnection(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to resolve google connection")
	}
	if conn == nil || conn.ID != in.ConnectionID {
		return nil, errx.New(errx.BadRequest, "connection_id is not this organization's connected Google account")
	}

	subscribed := true
	if in.SubscribedDefault != nil {
		subscribed = *in.SubscribedDefault
	}
	cats := in.CategoryIDs
	if cats == nil {
		cats = []string{}
	}

	src := &models.LeadSyncSource{
		OrganizationID:    orgID,
		CreatedByUserID:   userID,
		Provider:          string(models.IntegrationGoogleSheets),
		ConnectionID:      in.ConnectionID,
		SheetID:           strings.TrimSpace(in.SheetID),
		SheetTitle:        strings.TrimSpace(in.SheetTitle),
		TabTitle:          strings.TrimSpace(in.TabTitle),
		A1Range:           buildA1Range(in.TabTitle, 0),
		HasHeader:         in.HasHeader,
		ColumnMapping:     in.ColumnMapping,
		Dedup:             in.Dedup,
		TargetCampaignID:  in.TargetCampaignID,
		CategoryIDs:       cats,
		SubscribedDefault: subscribed,
		Label:             strings.TrimSpace(in.Label),
		Status:            models.LeadSyncStatusIdle,
	}
	if err := s.repo.Create(ctx, src); err != nil {
		return nil, errx.New(errx.Internal, "failed to create sync source")
	}
	return src, nil
}

func (s *service) Update(ctx context.Context, orgID, id uuid.UUID, in *models.UpdateLeadSyncSource) (*models.LeadSyncSource, *errx.Error) {
	if in == nil {
		return nil, errx.New(errx.BadRequest, "missing payload")
	}
	src, xerr := s.Get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}

	if in.SheetID != nil {
		v := strings.TrimSpace(*in.SheetID)
		if v == "" {
			return nil, errx.New(errx.BadRequest, "sheet_id cannot be empty")
		}
		src.SheetID = v
	}
	if in.SheetTitle != nil {
		src.SheetTitle = strings.TrimSpace(*in.SheetTitle)
	}
	if in.TabTitle != nil {
		src.TabTitle = strings.TrimSpace(*in.TabTitle)
		src.A1Range = buildA1Range(src.TabTitle, 0)
	}
	if in.HasHeader != nil {
		src.HasHeader = *in.HasHeader
	}
	if in.ColumnMapping != nil {
		if len(*in.ColumnMapping) == 0 {
			return nil, errx.New(errx.BadRequest, "column_mapping cannot be empty")
		}
		if xerr := validateMappingHasEmail(*in.ColumnMapping); xerr != nil {
			return nil, xerr
		}
		src.ColumnMapping = *in.ColumnMapping
	}
	if in.Dedup != nil {
		if xerr := validateDedup(*in.Dedup); xerr != nil {
			return nil, xerr
		}
		src.Dedup = *in.Dedup
	}
	if in.ClearCampaign {
		src.TargetCampaignID = nil
	} else if in.TargetCampaignID != nil {
		src.TargetCampaignID = in.TargetCampaignID
	}
	if in.CategoryIDs != nil {
		src.CategoryIDs = *in.CategoryIDs
	}
	if in.SubscribedDefault != nil {
		src.SubscribedDefault = *in.SubscribedDefault
	}
	if in.Label != nil {
		src.Label = strings.TrimSpace(*in.Label)
	}

	if err := s.repo.Update(ctx, src); err != nil {
		return nil, errx.New(errx.Internal, "failed to update sync source")
	}
	return src, nil
}

func (s *service) Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error {
	if _, xerr := s.Get(ctx, orgID, id); xerr != nil {
		return xerr
	}
	if err := s.repo.Delete(ctx, orgID, id); err != nil {
		return errx.New(errx.Internal, "failed to delete sync source")
	}
	return nil
}

// SyncNow reads the source's sheet and upserts contacts through the existing
// contact ImportCommit path.
//
// Idempotency: SyncNow is naturally idempotent. ImportCommit dedupes/upserts by
// (user, email), so re-running the same source against the same sheet converges
// to the same contact set — no Idempotency-Key header is required for safe
// retries (re-runs at worst re-update unchanged rows).
func (s *service) SyncNow(ctx context.Context, triggeringUserID, orgID, sourceID uuid.UUID) (*models.LeadSyncResult, *errx.Error) {
	src, xerr := s.Get(ctx, orgID, sourceID)
	if xerr != nil {
		return nil, xerr
	}

	// Read the full tab. We deliberately re-read the whole range each run so
	// new rows (appended at the bottom) are picked up.
	a1 := buildA1Range(src.TabTitle, 0)
	values, err := s.integration.SpreadsheetValues(ctx, orgID, src.ConnectionID, src.SheetID, a1)
	if err != nil {
		s.recordResult(ctx, src.ID, models.LeadSyncStatusError, nil, nil, err.Error())
		return nil, errx.New(errx.BadRequest, "failed to read sheet: "+err.Error())
	}

	csvBytes, cerr := encodeCSV(values)
	if cerr != nil {
		s.recordResult(ctx, src.ID, models.LeadSyncStatusError, nil, nil, cerr.Error())
		return nil, errx.New(errx.Internal, "failed to encode sheet rows")
	}

	var campaignIDs []string
	if src.TargetCampaignID != nil {
		campaignIDs = []string{src.TargetCampaignID.String()}
	}
	subscribed := src.SubscribedDefault
	opts := &models.ContactImportCommit{
		Mapping:           src.ColumnMapping,
		Dedup:             src.Dedup,
		HasHeader:         src.HasHeader,
		CategoryIDs:       src.CategoryIDs,
		CampaignIDs:       campaignIDs,
		SubscribedDefault: &subscribed,
	}

	result, ierr := s.contacts.ImportCommit(ctx, triggeringUserID.String(), bytes.NewReader(csvBytes), "google-sheets-sync.csv", opts)
	if ierr != nil {
		s.recordResult(ctx, src.ID, models.LeadSyncStatusError, nil, nil, ierr.Message)
		return nil, ierr
	}

	now := time.Now().UTC()
	resJSON, _ := json.Marshal(result)
	s.recordResult(ctx, src.ID, models.LeadSyncStatusIdle, &now, resJSON, "")

	return &models.LeadSyncResult{SourceID: src.ID, Result: result}, nil
}

// recordResult is a best-effort persistence of a sync outcome. Failures to
// persist the bookkeeping never fail the sync the user already paid for.
func (s *service) recordResult(ctx context.Context, id uuid.UUID, status models.LeadSyncStatus, syncedAt *time.Time, resJSON []byte, errMsg string) {
	_ = s.repo.SetResult(ctx, id, status, syncedAt, resJSON, errMsg)
}

// --- helpers ----------------------------------------------------------------

// buildA1Range builds an A1 range for a tab. With maxRows == 0 it returns the
// full-column span for the tab (read everything); with maxRows > 0 it bounds
// the read to the first maxRows rows (preview).
//
// Tab titles are quoted with single quotes and any embedded single quote is
// doubled, per the Sheets A1 grammar.
func buildA1Range(tabTitle string, maxRows int) string {
	span := fullColumnSpan
	if maxRows > 0 {
		span = "A1:Z" + strconv.Itoa(maxRows)
	}
	tab := strings.TrimSpace(tabTitle)
	if tab == "" {
		return span
	}
	quoted := "'" + strings.ReplaceAll(tab, "'", "''") + "'"
	return quoted + "!" + span
}

// encodeCSV writes the sheet's 2-D values to an in-memory CSV buffer. The
// contact importer re-parses this with the stdlib CSV reader, so the round-trip
// stays inside one well-understood format.
func encodeCSV(values [][]string) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	for _, row := range values {
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func validateDedup(d models.ContactImportDedupStrategy) *errx.Error {
	switch d {
	case models.ContactImportDedupSkip, models.ContactImportDedupUpdate, models.ContactImportDedupCreateDuplicate:
		return nil
	}
	return errx.New(errx.BadRequest, "unknown dedup strategy: "+string(d))
}

// validateMappingHasEmail enforces that at least one column maps to the email
// target — without it ImportCommit would reject every row as "missing email".
func validateMappingHasEmail(mapping []models.ContactImportColumnMapping) *errx.Error {
	for _, m := range mapping {
		if m.Target == models.ContactImportTargetEmail {
			return nil
		}
	}
	return errx.New(errx.BadRequest, "column_mapping must map one column to 'email'")
}

// normalizeHeaders trims the header row and synthesises a name for any blank
// cell so every column is mappable, mirroring the CSV importer's behaviour.
func normalizeHeaders(first []string) []string {
	out := make([]string, len(first))
	for i, c := range first {
		out[i] = strings.TrimSpace(c)
		if out[i] == "" {
			out[i] = "Column " + strconv.Itoa(i+1)
		}
	}
	return out
}

// padRow returns a copy of row padded (or truncated) to n columns. Sheets
// returns ragged rows when trailing cells are empty.
func padRow(row []string, n int) []string {
	if len(row) >= n {
		return row[:n]
	}
	out := make([]string, n)
	copy(out, row)
	return out
}

// suggestMapping applies the same fuzzy header heuristics the contact importer
// uses so the dashboard's preview arrives with sensible defaults.
func suggestMapping(headers []string) []models.ContactImportColumnMapping {
	out := make([]models.ContactImportColumnMapping, len(headers))
	for i, h := range headers {
		out[i] = guessTarget(i, h)
	}
	return out
}

func guessTarget(idx int, header string) models.ContactImportColumnMapping {
	key := strings.ToLower(header)
	key = strings.NewReplacer(" ", "", "_", "", "-", "", ".", "").Replace(key)
	switch key {
	case "email", "emailaddress", "mail", "primaryemail":
		return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetEmail}
	case "firstname", "givenname", "fname", "first":
		return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetFirstName}
	case "lastname", "familyname", "surname", "lname", "last":
		return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetLastName}
	case "company", "companyname", "organization", "organisation", "employer", "account", "accountname":
		return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetCompany}
	case "phone", "phonenumber", "mobile", "cell":
		return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetPhone}
	case "subscribed", "optin", "optedin", "subscribe":
		return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetSubscribed}
	case "categories", "category", "tags", "tag", "labels", "label":
		return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetCategories}
	}
	return models.ContactImportColumnMapping{Index: idx, Target: models.ContactImportTargetIgnore}
}
