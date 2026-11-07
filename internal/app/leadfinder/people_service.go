package leadfinder

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emailverify"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// guessPeopleMax is how many people per lead get an address worked out.
	guessPeopleMax = 10
	// probesPerLead bounds the address checks one lead's people cost a run.
	probesPerLead = 15
	// probesPerPerson bounds the checks one on-request search makes.
	probesPerPerson   = 8
	personFindTimeout = 45 * time.Second
)

// Confidence that a person's address reaches them, by how it was arrived at.
const (
	confPublishedValid = 100
	confPatternValid   = 95
	confPublished      = 80
	confManual         = 80
	confKnownPattern   = 60
	confGuess          = 25
)

// mailDomain is the domain a lead's people get addresses at: the site's own
// after redirects, else the one its listing gave.
func mailDomain(l *models.Lead) string {
	if l.Site.FinalURL != "" {
		if _, d := NormalizeWebsite(l.Site.FinalURL); d != "" {
			return d
		}
	}
	return l.Domain
}

func toLeadPerson(f FoundPerson) models.LeadPerson {
	p := models.LeadPerson{
		Name: f.Name, FirstName: f.First, LastName: f.Last, Title: f.Title, Seniority: f.Seniority,
		LinkedInURL: f.LinkedIn, Phone: f.Phone, Source: f.Source, SourceURL: f.URL,
	}
	if p.Seniority == "" {
		p.Seniority = seniorityOf(p.Title)
	}
	p.NameKey = nameKey(p.FirstName, p.LastName, p.Name)
	if f.Email != "" {
		p.Email, p.EmailSource, p.EmailConfidence = f.Email, models.LeadEmailWebsite, confPublished
	}
	return p
}

// mergeFound folds what a crawl found into the people a lead already has,
// filling blanks and never overwriting what a teammate typed in. Someone an
// earlier read found that this one did not is returned as stale, unless a
// teammate added them, typed their address or made them a contact.
func mergeFound(existing []models.LeadPerson, found []FoundPerson) (people, stale []models.LeadPerson) {
	seen := map[string]bool{}
	for _, f := range found {
		seen[nameKey(f.First, f.Last, f.Name)] = true
	}
	var out []models.LeadPerson
	for _, p := range existing {
		if !seen[p.NameKey] && p.Source != models.LeadPersonManual && p.EmailSource != models.LeadEmailManual && p.ContactID == nil {
			stale = append(stale, p)
			continue
		}
		out = append(out, p)
	}
	byKey := map[string]int{}
	for i, p := range out {
		byKey[p.NameKey] = i
	}
	for _, f := range found {
		np := toLeadPerson(f)
		i, ok := byKey[np.NameKey]
		if !ok {
			byKey[np.NameKey] = len(out)
			out = append(out, np)
			continue
		}
		cur := &out[i]
		if cur.Title == "" {
			cur.Title = np.Title
		}
		cur.Seniority = seniorityOf(cur.Title)
		if cur.LinkedInURL == "" {
			cur.LinkedInURL = np.LinkedInURL
		}
		if cur.Phone == "" {
			cur.Phone = np.Phone
		}
		if cur.SourceURL == "" {
			cur.SourceURL = np.SourceURL
		}
		if np.Email != "" && cur.EmailSource != models.LeadEmailManual && (cur.Email == "" || cur.EmailSource == models.LeadEmailPattern) {
			cur.Email, cur.EmailSource, cur.EmailStatus, cur.EmailConfidence = np.Email, np.EmailSource, "", np.EmailConfidence
		}
	}
	return out, stale
}

// addressFinder works out people's addresses at one domain, sharing what it
// learns (the pattern, whether the server accepts anything) across them.
type addressFinder struct {
	s       *Service
	orgID   uuid.UUID
	domain  string
	pattern string
	// acceptsAll: the server takes any address, so a guess cannot be confirmed.
	acceptsAll bool
	// blind: checks come back undecided (a provider that hides its mailboxes, a
	// blocked port), so guessing more costs checks and learns nothing.
	blind  bool
	noMail bool
	verify bool
	probes int
	budget int
}

func (s *Service) newAddressFinder(orgID uuid.UUID, l *models.Lead, verify bool, budget int) *addressFinder {
	return &addressFinder{
		s: s, orgID: orgID, domain: mailDomain(l), pattern: l.Site.EmailPattern, acceptsAll: l.Site.AcceptsAll,
		verify: verify && s.Verifier != nil, budget: budget,
	}
}

func (f *addressFinder) check(ctx context.Context, email string) (emailverify.Result, bool) {
	if !f.verify || f.probes >= f.budget || ctx.Err() != nil {
		return emailverify.Result{}, false
	}
	f.probes++
	return f.s.Verifier.VerifyAddress(ctx, f.orgID, email), true
}

// settle records what checking an address said about the domain.
func (f *addressFinder) settle(r emailverify.Result) {
	switch {
	case r.IsCatchAll || r.SubStatus == emailverify.SubStatusCatchAll:
		f.acceptsAll = true
	case r.SubStatus == emailverify.SubStatusNoMX:
		f.noMail = true
	case r.Status == emailverify.StatusUnknown || r.SubStatus == emailverify.SubStatusUndisclosed:
		f.blind = true
	}
}

// find fills in one person's address. It returns false when it found none.
func (f *addressFinder) find(ctx context.Context, p *models.LeadPerson) bool {
	// An address someone typed or the site published is only checked.
	if p.Email != "" && p.EmailSource != models.LeadEmailPattern {
		if p.EmailStatus == "" {
			if r, ok := f.check(ctx, p.Email); ok {
				p.EmailStatus = string(r.Status)
				if r.Status == emailverify.StatusValid && p.EmailSource == models.LeadEmailWebsite {
					p.EmailConfidence = confPublishedValid
				}
				f.settle(r)
			}
		}
		return true
	}
	if p.Email != "" && p.EmailStatus == string(emailverify.StatusValid) {
		return true
	}
	first, last := p.FirstName, p.LastName
	if first == "" {
		n := looseName("", "", p.Name)
		first, last = n.first, n.last
	}
	list := candidates(f.pattern, first, last, f.domain, probeOrder+1)
	if len(list) == 0 || f.noMail {
		return false
	}
	guessed := func(email string) bool {
		conf := confGuess
		if f.pattern != "" && email == list[0] {
			conf = confKnownPattern
		}
		p.Email, p.EmailSource, p.EmailStatus, p.EmailConfidence = email, models.LeadEmailPattern, models.LeadPersonStatusGuessed, conf
		return true
	}
	if f.acceptsAll || f.blind || !f.verify {
		return guessed(list[0])
	}
	for _, c := range list {
		r, ok := f.check(ctx, c)
		if !ok {
			return guessed(list[0])
		}
		f.settle(r)
		switch {
		case r.Status == emailverify.StatusValid:
			p.Email, p.EmailSource, p.EmailStatus, p.EmailConfidence = c, models.LeadEmailPattern, string(r.Status), confPatternValid
			if f.pattern == "" {
				if at := strings.LastIndexByte(c, '@'); at > 0 {
					f.pattern = patternOf(c[:at], first, last)
				}
			}
			return true
		case f.noMail:
			p.Email, p.EmailSource, p.EmailStatus, p.EmailConfidence = "", models.LeadEmailNone, "", 0
			return false
		case f.acceptsAll || f.blind:
			return guessed(list[0])
		}
		// Rejected: the next shape may be the one.
	}
	// Every shape was refused: this person has no address there.
	if p.EmailSource == models.LeadEmailPattern {
		p.Email, p.EmailSource, p.EmailStatus, p.EmailConfidence = "", models.LeadEmailNone, "", 0
	}
	return false
}

// findAll works out addresses for a lead's people, most senior first, and
// marks any the workspace has suppressed.
func (f *addressFinder) findAll(ctx context.Context, people []models.LeadPerson) {
	order := make([]int, len(people))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return seniorityRank(people[order[a]].Seniority) < seniorityRank(people[order[b]].Seniority)
	})
	for n, i := range order {
		if n >= guessPeopleMax || ctx.Err() != nil {
			break
		}
		f.find(ctx, &people[i])
	}
	f.markSuppressed(ctx, people)
}

func (f *addressFinder) markSuppressed(ctx context.Context, people []models.LeadPerson) {
	var addrs []string
	for _, p := range people {
		if p.Email != "" {
			addrs = append(addrs, p.Email)
		}
	}
	if len(addrs) == 0 {
		return
	}
	suppressed, err := f.s.Repo.SuppressedEmails(ctx, f.orgID, addrs)
	if err != nil {
		log.Warn().Err(err).Msg("leadfinder: could not check suppression for people")
		return
	}
	for i := range people {
		if suppressed[people[i].Email] {
			people[i].EmailStatus = "suppressed"
		}
	}
}

// remember keeps what the finder learned about the domain on the lead.
func (f *addressFinder) remember(l *models.Lead) {
	if f.pattern != "" {
		l.Site.EmailPattern = f.pattern
	}
	if f.acceptsAll {
		l.Site.AcceptsAll = true
	}
}

// enrichPeople merges what a crawl found into the lead's people, works out
// their addresses and saves them. It returns how many people the crawl named.
func (s *Service) enrichPeople(ctx context.Context, run *models.LeadEnrichmentRun, l *models.Lead, found []FoundPerson) (int, error) {
	existing, err := s.Repo.PeopleForLeads(ctx, run.OrganizationID, []uuid.UUID{l.ID})
	if err != nil {
		return 0, err
	}
	people, stale := mergeFound(existing[l.ID], found)
	for _, p := range stale {
		if _, err := s.Repo.DeletePerson(ctx, run.OrganizationID, l.ID, p.ID); err != nil {
			return 0, err
		}
	}
	if len(people) == 0 {
		return 0, nil
	}
	f := s.newAddressFinder(run.OrganizationID, l, run.Verify, probesPerLead)
	f.findAll(ctx, people)
	f.remember(l)
	if _, err := s.Repo.SavePeople(ctx, run.OrganizationID, l.ID, people); err != nil {
		if errors.Is(err, repository.ErrLeadNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return len(found), nil
}

// --- people a teammate adds or edits ----------------------------------------

// PersonInput is a person typed in on a lead. Name or a first name is required.
type PersonInput struct {
	Name        string `json:"name"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	Title       string `json:"title"`
	Email       string `json:"email"`
	LinkedInURL string `json:"linkedin_url"`
	Phone       string `json:"phone"`
}

// PersonPatch edits a person. Nil fields are left alone.
type PersonPatch struct {
	Name        *string `json:"name"`
	FirstName   *string `json:"first_name"`
	LastName    *string `json:"last_name"`
	Title       *string `json:"title"`
	Email       *string `json:"email"`
	LinkedInURL *string `json:"linkedin_url"`
	Phone       *string `json:"phone"`
}

func checkPersonFields(p *models.LeadPerson) error {
	switch {
	case p.NameKey == "" || p.FirstName == "":
		return errx.New(errx.BadRequest, "enter the person's name")
	case len([]rune(p.Name)) > 120 || len([]rune(p.FirstName)) > 60 || len([]rune(p.LastName)) > 60:
		return errx.New(errx.BadRequest, "a name is limited to 120 characters")
	case len([]rune(p.Title)) > 120:
		return errx.New(errx.BadRequest, "a title is limited to 120 characters")
	case p.LinkedInURL != "" && !strings.Contains(strings.ToLower(p.LinkedInURL), "linkedin.com/"):
		return errx.New(errx.BadRequest, "the LinkedIn link must point at linkedin.com")
	case len(p.LinkedInURL) > 300 || len(p.Phone) > 40:
		return errx.New(errx.BadRequest, "a link or phone number is too long")
	}
	return nil
}

func setTypedEmail(p *models.LeadPerson, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		p.Email, p.EmailSource, p.EmailStatus, p.EmailConfidence = "", models.LeadEmailNone, "", 0
		return nil
	}
	e := cleanEmail(raw)
	if e == "" {
		return errx.New(errx.BadRequest, "the email address is not valid")
	}
	p.Email, p.EmailSource, p.EmailStatus, p.EmailConfidence = e, models.LeadEmailManual, "", confManual
	return nil
}

// AddPerson adds someone to a lead. Without an address, it works one out at the
// lead's domain the way FindPersonEmail does.
func (s *Service) AddPerson(ctx context.Context, orgID, leadID uuid.UUID, in PersonInput) (*models.LeadPerson, error) {
	l, err := s.Get(ctx, orgID, leadID)
	if err != nil {
		return nil, err
	}
	n := looseName(in.FirstName, in.LastName, in.Name)
	p := models.LeadPerson{
		Name: n.full, FirstName: n.first, LastName: n.last, Title: strings.TrimSpace(in.Title),
		LinkedInURL: strings.TrimSpace(in.LinkedInURL), Phone: strings.TrimSpace(in.Phone), Source: models.LeadPersonManual,
	}
	p.Seniority = seniorityOf(p.Title)
	p.NameKey = nameKey(p.FirstName, p.LastName, p.Name)
	if err := checkPersonFields(&p); err != nil {
		return nil, err
	}
	if err := setTypedEmail(&p, in.Email); err != nil {
		return nil, err
	}
	return s.findAndSave(ctx, orgID, l, p)
}

// UpdatePerson edits someone on a lead.
func (s *Service) UpdatePerson(ctx context.Context, orgID, leadID, id uuid.UUID, patch PersonPatch) (*models.LeadPerson, error) {
	p, err := s.person(ctx, orgID, leadID, id)
	if err != nil {
		return nil, err
	}
	if patch.Name != nil || patch.FirstName != nil || patch.LastName != nil {
		first, last, full := p.FirstName, p.LastName, p.Name
		if patch.FirstName != nil {
			first = *patch.FirstName
		}
		if patch.LastName != nil {
			last = *patch.LastName
		}
		if patch.Name != nil {
			full = *patch.Name
			if patch.FirstName == nil && patch.LastName == nil {
				first, last = "", ""
			}
		} else {
			full = strings.TrimSpace(first + " " + last)
		}
		n := looseName(first, last, full)
		p.Name, p.FirstName, p.LastName = n.full, n.first, n.last
		p.NameKey = nameKey(p.FirstName, p.LastName, p.Name)
	}
	if patch.Title != nil {
		p.Title = strings.TrimSpace(*patch.Title)
		p.Seniority = seniorityOf(p.Title)
	}
	if patch.LinkedInURL != nil {
		p.LinkedInURL = strings.TrimSpace(*patch.LinkedInURL)
	}
	if patch.Phone != nil {
		p.Phone = strings.TrimSpace(*patch.Phone)
	}
	if err := checkPersonFields(p); err != nil {
		return nil, err
	}
	if patch.Email != nil && !strings.EqualFold(strings.TrimSpace(*patch.Email), p.Email) {
		if err := setTypedEmail(p, *patch.Email); err != nil {
			return nil, err
		}
		if p.Email != "" {
			l, err := s.Get(ctx, orgID, leadID)
			if err != nil {
				return nil, err
			}
			pctx, cancel := context.WithTimeout(ctx, personFindTimeout)
			defer cancel()
			f := s.newAddressFinder(orgID, l, true, 1)
			f.find(pctx, p)
			one := []models.LeadPerson{*p}
			f.markSuppressed(pctx, one)
			*p = one[0]
		}
	}
	return s.savePerson(ctx, orgID, leadID, *p)
}

// FindPersonEmail works out a person's address at the lead's domain again,
// checking each likely shape until one is confirmed.
func (s *Service) FindPersonEmail(ctx context.Context, orgID, leadID, id uuid.UUID) (*models.LeadPerson, error) {
	l, err := s.Get(ctx, orgID, leadID)
	if err != nil {
		return nil, err
	}
	p, err := s.person(ctx, orgID, leadID, id)
	if err != nil {
		return nil, err
	}
	if p.EmailSource == models.LeadEmailPattern {
		p.Email, p.EmailSource, p.EmailStatus, p.EmailConfidence = "", models.LeadEmailNone, "", 0
	}
	if p.Email != "" {
		p.EmailStatus = ""
	}
	return s.findAndSave(ctx, orgID, l, *p)
}

// findAndSave works out p's address when it has none, charging for the search
// when the instance is metered and refunding it when nothing turned up.
func (s *Service) findAndSave(ctx context.Context, orgID uuid.UUID, l *models.Lead, p models.LeadPerson) (*models.LeadPerson, error) {
	pctx, cancel := context.WithTimeout(ctx, personFindTimeout)
	defer cancel()
	searching := p.Email == ""
	if searching && mailDomain(l) == "" {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "lead_person_no_domain", "This business has no website, so there is no domain to work out an address at")
	}
	key := "lead_person:" + uuid.NewString()
	if searching && s.metered() {
		if _, err := s.Credits.ChargeUpfront(pctx, orgID, credits.UnitUsage(instancesettings.UnitLeadPersonEmail, 1), "lead_person_email", key); err != nil {
			return nil, creditErr(err)
		}
	}
	f := s.newAddressFinder(orgID, l, true, probesPerPerson)
	found := f.find(pctx, &p)
	one := []models.LeadPerson{p}
	f.markSuppressed(pctx, one)
	p = one[0]
	if searching && s.metered() && !found {
		if _, err := s.Credits.RefundCharge(context.WithoutCancel(pctx), orgID, key, "lead_person_email_refund"); err != nil {
			log.Warn().Err(err).Msg("leadfinder: could not refund a person search that found nothing")
		}
	}
	if f.pattern != l.Site.EmailPattern || f.acceptsAll != l.Site.AcceptsAll {
		f.remember(l)
		if err := s.Repo.SaveSite(ctx, orgID, l.ID, l.Site); err != nil {
			log.Warn().Err(err).Msg("leadfinder: could not store a domain's address pattern")
		}
	}
	saved, err := s.savePerson(ctx, orgID, l.ID, p)
	if err != nil && searching && s.metered() && found {
		if _, rerr := s.Credits.RefundCharge(context.WithoutCancel(pctx), orgID, key, "lead_person_email_refund"); rerr != nil {
			log.Warn().Err(rerr).Msg("leadfinder: could not refund a person search that was not saved")
		}
	}
	return saved, err
}

func (s *Service) savePerson(ctx context.Context, orgID, leadID uuid.UUID, p models.LeadPerson) (*models.LeadPerson, error) {
	saved, err := s.Repo.SavePeople(ctx, orgID, leadID, []models.LeadPerson{p})
	switch {
	case errors.Is(err, repository.ErrLeadNotFound):
		return nil, errx.New(errx.NotFound, "lead not found")
	case errors.Is(err, repository.ErrLeadPersonExists):
		return nil, errx.NewWithIdentifier(errx.Conflict, "lead_person_exists", "Someone with this name is already on this lead")
	case err != nil:
		return nil, err
	case len(saved) == 0:
		return nil, errx.New(errx.NotFound, "person not found")
	}
	return &saved[0], nil
}

func (s *Service) person(ctx context.Context, orgID, leadID, id uuid.UUID) (*models.LeadPerson, error) {
	p, err := s.Repo.GetPerson(ctx, orgID, leadID, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errx.New(errx.NotFound, "person not found")
	}
	return p, nil
}

// DeletePerson removes someone from a lead. A contact made from them stays.
func (s *Service) DeletePerson(ctx context.Context, orgID, leadID, id uuid.UUID) error {
	ok, err := s.Repo.DeletePerson(ctx, orgID, leadID, id)
	if err != nil {
		return err
	}
	if !ok {
		return errx.New(errx.NotFound, "person not found")
	}
	return nil
}
