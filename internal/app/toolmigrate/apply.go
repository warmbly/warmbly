package toolmigrate

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/campaign"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/mailboximport"
	"github.com/warmbly/warmbly/internal/models"
	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
	"github.com/warmbly/warmbly/internal/pkg/tmplvars"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/utils"
)

// Item kinds recorded per target so a resumed run never writes twice.
const (
	itemCampaign      = "campaign"
	itemVariant       = "variant"
	itemMailboxImport = "mailbox_import"
	itemContact       = "contact"
)

const (
	suppressionBatch = 5000
	maxCustomKeys    = 100
	maxCellLen       = 500
	maxWaitDays      = 60
)

// pages walks a provider listing until its last page, checkpointing after each.
func (r *run) pages(read func(page string) (next string, err error)) error {
	for {
		prev := r.cur.Page
		next, err := read(prev)
		if err != nil {
			return err
		}
		if next == prev {
			next = ""
		}
		r.cur.Page = next
		if err := r.save(); err != nil {
			return err
		}
		if next == "" {
			return nil
		}
	}
}

func unsupported(err error) bool { return errors.Is(err, source.ErrUnsupported) }

// blocklist adds the source's blocked addresses and domains to the workspace's suppressions.
func (r *run) blocklist() error {
	reason := truncate("Imported from "+r.label, 200)
	err := r.pages(func(page string) (string, error) {
		entries, next, err := r.sess.Blocklist(r.parent, r.ws, page)
		if err != nil {
			return "", err
		}
		values := make([]string, 0, len(entries))
		for _, e := range entries {
			if v := strings.TrimSpace(e.Value); v != "" {
				values = append(values, v)
			}
		}
		added, err := r.suppress(values, reason)
		r.counts.BlocklistImported += added
		return next, err
	})
	if unsupported(err) {
		return nil
	}
	return err
}

// suppress adds values in batches the suppression list accepts.
func (r *run) suppress(values []string, reason string) (int, error) {
	added := 0
	for start := 0; start < len(values); start += suppressionBatch {
		end := min(start+suppressionBatch, len(values))
		entries := make([]models.SuppressionEntry, 0, end-start)
		for _, v := range values[start:end] {
			entries = append(entries, models.SuppressionEntry{Value: v})
		}
		res, xerr := r.s.d.Outreach.AddSuppressions(r.ctx, r.job.OrgID, r.actor, &models.AddSuppressionsRequest{Entries: entries, Reason: reason})
		if xerr != nil {
			return added, fail("Blocked addresses could not be added: %s", xerr.Message)
		}
		added += res.Added
	}
	return added, nil
}

// mailboxColumns are the mailbox import's columns, in cell order.
var mailboxColumns = []models.MailboxImportField{
	models.ImportFieldEmail, models.ImportFieldName,
	models.ImportFieldSMTPHost, models.ImportFieldSMTPPort, models.ImportFieldSMTPSecurity, models.ImportFieldSMTPUsername, models.ImportFieldSMTPPassword,
	models.ImportFieldIMAPHost, models.ImportFieldIMAPPort, models.ImportFieldIMAPSecurity, models.ImportFieldIMAPUsername, models.ImportFieldIMAPPassword,
	models.ImportFieldDailyLimit, models.ImportFieldWarmup, models.ImportFieldReplyTo,
}

// mailboxes connects SMTP mailboxes the source returned passwords for, and lists the rest to reconnect.
func (r *run) mailboxes() error {
	if _, done, err := r.s.d.Repo.Item(r.ctx, r.job.ID, itemMailboxImport, ""); err != nil || done {
		return err
	}
	list, err := r.sess.Mailboxes(r.parent, r.ws)
	if unsupported(err) {
		return nil
	}
	if err != nil {
		return err
	}
	emails := make([]string, 0, len(list))
	for _, m := range list {
		emails = append(emails, strings.ToLower(strings.TrimSpace(m.Email)))
	}
	have, err := r.s.d.Repo.MailboxIDsByEmail(r.ctx, r.job.OrgID, emails)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	header := make([]string, len(mailboxColumns))
	for i, f := range mailboxColumns {
		header[i] = string(f)
	}
	_ = w.Write(header)
	var queued, manual []models.ToolMigrationMailbox
	seen := map[string]bool{}
	for _, m := range list {
		addr := strings.ToLower(strings.TrimSpace(m.Email))
		if addr == "" || seen[addr] {
			continue
		}
		seen[addr] = true
		if _, connected := have[addr]; connected {
			continue
		}
		if !m.HasSecrets() || !r.job.ViaSession || r.s.d.Mailboxes == nil {
			manual = append(manual, models.ToolMigrationMailbox{Email: addr, Kind: m.Kind})
			continue
		}
		_ = w.Write(mailboxRow(addr, m, r.opts.Warmup))
		queued = append(queued, models.ToolMigrationMailbox{Email: addr, Kind: m.Kind})
	}
	w.Flush()

	// Reserved right before the import is created, so two holders never connect the same mailboxes twice.
	state, err := r.reserve(itemMailboxImport, "")
	if err != nil {
		return err
	}
	if state != repository.ItemReserved {
		if state == repository.ItemPending {
			r.note("Connecting mailboxes from %s may already have started. Check the mailbox imports before connecting them again.", r.label)
		}
		return nil
	}
	if len(queued) > 0 {
		mapping := models.MailboxImportMapping{}
		for i, f := range mailboxColumns {
			mapping[strconv.Itoa(i)] = f
		}
		hasHeader := true
		imp, xerr := r.s.d.Mailboxes.Create(r.ctx, mailboximport.CreateInput{
			OrgID: r.job.OrgID, UserID: r.actor,
			Input:   mailboximport.Input{Filename: truncate(r.label+" mailboxes.csv", 200), Text: buf.String()},
			Mapping: mapping,
			Options: models.MailboxImportOptions{HasHeader: &hasHeader, OnExisting: "skip"},
		})
		if xerr != nil {
			r.note("Mailboxes from %s could not be connected automatically (%s). Connect them from the list below.", r.label, xerr.Message)
			manual = append(manual, queued...)
		} else {
			r.mailboxImportID = &imp.ID
			r.counts.MailboxesQueued += len(queued)
		}
	}
	buf.Reset()
	r.followup.Reconnect = append(r.followup.Reconnect, manual...)
	r.counts.MailboxesManual += len(manual)
	marker := uuid.Nil
	if r.mailboxImportID != nil {
		marker = *r.mailboxImportID
	}
	return r.complete(itemMailboxImport, "", marker)
}

// reserve claims an item before the write it records, refusing once the target is no longer ours.
func (r *run) reserve(kind, sourceID string) (repository.ToolMigrationItemState, error) {
	state, _, err := r.s.d.Repo.ReserveItem(r.ctx, r.job.ID, r.job.Token, r.job.OrgID, kind, sourceID)
	if err == nil && state == repository.ItemLost {
		err = errLost
	}
	return state, err
}

// complete records what a reserved item became; uuid.Nil marks one that was not created.
func (r *run) complete(kind, sourceID string, id uuid.UUID) error {
	held, err := r.s.d.Repo.CompleteItem(r.ctx, r.job.ID, r.job.Token, kind, sourceID, id)
	if err != nil {
		return err
	}
	if !held {
		return errLost
	}
	return nil
}

func mailboxRow(addr string, m source.Mailbox, warmup bool) []string {
	sec := func(s source.Security) string {
		if s == "" {
			return ""
		}
		if s == source.SecuritySSL {
			return "ssl"
		}
		return string(s)
	}
	port := func(n int) string {
		if n <= 0 {
			return ""
		}
		return strconv.Itoa(n)
	}
	user := func(s *source.Server) string {
		if s.Username != "" {
			return s.Username
		}
		return addr
	}
	daily := ""
	if m.DailyCap > 0 {
		daily = strconv.Itoa(m.DailyCap)
	}
	on := warmup && (m.Warmup == nil || *m.Warmup)
	replyTo := ""
	if strings.Contains(m.ReplyTo, "@") {
		replyTo = strings.TrimSpace(m.ReplyTo)
	}
	return []string{
		addr, truncate(m.FromName, 100),
		m.SMTP.Host, port(m.SMTP.Port), sec(m.SMTP.Security), user(m.SMTP), m.SMTP.Password,
		m.IMAP.Host, port(m.IMAP.Port), sec(m.IMAP.Security), user(m.IMAP), m.IMAP.Password,
		daily, strconv.FormatBool(on), replyTo,
	}
}

// sourceCampaigns lists the source campaigns this migration brings, in a stable
// order, read once per run for both the campaigns and the leads phase.
func (r *run) sourceCampaigns() ([]source.Campaign, error) {
	if r.campaignList != nil {
		return r.campaignList, nil
	}
	list, err := r.sess.Campaigns(r.parent, r.ws)
	if unsupported(err) {
		r.campaignList = []source.Campaign{}
		return r.campaignList, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]source.Campaign, 0, len(list))
	for _, c := range list {
		if len(r.opts.CampaignIDs) > 0 && !contains(r.opts.CampaignIDs, c.ID) {
			continue
		}
		if r.opts.SkipFinished && (c.Status == source.CampaignCompleted || c.Status == source.CampaignArchived) {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	r.campaignList = out
	return out, nil
}

// campaigns creates one draft campaign per source campaign, never launched.
func (r *run) campaigns() error {
	list, err := r.sourceCampaigns()
	if err != nil {
		return err
	}
	for i := r.cur.Campaign; i < len(list); i++ {
		if err := r.campaign(list[i]); err != nil {
			return err
		}
		r.cur.Campaign = i + 1
		if err := r.save(); err != nil {
			return err
		}
	}
	return nil
}

func (r *run) campaign(c source.Campaign) error {
	if _, done, err := r.s.d.Repo.Item(r.ctx, r.job.ID, itemCampaign, c.ID); err != nil || done {
		return err
	}
	name := campaignName(c.Name)
	made, err := r.s.d.Repo.CountItems(r.ctx, r.job.ID, itemCampaign)
	if err != nil {
		return err
	}
	if made >= campaignCap {
		r.counts.CampaignsSkipped++
		r.note("Only the first %d campaigns are brought over per workspace; the rest were left out.", campaignCap)
		return nil
	}

	data, plan := r.buildCampaign(c, name)
	senders := lowerAll(c.Senders)
	found, err := r.s.d.Repo.MailboxIDsByEmail(r.ctx, r.job.OrgID, senders)
	if err != nil {
		return err
	}
	var missing []string
	for _, addr := range senders {
		if id, ok := found[addr]; ok {
			data.Senders = append(data.Senders, models.CampaignSenderInput{EmailAccountID: id})
		} else if !contains(missing, addr) {
			missing = append(missing, addr)
		}
	}
	if len(data.Senders) > 0 {
		explicit := "explicit"
		data.SenderStrategy = &explicit
	}

	state, err := r.reserve(itemCampaign, c.ID)
	if err != nil {
		return err
	}
	switch state {
	case repository.ItemDone:
		return nil
	case repository.ItemPending:
		r.counts.CampaignsSkipped++
		r.note("%s may already have been created by an earlier attempt that stopped. Check your campaigns for it.", name)
		return nil
	}
	orgID := r.job.OrgID
	created, xerr := r.s.d.Campaigns.Create(campaign.WithMigrationCreate(r.ctx), r.actor.String(), &orgID, data)
	if xerr != nil {
		r.counts.CampaignsSkipped++
		r.note("%s could not be created: %s", name, xerr.Message)
		return r.complete(itemCampaign, c.ID, uuid.Nil)
	}
	if err := r.complete(itemCampaign, c.ID, created.ID); err != nil {
		return err
	}
	r.counts.CampaignsCreated++
	r.counts.StepsCreated += len(data.Sequences)
	r.followup.Campaigns = append(r.followup.Campaigns, models.ToolMigrationCampaign{
		SourceID: c.ID, CampaignID: created.ID, Name: created.Name, MissingSenders: missing,
	})
	for _, n := range plan.notes {
		r.note("%s: %s", name, n)
	}
	return r.variants(created.ID, name, plan.variants)
}

// campaignPlan is what building a campaign left for later: extra variants per step, and notes.
type campaignPlan struct {
	variants [][]source.Variant
	notes    []string
}

func (p *campaignPlan) note(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	for _, n := range p.notes {
		if n == msg {
			return
		}
	}
	p.notes = append(p.notes, msg)
}

var hhmm = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// buildCampaign maps a source campaign onto a draft Warmbly campaign.
func (r *run) buildCampaign(c source.Campaign, name string) (*models.CreateCampaign, *campaignPlan) {
	plan := &campaignPlan{}
	data := &models.CreateCampaign{Name: name, Description: truncate("Imported from "+r.label, 300)}
	stopOnReply, opens, links, textOnly := c.StopOnReply, c.TrackOpens, c.TrackClicks, c.PlainText
	data.StopOnReply, data.OpenTracking, data.LinkTracking, data.TextOnly = &stopOnReply, &opens, &links, &textOnly
	if c.DailyLimit > 0 {
		v := c.DailyLimit
		data.DailyLimit = &v
	}
	if c.MaxNewLeadsPerDay > 0 {
		v := c.MaxNewLeadsPerDay
		data.MaxNewLeadsPerDay = &v
	}
	r.schedule(c.Schedule, data, plan)

	emptyCopy := false
	for i, st := range c.Steps {
		main, extra := pickVariant(st.Variants)
		subject, body := strings.TrimSpace(main.Subject), main.Body
		if strings.TrimSpace(stripTags(body)) == "" {
			emptyCopy = true
		}
		subject, body = safeCopy(subject, body, main.HTML, func() {
			plan.note("step %d had merge fields Warmbly could not read, so they were removed. Check the copy before launching.", i+1)
		})
		seq := models.CreateSequenceInput{Name: fmt.Sprintf("Step %d", i+1), Subject: subject}
		if main.HTML {
			seq.BodyHTML = body
		} else {
			seq.BodyPlain = body
		}
		thread := i > 0 && !st.NewThread
		seq.ThreadReply = &thread
		if i == 0 {
			if mins := st.WaitDays*1440 + st.WaitMinutes; mins > 0 {
				mins = min(mins, maxWaitDays*1440)
				data.EntryDelayMinutes = &mins
			}
		}
		if i+1 < len(c.Steps) {
			next := c.Steps[i+1]
			days := waitDays(next.WaitDays, next.WaitMinutes)
			if days*1440 != max(next.WaitDays, 0)*1440+max(next.WaitMinutes, 0) {
				plan.note("the wait before step %d was rounded up to whole days.", i+2)
			}
			if days > maxWaitDays {
				days = maxWaitDays
				plan.note("the wait before step %d was shortened to %d days, the longest Warmbly allows.", i+2, maxWaitDays)
			}
			seq.WaitAfter = &days
		}
		data.Sequences = append(data.Sequences, seq)
		plan.variants = append(plan.variants, extra)
	}
	if emptyCopy || len(c.Steps) == 0 {
		plan.note("write the email copy before launching; %s does not share it through its API.", r.label)
	}
	for _, n := range c.UnmappedNotes {
		plan.note("%s", n)
	}
	return data, plan
}

// schedule copies the sending window when it is one Warmbly can hold.
func (r *run) schedule(sc *source.Schedule, data *models.CreateCampaign, plan *campaignPlan) {
	if sc == nil {
		return
	}
	if tz := strings.TrimSpace(sc.Timezone); tz != "" {
		if _, err := time.LoadLocation(tz); err == nil {
			data.Timezone = &tz
		} else {
			plan.note("its time zone %q is not one Warmbly knows, so the workspace's is used.", tz)
		}
	}
	var days uint8
	for _, d := range sc.Days {
		if d >= 1 && d <= 7 {
			days |= 1 << (d - 1)
		}
	}
	if days != 0 {
		data.Days = &days
	}
	if hhmm.MatchString(sc.From) && hhmm.MatchString(sc.To) && sc.From < sc.To {
		from, to := sc.From, sc.To
		data.StartTime, data.EndTime = &from, &to
	}
	now := time.Now()
	if sc.Start != nil && sc.Start.After(now) {
		start := *sc.Start
		data.StartDate = &start
	}
	if sc.End != nil {
		if sc.End.After(now) {
			end := *sc.End
			data.EndDate = &end
		} else {
			plan.note("its end date had passed, so it was left off.")
		}
	}
	if sc.MinGapMinutes > 0 {
		plan.note("the gap between sends is set per mailbox in Warmbly, so its %d minute gap was not copied.", sc.MinGapMinutes)
	}
}

// waitDays is a wait rounded up to whole days, never negative.
func waitDays(days, minutes int) int {
	total := max(days, 0)*1440 + max(minutes, 0)
	return int(math.Ceil(float64(total) / 1440))
}

// pickVariant takes the first enabled variant as the step's copy and returns the other enabled ones.
func pickVariant(vs []source.Variant) (source.Variant, []source.Variant) {
	main := -1
	for i, v := range vs {
		if !v.Disabled {
			main = i
			break
		}
	}
	if main < 0 {
		if len(vs) == 0 {
			return source.Variant{}, nil
		}
		return vs[0], nil
	}
	var extra []source.Variant
	for i, v := range vs {
		if i != main {
			extra = append(extra, v)
		}
	}
	return vs[main], extra
}

var (
	tokenRE = regexp.MustCompile(`\{\{[^{}]*\}\}`)
	tagRE   = regexp.MustCompile(`<[^>]*>`)
)

func stripTags(s string) string { return tagRE.ReplaceAllString(s, "") }

// safeCopy returns copy that renders: as written, then without merge fields, then without braces.
func safeCopy(subject, body string, html bool, changed func()) (string, string) {
	ok := func(sub, b string) bool {
		return tmplvars.CheckCopy(tmplvars.Part{Name: "subject", Text: sub}, tmplvars.Part{Name: "body", Text: b, HTML: html}) == nil
	}
	if ok(subject, body) {
		return subject, body
	}
	changed()
	sub, b := tokenRE.ReplaceAllString(subject, ""), tokenRE.ReplaceAllString(body, "")
	if ok(sub, b) {
		return sub, b
	}
	strip := strings.NewReplacer("{", "", "}", "")
	return strip.Replace(sub), strip.Replace(b)
}

// variants adds a step's other A/B versions after the campaign exists.
func (r *run) variants(campaignID uuid.UUID, name string, perStep [][]source.Variant) error {
	hasExtra := false
	for _, vs := range perStep {
		if len(vs) > 0 {
			hasExtra = true
		}
	}
	if !hasExtra {
		return nil
	}
	steps, xerr := r.s.d.Steps.Get(r.ctx, r.job.OrgID.String(), campaignID.String())
	if xerr != nil {
		r.note("%s: its A/B variants could not be added: %s", name, xerr.Message)
		return nil
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Position < steps[j].Position })
	for i, vs := range perStep {
		if i >= len(steps) {
			break
		}
		stepID := steps[i].ID
		for k, v := range vs {
			key := fmt.Sprintf("%s/%d/%d", campaignID, i, k)
			if state, err := r.reserve(itemVariant, key); err != nil {
				return err
			} else if state != repository.ItemReserved {
				continue
			}
			subject, body := safeCopy(strings.TrimSpace(v.Subject), v.Body, v.HTML, func() {
				r.note("%s: a variant of step %d had merge fields Warmbly could not read, so they were removed.", name, i+1)
			})
			active := !v.Disabled
			req := &models.CreateCampaignABVariantRequest{
				Name: "Variant " + string(rune('B'+k%25)), SequenceID: &stepID, Weight: 100, Subject: subject, IsActive: &active,
			}
			if v.HTML {
				req.BodyHTML = body
			} else {
				req.BodyPlain = body
			}
			made, xerr := r.s.d.Outreach.CreateABVariant(r.ctx, campaignID, req)
			id := uuid.Nil
			if xerr != nil {
				r.note("%s: a variant of step %d could not be added: %s", name, i+1, xerr.Message)
			} else {
				id = made.ID
			}
			if err := r.complete(itemVariant, key, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// leads imports each brought campaign's leads, then the account's leads outside any campaign.
func (r *run) leads() error {
	list, err := r.sourceCampaigns()
	if err != nil {
		return err
	}
	if err := r.ensureCategory(); err != nil {
		return err
	}
	for r.cur.Campaign < len(list) {
		c := list[r.cur.Campaign]
		var target *uuid.UUID
		if id, ok, err := r.s.d.Repo.Item(r.ctx, r.job.ID, itemCampaign, c.ID); err != nil {
			return err
		} else if ok && id != uuid.Nil {
			target = &id
		}
		detail := r.label + ": " + strings.TrimSpace(c.Name)
		err := r.pages(func(page string) (string, error) {
			batch, next, err := r.sess.Leads(r.parent, r.ws, c.ID, page)
			if err != nil {
				return "", err
			}
			r.cur.LeadsServed = true
			return next, r.importLeads(batch, target, detail, false)
		})
		if err != nil && !unsupported(err) {
			return err
		}
		r.cur.Campaign++
		r.cur.Page = ""
		if err := r.save(); err != nil {
			return err
		}
	}
	err = r.pages(func(page string) (string, error) {
		batch, next, err := r.sess.Leads(r.parent, r.ws, "", page)
		if err != nil {
			return "", err
		}
		r.cur.LeadsServed = true
		return next, r.importLeads(batch, nil, r.label, true)
	})
	if err != nil && !unsupported(err) {
		return err
	}
	// Leads outside campaigns are optional; a tool that cannot list them only costs those leads.
	if !r.cur.LeadsServed && unsupported(err) {
		r.note("%s does not share leads outside its campaigns through its API. Export any you need to a CSV file and import that.", r.label)
	}
	return nil
}

// ensureCategory files every imported contact under one category named for the source.
func (r *run) ensureCategory() error {
	if r.categoryID != nil || r.s.d.Categories == nil {
		return nil
	}
	title := truncate("From "+r.label, 50)
	ids, xerr := r.s.d.Categories.ResolveCategoryNames(r.ctx, r.job.OrgID, r.actor, []string{title})
	if xerr != nil {
		return fail("The contact category could not be created: %s", xerr.Message)
	}
	if id, ok := ids[strings.ToLower(title)]; ok {
		r.categoryID = &id
	}
	return nil
}

// leadGroup is one import call: who is enrolled, and how new contacts start.
type leadGroup struct {
	leads      []source.Lead
	enroll     bool
	subscribed bool
}

// importLeads writes one page: enrollable leads into the campaign, the rest as contacts only, opt-outs suppressed.
func (r *run) importLeads(batch []source.Lead, campaignID *uuid.UUID, detail string, accountLevel bool) error {
	r.counts.LeadsRead += len(batch)
	valid := make([]source.Lead, 0, len(batch))
	for _, l := range batch {
		l.Email = strings.ToLower(strings.TrimSpace(l.Email))
		if !strings.Contains(l.Email, "@") {
			r.counts.LeadsFailed++
			continue
		}
		valid = append(valid, l)
	}
	if accountLevel {
		emails := make([]string, len(valid))
		for i, l := range valid {
			emails[i] = l.Email
		}
		seen, err := r.s.d.Repo.ItemsIn(r.ctx, r.job.ID, itemContact, emails)
		if err != nil {
			return err
		}
		kept := valid[:0]
		for _, l := range valid {
			if !seen[l.Email] {
				kept = append(kept, l)
			}
		}
		valid = kept
	}

	enroll := leadGroup{enroll: true, subscribed: true}
	plain := leadGroup{subscribed: true}
	optOut := leadGroup{}
	var unsubscribed, bounced []string
	for _, l := range valid {
		switch {
		case l.Status == source.LeadUnsubscribed:
			optOut.leads = append(optOut.leads, l)
			unsubscribed = append(unsubscribed, l.Email)
		case l.Status == source.LeadBounced:
			optOut.leads = append(optOut.leads, l)
			bounced = append(bounced, l.Email)
		case campaignID == nil:
			plain.leads = append(plain.leads, l)
		case l.Status.Final(), l.Status == source.LeadPaused:
			plain.leads = append(plain.leads, l)
			r.counts.LeadsNotEnrolled++
		case (l.StepsSent > 0 || l.Status == source.LeadInProgress) && r.opts.InFlight != models.ToolMigrationRestart:
			plain.leads = append(plain.leads, l)
			r.counts.LeadsNotEnrolled++
		default:
			enroll.leads = append(enroll.leads, l)
		}
	}
	for _, g := range []leadGroup{enroll, plain, optOut} {
		if err := r.importGroup(g, campaignID, detail); err != nil {
			return err
		}
	}
	n, err := r.suppress(unsubscribed, truncate("Unsubscribed in "+r.label, 200))
	r.counts.LeadsSuppressed += n
	if err != nil {
		return err
	}
	n, err = r.suppress(bounced, truncate("Bounced in "+r.label, 200))
	r.counts.LeadsSuppressed += n
	return err
}

// leadColumns are the fixed contact fields, before the custom ones.
var leadColumns = []models.ContactImportColumnTarget{
	models.ContactImportTargetEmail, models.ContactImportTargetFirstName, models.ContactImportTargetLastName,
	models.ContactImportTargetCompany, models.ContactImportTargetPhone,
}

func (r *run) importGroup(g leadGroup, campaignID *uuid.UUID, detail string) error {
	if len(g.leads) == 0 {
		return nil
	}
	customs := make([]map[string]string, len(g.leads))
	keySet := map[string]bool{}
	for i, l := range g.leads {
		customs[i] = leadCustom(l)
		for k := range customs[i] {
			keySet[k] = true
		}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > maxCustomKeys {
		keys = keys[:maxCustomKeys]
		r.note("Leads from %s had more than %d custom fields; only the first %d were kept.", r.label, maxCustomKeys, maxCustomKeys)
	}

	mapping := make([]models.ContactImportColumnMapping, 0, len(leadColumns)+len(keys))
	for i, t := range leadColumns {
		mapping = append(mapping, models.ContactImportColumnMapping{Index: i, Target: t})
	}
	for j, k := range keys {
		mapping = append(mapping, models.ContactImportColumnMapping{Index: len(leadColumns) + j, Target: models.ContactImportTargetCustom, CustomKey: k})
	}
	rows := make([]contact.ImportRow, len(g.leads))
	for i, l := range g.leads {
		cells := []string{l.Email, cell(l.FirstName), cell(l.LastName), cell(l.Company), cell(l.Phone)}
		for _, k := range keys {
			cells = append(cells, cell(customs[i][k]))
		}
		rows[i] = contact.ImportRow{Line: i + 1, Cells: cells}
	}

	subscribed := g.subscribed
	opts := &models.ContactImportCommit{
		Mapping: mapping, Dedup: models.ContactImportDedupUpdate, SkipMissingSegments: true,
		SubscribedDefault: &subscribed, Source: models.ContactSourceImport, SourceDetail: truncate(detail, 200),
	}
	if r.categoryID != nil {
		opts.CategoryIDs = []string{r.categoryID.String()}
	}
	if g.enroll && campaignID != nil {
		opts.CampaignIDs = []string{campaignID.String()}
	}
	sink := &collectSink{outcomes: map[int]models.ContactImportRowOutcome{}}
	if _, xerr := r.s.d.Contacts.RunImport(r.ctx, r.actor.String(), r.job.OrgID, rows, opts, sink, nil); xerr != nil {
		return fail("Leads could not be imported: %s", xerr.Message)
	}
	touched := map[string]uuid.UUID{}
	for _, o := range sink.outcomes {
		switch o.Status {
		case models.ContactImportRowImported:
			r.counts.ContactsImported++
		case models.ContactImportRowUpdated:
			r.counts.ContactsUpdated++
		case models.ContactImportRowFailed:
			r.counts.LeadsFailed++
			continue
		}
		if g.enroll && campaignID != nil {
			r.counts.LeadsEnrolled++
		}
		if o.ContactID != nil && o.Email != "" {
			touched[strings.ToLower(o.Email)] = *o.ContactID
		}
	}
	if len(touched) == 0 {
		return nil
	}
	held, err := r.s.d.Repo.PutItems(r.ctx, r.job.ID, r.job.Token, r.job.OrgID, itemContact, touched)
	if err != nil {
		return err
	}
	if !held {
		return errLost
	}
	return nil
}

// leadCustom is a lead's custom fields under the keys Translate writes into templates; the
// provider's own fields win over the built-in extras.
func leadCustom(l source.Lead) map[string]string {
	out := map[string]string{}
	put := func(k, v string) {
		k = source.FieldKey(k)
		if k == "" || !utils.IsValidJSONKey(k) || strings.TrimSpace(v) == "" {
			return
		}
		out[k] = v
	}
	for k, v := range l.LeadCustomKeys() {
		put(k, v)
	}
	for k, v := range l.Custom {
		put(k, v)
	}
	return out
}

// collectSink keeps every row outcome of an import by line.
type collectSink struct {
	outcomes map[int]models.ContactImportRowOutcome
}

func (c *collectSink) Settle(_ context.Context, outcomes []models.ContactImportRowOutcome) error {
	for _, o := range outcomes {
		c.outcomes[o.Line] = o
	}
	return nil
}

func (c *collectSink) Cancelled(context.Context) bool { return false }

// campaignName fits a source name into Warmbly's 3 to 50 bytes.
func campaignName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	name = truncate(name, 50)
	if len(name) < 3 {
		return "Imported campaign"
	}
	return name
}

func cell(s string) string { return truncate(strings.TrimSpace(s), maxCellLen) }

// truncate cuts s to at most n bytes without splitting a character.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimSpace(s[:cut])
}

func lowerAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" && !contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
