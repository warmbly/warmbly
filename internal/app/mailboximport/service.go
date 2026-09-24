package mailboximport

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/sendingdomain"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/dnsauth"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/pkg/typesafe"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

// EmailConnector is the mailbox service's connect and settings surface.
type EmailConnector interface {
	OnboardSMTPIMAP(ctx context.Context, userID string, orgID *uuid.UUID, data *models.NewSMTPIMAPAccount) (*models.Email, *errx.Error)
	UpdateSMTPIMAPCredentials(ctx context.Context, orgID *uuid.UUID, accountID uuid.UUID, creds *models.SmtpImap) (*models.Email, *errx.Error)
	Update(ctx context.Context, orgID, userID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error)
	SetWarmupLifecycle(ctx context.Context, orgID, emailAccountID, action string) (*models.Email, *errx.Error)
	UpdateTrackingDomain(ctx context.Context, orgID, emailAccountID, domain string) (*models.TrackingDomainStatus, *errx.Error)
}

// DomainSetup is the sending-domain side: tracking host suggestions and root redirects.
type DomainSetup interface {
	Overview(ctx context.Context, orgID uuid.UUID) ([]models.SendingDomain, *errx.Error)
	TrackingSuggestion(ctx context.Context, domain string, inUse []models.TrackingDomainUse) *models.TrackingSuggestion
	AutoRedirect(ctx context.Context, orgID, userID uuid.UUID, domain, target string) *errx.Error
	AutoTracking(ctx context.Context, orgID uuid.UUID, domain, host string)
}

// Mailboxes is the mailbox store, for duplicate checks and host classification.
type Mailboxes interface {
	Get(ctx context.Context, orgID, emailAccountID string) (*models.Email, *errx.Error)
	FindInOrganization(ctx context.Context, orgID uuid.UUID, email string) (*models.EmailRef, *errx.Error)
	FindManyInOrganization(ctx context.Context, orgID uuid.UUID, emails []string) (map[string]models.EmailRef, *errx.Error)
	ListUnclassifiedDomains(ctx context.Context, limit int) ([]string, *errx.Error)
	SetDomainMailHost(ctx context.Context, domain, mailHost, passwordAuthMethod string) *errx.Error
}

// Allowance reports how many more mailboxes the workspace may hold.
type Allowance interface {
	MailboxAllowance(ctx context.Context, orgID uuid.UUID) (*models.MailboxAllowance, *errx.Error)
}

// WarmupScheduler seeds a mailbox's warmup chain right away.
type WarmupScheduler interface {
	EnsureWarmupScheduled(ctx context.Context, accountID uuid.UUID) error
}

// Auditor records what the import did, on the audit spine that keeps every dashboard live.
type Auditor interface {
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ipAddress, userAgent string, changes, metadata map[string]string)
}

// Delegator connects mailboxes through an administrator's grant over their domain.
type Delegator interface {
	GrantFor(ctx context.Context, orgID uuid.UUID, provider, domain string) (*models.DomainGrant, error)
	Connect(ctx context.Context, orgID uuid.UUID, userID string, grantID uuid.UUID, email, name string) (*models.Email, *errx.Error)
}

// VendorSource reads a vendor mailbox's credentials when its row is worked, and
// records the link so a later password change can be fetched again.
type VendorSource interface {
	Fields(ctx context.Context, orgID, connectionID uuid.UUID, mailboxID, email, provider string) (map[models.MailboxImportField]string, *errx.Error)
	Link(ctx context.Context, accountID, connectionID uuid.UUID, mailboxID string)
}

// VendorAuthorization is where getting a vendor domain onto a grant stands.
// All zero means the vendor cannot do it and the mailbox needs a sign-in.
type VendorAuthorization struct {
	GrantID *uuid.UUID
	Pending bool
	// Message says why the vendor could not authorize the domain.
	Message string
}

// VendorAuthorizer is a VendorSource that can have the vendor authorize this
// instance's app on a domain, so its mailboxes connect with no sign-in.
type VendorAuthorizer interface {
	AuthorizeDomain(ctx context.Context, orgID, userID, connectionID uuid.UUID, email, provider string) VendorAuthorization
}

// Deps is everything the service talks to. Asker, Warmup, Auditor,
// Publisher, Delegator and Vendors are optional.
type Deps struct {
	Repo      repository.MailboxImportRepository
	Emails    EmailConnector
	Mailboxes Mailboxes
	Tags      repository.GroupRepository
	Cipher    cipher.CipherService
	Detector  *mailhost.Detector
	Allowance Allowance
	Asker     typesafe.Asker
	Warmup    WarmupScheduler
	Auditor   Auditor
	Publisher *pubsub.StreamingPublisher
	// GoogleSignin reports whether new Gmail mailboxes may connect with Google sign-in here.
	GoogleSignin func() bool
	Delegator    Delegator
	Vendors      VendorSource
	Domains      DomainSetup
}

type Service struct {
	repo      repository.MailboxImportRepository
	emails    EmailConnector
	mailboxes Mailboxes
	tags      repository.GroupRepository
	cipher    cipher.CipherService
	detector  *mailhost.Detector
	allowance Allowance
	asker     typesafe.Asker
	warmup    WarmupScheduler
	auditor   Auditor
	publisher *pubsub.StreamingPublisher
	google    func() bool
	delegator Delegator
	vendors   VendorSource
	domains   DomainSetup
	// redirected remembers which import already set up which domain's redirect.
	redirected sync.Map

	kick     chan struct{}
	progress sync.Map // import id -> time.Time of the last progress event
	causes   sync.Map // scrubbed server reply -> refined cause key
}

func NewService(d Deps) *Service {
	if d.GoogleSignin == nil {
		d.GoogleSignin = func() bool { return false }
	}
	if d.Detector == nil {
		d.Detector = mailhost.NewDetector(nil, nil, nil)
	}
	return &Service{
		repo: d.Repo, emails: d.Emails, mailboxes: d.Mailboxes, tags: d.Tags, cipher: d.Cipher,
		detector: d.Detector, allowance: d.Allowance, asker: d.Asker, warmup: d.Warmup,
		auditor: d.Auditor, publisher: d.Publisher, google: d.GoogleSignin,
		delegator: d.Delegator, vendors: d.Vendors, domains: d.Domains,
		kick: make(chan struct{}, 1),
	}
}

// prepared is an input read, mapped, detected and judged.
type prepared struct {
	table      *table
	hasHeader  bool
	suggestion suggestion
	rows       []builtRow
	detections map[string]mailhost.Detection
	signature  string
}

func (s *Service) prepare(ctx context.Context, orgID uuid.UUID, in Input, given models.MailboxImportMapping, opts models.MailboxImportOptions) (*prepared, *errx.Error) {
	t, xerr := readInput(in)
	if xerr != nil {
		return nil, xerr
	}
	hasHeader := hasHeaderRow(t.records[0])
	if opts.HasHeader != nil {
		hasHeader = *opts.HasHeader
	}
	width := 0
	for _, rec := range t.records {
		if len(rec) > width {
			width = len(rec)
		}
	}
	headers := make([]string, width)
	body := t.records
	firstLine := 1
	if hasHeader {
		for i := range headers {
			if i < len(t.records[0]) {
				headers[i] = t.records[0][i]
			}
		}
		body, firstLine = t.records[1:], 2
	}
	for i := range headers {
		if strings.TrimSpace(headers[i]) == "" {
			headers[i] = fmt.Sprintf("Column %d", i+1)
		}
	}
	if len(body) == 0 {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "mailbox_import_empty", "The file has a header row but no mailboxes.")
	}
	if len(body) > config.MailboxImportMaxRows {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "mailbox_import_too_large",
			"One import takes up to 5,000 mailboxes. Split the file and import the rest separately.")
	}

	sig := ""
	var saved models.MailboxImportMapping
	if hasHeader {
		sig = headerSignature(headers)
		if len(given) == 0 {
			if m, ok, err := s.repo.GetMapping(ctx, orgID, sig); err == nil && ok {
				saved = m
			}
		}
	}
	sug := s.suggest(ctx, headers, body, given, saved)

	emails, domains := emailsAndDomains(body, sug.mapping)
	existing, xerr := s.mailboxes.FindManyInOrganization(ctx, orgID, emails)
	if xerr != nil {
		return nil, xerr
	}
	// An agency file can span thousands of domains; the cache makes the next preview instant.
	detectCtx, cancel := context.WithTimeout(ctx, detectWait)
	detections := s.detector.DetectMany(detectCtx, domains)
	cancel()

	rows := buildRows(buildContext{
		headers: headers, mapping: sug.mapping, detections: detections, existing: existing,
		sharedPassword: strings.TrimSpace(opts.SharedPassword), googleSignin: s.google(),
		grants: s.grantsFor(ctx, orgID, detections),
	}, body, firstLine)
	return &prepared{table: t, hasHeader: hasHeader, suggestion: sug, rows: rows, detections: detections, signature: sig}, nil
}

// grantsFor finds the administrator grants covering the Google and Microsoft domains detected.
func (s *Service) grantsFor(ctx context.Context, orgID uuid.UUID, detections map[string]mailhost.Detection) map[string]uuid.UUID {
	out := map[string]uuid.UUID{}
	if s.delegator == nil {
		return out
	}
	for domain, d := range detections {
		provider := ""
		switch {
		case d.Host.Google():
			provider = models.GrantProviderGoogle
		case d.Host.Microsoft():
			provider = models.GrantProviderMicrosoft
		default:
			continue
		}
		if g, err := s.delegator.GrantFor(ctx, orgID, provider, domain); err == nil && g != nil {
			out[grantKey(provider, domain)] = g.ID
		}
	}
	return out
}

// Preview reads an input and says what an import of it would do, without doing any of it.
func (s *Service) Preview(ctx context.Context, orgID uuid.UUID, in Input, given models.MailboxImportMapping, opts models.MailboxImportOptions) (*models.MailboxImportPreview, *errx.Error) {
	p, xerr := s.prepare(ctx, orgID, in, given, opts)
	if xerr != nil {
		return nil, xerr
	}
	out := &models.MailboxImportPreview{
		Format: p.table.format, HasHeader: p.hasHeader, Vendor: p.suggestion.vendor,
		Columns: p.suggestion.columns, Mapping: p.suggestion.mapping, SavedMapping: p.suggestion.savedMapping,
		MaxRows: config.MailboxImportMaxRows, Rows: make([]models.MailboxImportPreviewRow, 0, 100),
		Issues: make([]models.MailboxImportIssue, 0), Domains: make([]models.MailboxImportDomain, 0),
	}
	issues := map[string]*models.MailboxImportIssue{}
	domainRows := map[string]int{}
	out.Summary.Total = len(p.rows)
	for _, r := range p.rows {
		switch r.status {
		case models.ImportPreviewReady:
			out.Summary.Ready++
		case models.ImportPreviewNeedsSignin:
			out.Summary.NeedsSignin++
		case models.ImportPreviewInvalid:
			out.Summary.Invalid++
		case models.ImportPreviewExisting:
			out.Summary.Existing++
		case models.ImportPreviewDuplicate:
			out.Summary.Duplicate++
		}
		if r.domain != "" {
			domainRows[r.domain]++
		}
		if r.cause != "" {
			is, ok := issues[r.cause]
			if !ok {
				info := causeInfo(r.cause)
				is = &models.MailboxImportIssue{Cause: r.cause, Title: info.Title, Fix: info.Fix, Lines: []int{}}
				issues[r.cause] = is
			}
			is.Count++
			if len(is.Lines) < 50 {
				is.Lines = append(is.Lines, r.line)
			}
		}
		if len(out.Rows) < 100 {
			out.Rows = append(out.Rows, previewRow(r))
		}
	}
	for _, is := range issues {
		out.Issues = append(out.Issues, *is)
	}
	sort.Slice(out.Issues, func(a, b int) bool { return out.Issues[a].Count > out.Issues[b].Count })

	out.Domains = s.domainSummary(ctx, orgID, p.detections, domainRows)
	if s.allowance != nil {
		if a, xerr := s.allowance.MailboxAllowance(ctx, orgID); xerr == nil {
			out.Allowance = a
		}
	}
	return out, nil
}

func previewRow(r builtRow) models.MailboxImportPreviewRow {
	pr := models.MailboxImportPreviewRow{
		Line: r.line, Email: r.email, Name: r.payload.Name, MailHost: r.payload.MailHost,
		AuthMethod: r.payload.AuthMethod, Status: r.status, Cause: r.cause, Problem: r.problem,
	}
	if r.payload.SMTP != nil && r.payload.SMTP.Host != "" {
		pr.SMTP = &models.MailboxImportEndpoint{Host: r.payload.SMTP.Host, Port: r.payload.SMTP.Port, Security: r.payload.SMTP.Security, Username: r.payload.SMTP.Username}
	}
	if r.payload.IMAP != nil && r.payload.IMAP.Host != "" {
		pr.IMAP = &models.MailboxImportEndpoint{Host: r.payload.IMAP.Host, Port: r.payload.IMAP.Port, Security: r.payload.IMAP.Security, Username: r.payload.IMAP.Username}
	}
	return pr
}

// detectWait bounds the domain detection one preview or create waits for.
const detectWait = 30 * time.Second

// maxAuthChecks bounds the SPF/DKIM/DMARC lookups one preview makes.
const maxAuthChecks = 40

// domainSummary is the per-domain summary: who hosts it, the servers, and its sending authentication.
func (s *Service) domainSummary(ctx context.Context, orgID uuid.UUID, detections map[string]mailhost.Detection, rows map[string]int) []models.MailboxImportDomain {
	known := map[string]models.SendingDomain{}
	if s.domains != nil {
		if list, xerr := s.domains.Overview(ctx, orgID); xerr == nil {
			for _, d := range list {
				known[d.Domain] = d
			}
		}
	}
	names := make([]string, 0, len(rows))
	for d := range rows {
		names = append(names, d)
	}
	sort.Slice(names, func(a, b int) bool {
		if rows[names[a]] != rows[names[b]] {
			return rows[names[a]] > rows[names[b]]
		}
		return names[a] < names[b]
	})
	out := make([]models.MailboxImportDomain, len(names))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, name := range names {
		det := detections[name]
		d := models.MailboxImportDomain{
			Domain: name, MailHost: string(det.Host), Label: det.Host.Label(), Source: det.Source, Rows: rows[name],
			PasswordAuth: string(det.PasswordAuth), AppPasswordURL: det.AppPasswordURL,
		}
		if det.Settings != nil {
			d.SMTP = &models.MailboxImportEndpoint{Host: det.Settings.SMTP.Host, Port: det.Settings.SMTP.Port, Security: det.Settings.SMTP.Security}
			d.IMAP = &models.MailboxImportEndpoint{Host: det.Settings.IMAP.Host, Port: det.Settings.IMAP.Port, Security: det.Settings.IMAP.Security}
		}
		if k, ok := known[name]; ok {
			d.Redirect = k.Redirect
		}
		out[i] = d
		if i >= maxAuthChecks {
			if target := config.TrackingHostname(); target != "" {
				out[i].Tracking = &models.TrackingSuggestion{Host: "track." + name, Status: "suggested", CNAMETarget: target}
			}
			continue
		}
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			res := dnsauth.Check(cctx, name, nil)
			out[i].Auth = &models.MailboxImportDomainAuth{State: res.State(), SPF: res.SPFFound, DKIM: res.DKIMFound, DMARC: res.DMARCFound}
			if s.domains != nil {
				out[i].Tracking = s.domains.TrackingSuggestion(cctx, name, known[name].TrackingDomains)
			}
		}(i, name)
	}
	wg.Wait()
	return out
}

// CreateInput carries everything an import is created from.
type CreateInput struct {
	OrgID   uuid.UUID
	UserID  uuid.UUID
	Input   Input
	Mapping models.MailboxImportMapping
	Options models.MailboxImportOptions
}

// Create stores the import and its rows, sealed, and wakes the runner.
func (s *Service) Create(ctx context.Context, c CreateInput) (*models.MailboxImport, *errx.Error) {
	onExisting := c.Options.OnExisting
	if onExisting == "" {
		onExisting = "update"
	}
	if onExisting != "update" && onExisting != "skip" {
		return nil, errx.New(errx.BadRequest, "on_existing must be update or skip")
	}
	if xerr := validateSettings(c.Options.Settings); xerr != nil {
		return nil, xerr
	}
	if xerr := validateDomainChoices(c.Options); xerr != nil {
		return nil, xerr
	}
	p, xerr := s.prepare(ctx, c.OrgID, c.Input, c.Mapping, c.Options)
	if xerr != nil {
		return nil, xerr
	}
	if _, ok := columnFor(p.suggestion.mapping)[models.ImportFieldEmail]; !ok {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "mailbox_import_no_email_column", "Choose which column holds the email addresses.")
	}

	ciph, err := s.cipher.Cipher(ctx, c.OrgID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	inserts := make([]repository.MailboxImportRowInsert, 0, len(p.rows))
	for _, r := range p.rows {
		ins := repository.MailboxImportRowInsert{
			Line: r.line, Email: r.email, Domain: r.domain, MailHost: r.payload.MailHost, Fields: r.fields,
		}
		switch r.status {
		case models.ImportPreviewReady, models.ImportPreviewExisting, models.ImportPreviewNeedsSignin:
			ins.Status = models.ImportRowQueued
		case models.ImportPreviewDuplicate:
			ins.Status, ins.Code, ins.Cause, ins.Message = models.ImportRowSkipped, causeDuplicateRow, causeDuplicateRow, r.problem
		default:
			ins.Status, ins.Code, ins.Cause, ins.Message = models.ImportRowFailed, r.cause, r.cause, r.problem
		}
		// An invalid row keeps what it has, so fixing the one missing piece is
		// enough to retry it; a row no fix can repair keeps nothing.
		if ins.Status != models.ImportRowSkipped && !unfixable[r.cause] {
			r.payload.TrackingDomain, r.payload.RedirectURL = domainChoices(c.Options, r.domain)
			raw, _ := json.Marshal(r.payload)
			sealed, err := ciph.Encrypt(ctx, string(raw))
			if err != nil {
				errs.CaptureException(err)
				return nil, errx.InternalError()
			}
			ins.Payload = sealed
		}
		inserts = append(inserts, ins)
	}

	settings, _ := json.Marshal(c.Options.Settings)
	columns, _ := json.Marshal(p.suggestion.headers)
	source := "file"
	if len(c.Input.File) == 0 {
		source = "paste"
	}
	vendorID := ""
	if p.suggestion.vendor != nil {
		vendorID = p.suggestion.vendor.ID
	}
	creator := c.UserID
	imp := &models.MailboxImport{
		ID: uuid.New(), OrganizationID: c.OrgID, CreatedBy: &creator, Status: models.ImportRunning,
		Source: source, Filename: trimFilename(c.Input.Filename), Vendor: vendorID, OnExisting: onExisting, Total: len(inserts),
	}
	if err := s.repo.Create(ctx, imp, settings, columns, inserts); err != nil {
		return nil, errx.InternalError()
	}

	if p.hasHeader && p.signature != "" && (c.Options.SaveMapping == nil || *c.Options.SaveMapping) {
		if err := s.repo.SaveMapping(ctx, c.OrgID, p.signature, p.suggestion.mapping); err != nil {
			log.Warn().Err(err).Msg("mailbox import: saving the column mapping failed")
		}
	}
	s.audit(ctx, c.OrgID, c.UserID, models.AuditActionCreate, imp.ID, map[string]string{
		"rows": strconv.Itoa(len(inserts)), "source": source,
	})
	s.Kick()
	return s.Get(ctx, c.OrgID, imp.ID)
}

// unfixable causes cannot be repaired by a new password or new servers.
var unfixable = map[string]bool{
	causeMissingEmail: true, causeInvalidEmail: true, causeProviderUnsupport: true, causeInvalidValue: true,
}

// validateDomainChoices checks the per-domain tracking hosts and redirects picked in review.
func validateDomainChoices(o models.MailboxImportOptions) *errx.Error {
	if len(o.TrackingDomains) > 500 || len(o.Redirects) > 500 {
		return errx.New(errx.BadRequest, "tracking_domains and redirects take up to 500 domains")
	}
	for _, host := range o.TrackingDomains {
		if h := config.NormalizeTrackingHost(host); h != "" {
			if xerr := validate.ValidateTrackingDomain(h); xerr != nil {
				return xerr
			}
		}
	}
	for _, target := range o.Redirects {
		if t := strings.TrimSpace(target); t != "" {
			if !strings.Contains(t, "://") {
				t = "https://" + t
			}
			if u, err := url.Parse(t); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				return errx.NewWithIdentifier(errx.BadRequest, sendingdomain.ErrIDTarget, "Each redirect needs a website address, like https://yourcompany.com.")
			}
		}
	}
	return nil
}

// domainChoices is what the review step picked for one domain.
func domainChoices(o models.MailboxImportOptions, domain string) (string, string) {
	return config.NormalizeTrackingHost(o.TrackingDomains[domain]), strings.TrimSpace(o.Redirects[domain])
}

func trimFilename(name string) string {
	name = strings.TrimSpace(name)
	if r := []rune(name); len(r) > 200 {
		name = string(r[:200])
	}
	return name
}

func validateSettings(st models.MailboxImportSettings) *errx.Error {
	check := func(v *int, max int, label string) *errx.Error {
		if v != nil && (*v < 0 || *v > max) {
			return errx.New(errx.BadRequest, fmt.Sprintf("%s must be between 0 and %d", label, max))
		}
		return nil
	}
	if st.DailyLimit != nil && *st.DailyLimit < config.LimitMin {
		return errx.New(errx.BadRequest, fmt.Sprintf("daily_limit must be at least %d", config.LimitMin))
	}
	if st.WarmupStart != nil && st.WarmupMax != nil && *st.WarmupStart > *st.WarmupMax {
		return errx.New(errx.BadRequest, "warmup_start cannot be above warmup_max")
	}
	for _, e := range []*errx.Error{
		check(st.DailyLimit, config.LimitMax, "daily_limit"),
		check(st.MinWait, 86400, "min_wait"),
		check(st.WarmupStart, 100, "warmup_start"),
		check(st.WarmupMax, 100, "warmup_max"),
		check(st.WarmupIncrease, 100, "warmup_increase"),
		check(st.WarmupReplyRate, 100, "warmup_reply_rate"),
	} {
		if e != nil {
			return e
		}
	}
	if len(st.TagIDs) > 50 {
		return errx.New(errx.BadRequest, "tag_ids takes up to 50 tags")
	}
	for _, id := range st.TagIDs {
		if _, err := uuid.Parse(id); err != nil {
			return errx.New(errx.BadRequest, "tag_ids must be tag ids")
		}
	}
	return nil
}

// Get is one import with its counts and grouped causes.
func (s *Service) Get(ctx context.Context, orgID, id uuid.UUID) (*models.MailboxImport, *errx.Error) {
	imp, err := s.repo.Get(ctx, orgID, id)
	if err != nil {
		return nil, errx.InternalError()
	}
	if imp == nil {
		return nil, errx.ErrNotFound
	}
	fillCauses(imp.Causes)
	return imp, nil
}

// List pages the workspace's imports, newest first, with an opaque cursor.
func (s *Service) List(ctx context.Context, orgID uuid.UUID, cursor string, limit int) ([]models.MailboxImport, string, *errx.Error) {
	var before *time.Time
	var beforeID *uuid.UUID
	if cursor != "" {
		t, id, ok := decodeCursor(cursor)
		if !ok {
			return nil, "", errx.NewWithIdentifier(errx.BadRequest, "invalid_cursor", "The cursor is not valid.")
		}
		before, beforeID = &t, &id
	}
	list, err := s.repo.List(ctx, orgID, before, beforeID, limit+1)
	if err != nil {
		return nil, "", errx.InternalError()
	}
	next := ""
	if len(list) > limit {
		list = list[:limit]
		last := list[len(list)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	for i := range list {
		fillCauses(list[i].Causes)
	}
	return list, next, nil
}

func encodeCursor(t time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

func decodeCursor(c string) (time.Time, uuid.UUID, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, false
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	return t, id, true
}

// Rows pages an import's rows by line, filtered by status and cause.
func (s *Service) Rows(ctx context.Context, orgID, id uuid.UUID, statuses []string, cause string, cursor string, limit int) ([]models.MailboxImportRow, string, *errx.Error) {
	after := 0
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil || n < 0 {
			return nil, "", errx.NewWithIdentifier(errx.BadRequest, "invalid_cursor", "The cursor is not valid.")
		}
		after = n
	}
	if imp, xerr := s.Get(ctx, orgID, id); xerr != nil {
		return nil, "", xerr
	} else if imp == nil {
		return nil, "", errx.ErrNotFound
	}
	rows, err := s.repo.ListRows(ctx, orgID, id, statuses, cause, after, limit+1)
	if err != nil {
		return nil, "", errx.InternalError()
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = strconv.Itoa(rows[len(rows)-1].Line)
	}
	return rows, next, nil
}

// FixRow corrects one failed row's credentials or servers and queues it again.
func (s *Service) FixRow(ctx context.Context, orgID, userID, id uuid.UUID, line int, fix models.MailboxImportRowFix) (*models.MailboxImportRow, *errx.Error) {
	row, sealed, err := s.repo.GetRow(ctx, orgID, id, line)
	if err != nil {
		return nil, errx.InternalError()
	}
	if row == nil {
		return nil, errx.ErrNotFound
	}
	if row.Status != models.ImportRowFailed {
		return nil, errx.NewWithIdentifier(errx.Conflict, "mailbox_import_row_not_failed", "Only a row that did not connect can be fixed.")
	}
	if sealed == "" {
		return nil, errx.NewWithIdentifier(errx.Conflict, "mailbox_import_credentials_expired",
			"This row's credentials were removed after the retry window. Import it again.")
	}
	ciph, cerr := s.cipher.Cipher(ctx, orgID)
	if cerr != nil {
		errs.CaptureException(cerr)
		return nil, errx.InternalError()
	}
	p, xerr := unseal(ctx, ciph, sealed)
	if xerr != nil {
		return nil, xerr
	}
	applyFix(&p, row.Email, fix)
	if p.SMTP == nil || p.IMAP == nil || p.SMTP.Host == "" || p.IMAP.Host == "" {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "mailbox_import_row_incomplete", "Add the SMTP and IMAP servers for this row.")
	}
	// A vendor row takes its password from the vendor again when it runs.
	if (p.SMTP.Password == "" || p.IMAP.Password == "") && p.VendorConnectionID == nil {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "mailbox_import_row_incomplete", "Add a password for this row.")
	}
	raw, _ := json.Marshal(p)
	resealed, err := ciph.Encrypt(ctx, string(raw))
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if err := s.repo.Requeue(ctx, id, line, resealed); err != nil {
		return nil, errx.InternalError()
	}
	if err := s.repo.Reopen(ctx, orgID, id); err != nil {
		return nil, errx.InternalError()
	}
	s.audit(ctx, orgID, userID, models.AuditActionUpdate, id, map[string]string{"fixed_line": strconv.Itoa(line)})
	s.Kick()
	row, _, err = s.repo.GetRow(ctx, orgID, id, line)
	if err != nil || row == nil {
		return nil, errx.InternalError()
	}
	return row, nil
}

func applyFix(p *payload, email string, fix models.MailboxImportRowFix) {
	if p.SMTP == nil {
		p.SMTP = &models.Service{Username: email}
	}
	if p.IMAP == nil {
		p.IMAP = &models.Service{Username: email}
	}
	host := mailhost.Host(p.MailHost)
	if fix.AppPassword != nil && *fix.AppPassword != "" {
		pw := mailhost.NormalizeAppPassword(host, *fix.AppPassword)
		p.SMTP.Password, p.IMAP.Password = pw, pw
	} else if fix.Password != nil && *fix.Password != "" {
		pw := mailhost.NormalizeAppPassword(host, *fix.Password)
		p.SMTP.Password, p.IMAP.Password = pw, pw
	}
	if fix.Username != nil && *fix.Username != "" {
		p.SMTP.Username, p.IMAP.Username = *fix.Username, *fix.Username
	}
	leg := func(svc *models.Service, f *models.MailboxImportLegFix, name string) {
		if f == nil {
			return
		}
		if f.Host != nil && strings.TrimSpace(*f.Host) != "" {
			svc.Host = strings.TrimSpace(*f.Host)
		}
		if f.Port != nil && *f.Port > 0 && *f.Port <= 65535 {
			svc.Port = *f.Port
			if f.Security == nil {
				if name == "smtp" {
					svc.Security = models.ResolveSMTPSecurity("", svc.Port)
				} else {
					svc.Security = models.ResolveIMAPSecurity("", svc.Port)
				}
			}
		}
		if f.Security != nil {
			if sec, ok := parseSecurity(*f.Security, svc.Port); ok {
				svc.Security = sec
			}
		}
		if f.Username != nil && *f.Username != "" {
			svc.Username = *f.Username
		}
		if f.Password != nil && *f.Password != "" {
			svc.Password = *f.Password
		}
	}
	leg(p.SMTP, fix.SMTP, "smtp")
	leg(p.IMAP, fix.IMAP, "imap")
	if p.SMTP.Port == 0 {
		p.SMTP.Port = 587
	}
	if p.IMAP.Port == 0 {
		p.IMAP.Port = 993
	}
	if fix.SMTP != nil && fix.SMTP.Host != nil {
		p.MailHost = string(mailhost.Refine(mailhost.FromServer(p.SMTP.Host), domainOf(email)))
		p.AuthMethod = mailhost.AuthMethodFor(mailhost.Host(p.MailHost), "")
	}
	p.Signin = false
}

// Retry queues failed rows again, optionally with one new password for all of them.
func (s *Service) Retry(ctx context.Context, orgID, userID, id uuid.UUID, req models.MailboxImportRetry) (*models.MailboxImport, *errx.Error) {
	if _, xerr := s.Get(ctx, orgID, id); xerr != nil {
		return nil, xerr
	}
	rows, err := s.repo.Retryable(ctx, orgID, id, req.Cause, req.Lines)
	if err != nil {
		return nil, errx.InternalError()
	}
	if len(rows) == 0 {
		return nil, errx.NewWithIdentifier(errx.Conflict, "mailbox_import_nothing_to_retry",
			"No failed rows here still hold their credentials. Import them again instead.")
	}
	var ciph *cipher.Cipher
	if req.Password != "" {
		c, cerr := s.cipher.Cipher(ctx, orgID)
		if cerr != nil {
			errs.CaptureException(cerr)
			return nil, errx.InternalError()
		}
		ciph = c
	}
	for _, w := range rows {
		resealed := ""
		if ciph != nil {
			p, xerr := unseal(ctx, ciph, w.Payload)
			if xerr != nil {
				return nil, xerr
			}
			pw := req.Password
			applyFix(&p, w.Email, models.MailboxImportRowFix{Password: &pw})
			raw, _ := json.Marshal(p)
			if resealed, err = ciph.Encrypt(ctx, string(raw)); err != nil {
				errs.CaptureException(err)
				return nil, errx.InternalError()
			}
		}
		if err := s.repo.Requeue(ctx, id, w.Line, resealed); err != nil {
			return nil, errx.InternalError()
		}
	}
	if err := s.repo.Reopen(ctx, orgID, id); err != nil {
		return nil, errx.InternalError()
	}
	s.audit(ctx, orgID, userID, models.AuditActionUpdate, id, map[string]string{"retried": strconv.Itoa(len(rows))})
	s.Kick()
	return s.Get(ctx, orgID, id)
}

// Cancel stops the rows not yet started; rows already connecting finish.
func (s *Service) Cancel(ctx context.Context, orgID, userID, id uuid.UUID) (*models.MailboxImport, *errx.Error) {
	if _, xerr := s.Get(ctx, orgID, id); xerr != nil {
		return nil, xerr
	}
	if err := s.repo.Cancel(ctx, orgID, id); err != nil {
		return nil, errx.InternalError()
	}
	s.audit(ctx, orgID, userID, models.AuditActionUpdate, id, map[string]string{"status": models.ImportCancelled})
	s.publish(ctx, orgID, id, models.ImportCancelled, true)
	return s.Get(ctx, orgID, id)
}

// FailedCSV is every row that did not connect, as uploaded minus secrets, with the reason and the fix.
func (s *Service) FailedCSV(ctx context.Context, orgID, id uuid.UUID) ([]byte, string, *errx.Error) {
	imp, xerr := s.Get(ctx, orgID, id)
	if xerr != nil {
		return nil, "", xerr
	}
	statuses := []string{models.ImportRowFailed, models.ImportRowNeedsSignin, models.ImportRowCancelled}
	var all []models.MailboxImportRow
	after := 0
	for {
		rows, err := s.repo.ListRows(ctx, orgID, id, statuses, "", after, 1000)
		if err != nil {
			return nil, "", errx.InternalError()
		}
		all = append(all, rows...)
		if len(rows) < 1000 {
			break
		}
		after = rows[len(rows)-1].Line
	}
	headers := []string{}
	seen := map[string]bool{}
	for _, r := range all {
		for h := range r.Fields {
			if !seen[h] {
				seen[h] = true
				headers = append(headers, h)
			}
		}
	}
	sort.Strings(headers)
	var buf bytes.Buffer
	buf.WriteString("\ufeff")
	w := csv.NewWriter(&buf)
	safeHeaders := make([]string, len(headers))
	for i, h := range headers {
		safeHeaders[i] = csvSafe(h)
	}
	_ = w.Write(append(append([]string{"line"}, safeHeaders...), "status", "problem", "how_to_fix"))
	for _, r := range all {
		rec := []string{strconv.Itoa(r.Line)}
		for _, h := range headers {
			rec = append(rec, csvSafe(r.Fields[h]))
		}
		info := causeInfo(r.Cause)
		msg := r.Message
		if msg == "" {
			msg = info.Title
		}
		rec = append(rec, r.Status, csvSafe(msg), info.Fix)
		_ = w.Write(rec)
	}
	w.Flush()
	name := "mailbox-import-failed.csv"
	if base := strings.TrimSuffix(imp.Filename, ".csv"); base != "" && imp.Filename != "" {
		name = strings.TrimSuffix(strings.TrimSuffix(base, ".xlsx"), ".tsv") + "-failed.csv"
	}
	return buf.Bytes(), name, nil
}

// csvSafe keeps a spreadsheet from reading a cell as a formula.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func unseal(ctx context.Context, ciph *cipher.Cipher, sealed string) (payload, *errx.Error) {
	var p payload
	raw, err := ciph.Decrypt(ctx, sealed)
	if err != nil {
		errs.CaptureException(err)
		return p, errx.InternalError()
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		errs.CaptureException(err)
		return p, errx.InternalError()
	}
	return p, nil
}

func (s *Service) audit(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, id uuid.UUID, meta map[string]string) {
	if s.auditor == nil {
		return
	}
	s.auditor.LogAction(ctx, orgID, actorID, action, models.AuditEntityMailboxImport, &id, "", "", nil, meta)
}

// ListRow is one mailbox picked from a vendor account or a granted directory.
type ListRow struct {
	Email string
	Name  string
	// GrantID connects through an administrator's grant.
	GrantID *uuid.UUID
	// Vendor rows fetch their credentials when worked.
	VendorConnectionID *uuid.UUID
	VendorMailboxID    string
	VendorProvider     string
}

// ListInput is an import of picked mailboxes rather than a file.
type ListInput struct {
	OrgID   uuid.UUID
	UserID  uuid.UUID
	Source  string // "vendor" or "workspace"
	Vendor  string // vendor id, or "google"/"microsoft"
	Label   string
	Rows    []ListRow
	Options models.MailboxImportOptions
}

// CreateFromList stores an import of picked mailboxes and wakes the runner.
func (s *Service) CreateFromList(ctx context.Context, in ListInput) (*models.MailboxImport, *errx.Error) {
	onExisting := in.Options.OnExisting
	if onExisting == "" {
		onExisting = "update"
	}
	if onExisting != "update" && onExisting != "skip" {
		return nil, errx.New(errx.BadRequest, "on_existing must be update or skip")
	}
	if xerr := validateSettings(in.Options.Settings); xerr != nil {
		return nil, xerr
	}
	if xerr := validateDomainChoices(in.Options); xerr != nil {
		return nil, xerr
	}
	if len(in.Rows) == 0 {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "mailbox_import_empty", "Pick at least one mailbox.")
	}
	if len(in.Rows) > config.MailboxImportMaxRows {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "mailbox_import_too_large", "One import takes up to 5,000 mailboxes.")
	}
	ciph, err := s.cipher.Cipher(ctx, in.OrgID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	seen := map[string]bool{}
	inserts := make([]repository.MailboxImportRowInsert, 0, len(in.Rows))
	for i, r := range in.Rows {
		email := strings.TrimSpace(r.Email)
		ins := repository.MailboxImportRowInsert{
			Line: i + 1, Email: email, Domain: domainOf(email), Status: models.ImportRowQueued,
			Fields: map[string]string{"email": email, "name": r.Name},
		}
		key := strings.ToLower(email)
		switch {
		case !emailRe.MatchString(email):
			ins.Status, ins.Code, ins.Cause, ins.Message = models.ImportRowFailed, causeInvalidEmail, causeInvalidEmail, "Not an email address."
		case seen[key]:
			ins.Status, ins.Code, ins.Cause, ins.Message = models.ImportRowSkipped, causeDuplicateRow, causeDuplicateRow, "Picked twice."
		}
		seen[key] = true
		if ins.Status == models.ImportRowQueued {
			p := payload{
				Name: strings.TrimSpace(r.Name), NameGiven: strings.TrimSpace(r.Name) != "",
				GrantID: r.GrantID, VendorConnectionID: r.VendorConnectionID, VendorMailboxID: r.VendorMailboxID, VendorProvider: r.VendorProvider,
			}
			if p.Name == "" {
				p.Name = nameFromEmail(email)
			}
			switch {
			case r.GrantID != nil && in.Vendor == models.GrantProviderGoogle:
				p.MailHost, p.AuthMethod = "google_workspace", models.MailAuthDelegated
			case r.GrantID != nil:
				p.MailHost, p.AuthMethod = "microsoft365", models.MailAuthDelegated
			}
			ins.MailHost = p.MailHost
			p.TrackingDomain, p.RedirectURL = domainChoices(in.Options, ins.Domain)
			raw, _ := json.Marshal(p)
			sealed, err := ciph.Encrypt(ctx, string(raw))
			if err != nil {
				errs.CaptureException(err)
				return nil, errx.InternalError()
			}
			ins.Payload = sealed
		}
		inserts = append(inserts, ins)
	}
	settings, _ := json.Marshal(in.Options.Settings)
	columns, _ := json.Marshal([]string{"email", "name"})
	creator := in.UserID
	imp := &models.MailboxImport{
		ID: uuid.New(), OrganizationID: in.OrgID, CreatedBy: &creator, Status: models.ImportRunning,
		Source: in.Source, Filename: trimFilename(in.Label), Vendor: in.Vendor, OnExisting: onExisting, Total: len(inserts),
	}
	if err := s.repo.Create(ctx, imp, settings, columns, inserts); err != nil {
		return nil, errx.InternalError()
	}
	s.audit(ctx, in.OrgID, in.UserID, models.AuditActionCreate, imp.ID, map[string]string{
		"rows": strconv.Itoa(len(inserts)), "source": in.Source, "vendor": in.Vendor,
	})
	s.Kick()
	return s.Get(ctx, in.OrgID, imp.ID)
}
