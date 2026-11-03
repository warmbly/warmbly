package mailboximport

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
)

// payload is what a row carries into the queue, sealed with the workspace's key.
type payload struct {
	Name string `json:"name"`
	// NameGiven is true when the file named the sender, so an update may rename.
	NameGiven  bool            `json:"name_given,omitempty"`
	SMTP       *models.Service `json:"smtp,omitempty"`
	IMAP       *models.Service `json:"imap,omitempty"`
	MailHost   string          `json:"mail_host"`
	AuthMethod string          `json:"auth_method"`
	// Signin marks a row that connects through Google or Microsoft sign-in.
	Signin bool `json:"signin,omitempty"`
	// GrantID connects the row through an administrator's grant over its domain.
	GrantID *uuid.UUID `json:"grant_id,omitempty"`
	// Vendor rows fetch their credentials from the vendor when they are worked.
	VendorConnectionID *uuid.UUID                   `json:"vendor_connection_id,omitempty"`
	VendorMailboxID    string                       `json:"vendor_mailbox_id,omitempty"`
	VendorProvider     string                       `json:"vendor_provider,omitempty"`
	Settings           models.MailboxImportSettings `json:"settings"`
	TagNames           []string                     `json:"tag_names,omitempty"`
	// TrackingDomain and RedirectURL are the domain-level choices made in the review step.
	TrackingDomain string `json:"tracking_domain,omitempty"`
	RedirectURL    string `json:"redirect_url,omitempty"`
	// email and importID are the row the payload belongs to, set when it is worked.
	email    string
	importID uuid.UUID
}

// builtRow is one data row after mapping, detection and validation.
type builtRow struct {
	line    int
	email   string
	domain  string
	status  string // a models.ImportPreview* value
	cause   string
	problem string
	payload payload
	fields  map[string]string
}

// buildContext is what every row is judged against.
type buildContext struct {
	headers        []string
	mapping        models.MailboxImportMapping
	detections     map[string]mailhost.Detection
	existing       map[string]models.EmailRef
	sharedPassword string
	// googleSignin is true when this instance connects new Gmail mailboxes with Google sign-in.
	googleSignin bool
	// grants maps "provider\x00domain" to the administrator's grant covering it.
	grants map[string]uuid.UUID
}

func grantKey(provider, domain string) string { return provider + "\x00" + domain }

// columnFor inverts the mapping: field -> column index.
func columnFor(m models.MailboxImportMapping) map[models.MailboxImportField]int {
	out := map[models.MailboxImportField]int{}
	for key, f := range m {
		if f == models.ImportFieldIgnore {
			continue
		}
		if i, err := strconv.Atoi(key); err == nil {
			out[f] = i
		}
	}
	return out
}

func domainOf(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return ""
	}
	return mailhost.NormalizeDomain(email[at+1:])
}

// emailsAndDomains reads the email column only, so detection and the
// existing-mailbox lookup can run before the rows are built.
func emailsAndDomains(body [][]string, m models.MailboxImportMapping) ([]string, []string) {
	col, ok := columnFor(m)[models.ImportFieldEmail]
	if !ok {
		return nil, nil
	}
	var emails, domains []string
	seen := map[string]bool{}
	for _, rec := range body {
		if col >= len(rec) {
			continue
		}
		e := strings.TrimSpace(rec[col])
		if !emailRe.MatchString(e) {
			continue
		}
		emails = append(emails, e)
		if d := domainOf(e); d != "" && !seen[d] {
			seen[d] = true
			domains = append(domains, d)
		}
	}
	return emails, domains
}

// buildRows judges every data row. firstLine is the file line of body[0].
func buildRows(bc buildContext, body [][]string, firstLine int) []builtRow {
	cols := columnFor(bc.mapping)
	seen := map[string]bool{}
	out := make([]builtRow, 0, len(body))
	for i, rec := range body {
		get := func(f models.MailboxImportField) string {
			if c, ok := cols[f]; ok && c < len(rec) {
				return strings.TrimSpace(rec[c])
			}
			return ""
		}
		r := buildRow(bc, get, firstLine+i)
		r.fields = rowFields(bc, rec)
		if r.email != "" && r.status != models.ImportPreviewInvalid {
			key := strings.ToLower(r.email)
			if seen[key] {
				r.status, r.cause, r.problem = models.ImportPreviewDuplicate, causeDuplicateRow, "Listed earlier in the file."
			} else {
				seen[key] = true
				if _, ok := bc.existing[key]; ok && r.status != models.ImportPreviewNeedsSignin {
					r.status = models.ImportPreviewExisting
				}
			}
		}
		out = append(out, r)
	}
	return out
}

// rowFields keeps the uploaded cells that are safe to show again, by header.
func rowFields(bc buildContext, rec []string) map[string]string {
	fields := make(map[string]string, len(bc.headers))
	for i, h := range bc.headers {
		if i >= len(rec) {
			break
		}
		f := bc.mapping[strconv.Itoa(i)]
		if secretFields[f] || (f == models.ImportFieldIgnore && looksSecret(rec[i])) {
			continue
		}
		fields[h] = rec[i]
	}
	return fields
}

func buildRow(bc buildContext, get func(models.MailboxImportField) string, line int) builtRow {
	r := builtRow{line: line}
	invalid := func(cause, problem string) builtRow {
		r.status, r.cause, r.problem = models.ImportPreviewInvalid, cause, problem
		return r
	}

	r.email = get(models.ImportFieldEmail)
	if r.email == "" {
		return invalid(causeMissingEmail, "No email address.")
	}
	if !emailRe.MatchString(r.email) {
		return invalid(causeInvalidEmail, fmt.Sprintf("%q is not an email address.", r.email))
	}
	r.domain = domainOf(r.email)

	name := get(models.ImportFieldName)
	if name == "" {
		name = strings.TrimSpace(get(models.ImportFieldFirstName) + " " + get(models.ImportFieldLastName))
	}
	r.payload.NameGiven = name != ""
	if name == "" {
		name = nameFromEmail(r.email)
	}
	r.payload.Name = name

	settings, tags, problem := rowSettings(get)
	r.payload.Settings, r.payload.TagNames = settings, tags
	if problem != "" {
		return invalid(causeInvalidValue, problem)
	}

	// Servers: the file's own, else what the domain's DNS says.
	det := bc.detections[r.domain]
	smtpHost, imapHost := get(models.ImportFieldSMTPHost), get(models.ImportFieldIMAPHost)
	host := det.Host
	var settingsFound *mailhost.Settings
	if smtpHost != "" || imapHost != "" {
		host = mailhost.Refine(mailhost.FromServer(firstNonEmpty(smtpHost, imapHost)), r.domain)
		if host == mailhost.Unknown {
			host = mailhost.Other
		}
		base := det.Settings
		if base == nil {
			base = mailhost.SettingsFor(host)
		}
		s := mailhost.Settings{}
		if base != nil {
			s = *base
		}
		if smtpHost != "" {
			s.SMTP = mailhost.Endpoint{Host: smtpHost}
		}
		if imapHost != "" {
			s.IMAP = mailhost.Endpoint{Host: imapHost}
		}
		settingsFound = &s
	} else if det.Settings != nil {
		s := *det.Settings
		settingsFound = &s
	}
	r.payload.MailHost = string(host)

	pa := mailhost.PasswordAuthFor(host)
	if det.PasswordAuth != "" && smtpHost == "" && imapHost == "" {
		pa = det.PasswordAuth
	}
	switch pa {
	case mailhost.Unsupported:
		if host == mailhost.Proton {
			return invalid(causeProviderUnsupport, "Proton Mail does not offer IMAP sign-in to other apps.")
		}
	case mailhost.OAuthOnly:
		if id, ok := bc.grants[grantKey(models.GrantProviderMicrosoft, r.domain)]; ok {
			return delegated(r, id)
		}
		r.status, r.cause = models.ImportPreviewNeedsSignin, causeMicrosoftSignin
		r.payload.Signin, r.payload.AuthMethod = true, models.MailAuthOAuth
		return r
	}
	// A Workspace domain an administrator granted connects through the grant: no app password to expire.
	if host.Google() {
		if id, ok := bc.grants[grantKey(models.GrantProviderGoogle, r.domain)]; ok {
			return delegated(r, id)
		}
	}

	// Passwords, per leg: a leg's own column, then an app password where the
	// host wants one, then the account password, then the shared one.
	appPw := mailhost.NormalizeAppPassword(host, get(models.ImportFieldAppPassword))
	accountPw := get(models.ImportFieldPassword)
	pick := func(own string) string {
		if own != "" {
			return own
		}
		if appPw != "" && (pa == mailhost.AppPassword || accountPw == "") {
			return appPw
		}
		if accountPw != "" {
			return mailhost.NormalizeAppPassword(host, accountPw)
		}
		return mailhost.NormalizeAppPassword(host, bc.sharedPassword)
	}
	smtpPw, imapPw := pick(get(models.ImportFieldSMTPPassword)), pick(get(models.ImportFieldIMAPPassword))
	username := get(models.ImportFieldUsername)
	if username == "" {
		username = r.email
	}
	// Whatever the file gave is kept even on a row that cannot go yet, so a
	// fix only has to supply the missing piece.
	r.payload.SMTP = &models.Service{Username: firstNonEmpty(get(models.ImportFieldSMTPUsername), username), Password: smtpPw}
	r.payload.IMAP = &models.Service{Username: firstNonEmpty(get(models.ImportFieldIMAPUsername), username), Password: imapPw}
	r.payload.AuthMethod = mailhost.AuthMethodFor(host, pa)

	if settingsFound == nil || settingsFound.SMTP.Host == "" || settingsFound.IMAP.Host == "" {
		return invalid(causeUnknownServers, "No IMAP and SMTP servers found for "+r.domain+".")
	}
	smtp, err := endpoint(settingsFound.SMTP, get(models.ImportFieldSMTPPort), get(models.ImportFieldSMTPSecurity), "smtp")
	if err != "" {
		return invalid(causeInvalidValue, err)
	}
	imap, err := endpoint(settingsFound.IMAP, get(models.ImportFieldIMAPPort), get(models.ImportFieldIMAPSecurity), "imap")
	if err != "" {
		return invalid(causeInvalidValue, err)
	}
	r.payload.SMTP.Host, r.payload.SMTP.Port, r.payload.SMTP.Security = smtp.Host, smtp.Port, smtp.Security
	r.payload.IMAP.Host, r.payload.IMAP.Port, r.payload.IMAP.Security = imap.Host, imap.Port, imap.Security

	if host.Google() && !mailhost.LooksLikeGoogleAppPassword(smtpPw) {
		if bc.googleSignin {
			// A sign-in row keeps no password: Google would never accept it.
			r.status, r.cause = models.ImportPreviewNeedsSignin, causeGoogleSignin
			r.payload.Signin, r.payload.AuthMethod = true, models.MailAuthOAuth
			r.payload.SMTP, r.payload.IMAP = nil, nil
			return r
		}
		if smtpPw != "" {
			return invalid("google_app_password_required", "Google needs a 16-letter app password here, not the account password.")
		}
	}
	if smtpPw == "" || imapPw == "" {
		return invalid(causeMissingPassword, "No password.")
	}
	r.status = models.ImportPreviewReady
	return r
}

// delegated marks a row that connects through an administrator's grant.
func delegated(r builtRow, grantID uuid.UUID) builtRow {
	id := grantID
	r.payload.GrantID, r.payload.AuthMethod = &id, models.MailAuthDelegated
	r.payload.SMTP, r.payload.IMAP = nil, nil
	r.status = models.ImportPreviewReady
	return r
}

// endpoint fills a leg's port and security from the file, defaulting from the detected settings.
func endpoint(base mailhost.Endpoint, portRaw, securityRaw, leg string) (mailhost.Endpoint, string) {
	e := base
	if portRaw != "" {
		p, err := strconv.Atoi(portRaw)
		if err != nil || p < 1 || p > 65535 {
			return e, fmt.Sprintf("%s port %q is not a port number.", strings.ToUpper(leg), portRaw)
		}
		e.Port = p
		if securityRaw == "" && p != base.Port {
			e.Security = ""
		}
	}
	if e.Port == 0 {
		e.Port = map[string]int{"smtp": 587, "imap": 993}[leg]
	}
	if securityRaw != "" {
		s, ok := parseSecurity(securityRaw, e.Port)
		if !ok {
			return e, fmt.Sprintf("%s security %q should be SSL/TLS or STARTTLS.", strings.ToUpper(leg), securityRaw)
		}
		e.Security = s
	}
	if e.Security == "" {
		if leg == "smtp" {
			e.Security = models.ResolveSMTPSecurity("", e.Port)
		} else {
			e.Security = models.ResolveIMAPSecurity("", e.Port)
		}
	}
	return e, ""
}

// parseSecurity reads the ways files spell encryption, including Smartlead's
// SSL/TLS port type and Instantly's true/false SSL column.
func parseSecurity(raw string, port int) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "tls", "ssl", "ssl/tls", "implicit", "implicit tls":
		return models.MailSecurityTLS, true
	case "starttls", "start_tls", "start-tls", "start tls":
		return models.MailSecurityStartTLS, true
	case "true", "yes", "1", "on":
		if port == 587 || port == 143 || port == 25 {
			return models.MailSecurityStartTLS, true
		}
		return models.MailSecurityTLS, true
	case "false", "no", "0", "off":
		// "No SSL" on a submission port is STARTTLS in practice; plaintext is never offered.
		return models.MailSecurityStartTLS, true
	case "none", "plain":
		return models.MailSecurityNone, true
	}
	return "", false
}

// rowSettings reads the per-row settings columns. A value that does not parse
// is a problem with the row rather than something to guess at.
func rowSettings(get func(models.MailboxImportField) string) (models.MailboxImportSettings, []string, string) {
	var s models.MailboxImportSettings
	intField := func(f models.MailboxImportField, label string, dst **int) string {
		raw := get(f)
		if raw == "" {
			return ""
		}
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(raw), "%"))
		if err != nil || n < 0 {
			return fmt.Sprintf("%s %q is not a whole number.", label, raw)
		}
		*dst = &n
		return ""
	}
	for _, p := range []string{
		intField(models.ImportFieldDailyLimit, "Daily limit", &s.DailyLimit),
		intField(models.ImportFieldMinWait, "Minimum wait", &s.MinWait),
		intField(models.ImportFieldWarmupStart, "Warmup start", &s.WarmupStart),
		intField(models.ImportFieldWarmupMax, "Warmup maximum", &s.WarmupMax),
		intField(models.ImportFieldWarmupIncrease, "Warmup increase", &s.WarmupIncrease),
		intField(models.ImportFieldWarmupReplyRate, "Warmup reply rate", &s.WarmupReplyRate),
	} {
		if p != "" {
			return s, nil, p
		}
	}
	if raw := get(models.ImportFieldWarmup); raw != "" {
		switch strings.ToLower(raw) {
		case "true", "yes", "y", "1", "on", "enabled":
			t := true
			s.Warmup = &t
		case "false", "no", "n", "0", "off", "disabled":
			f := false
			s.Warmup = &f
		default:
			return s, nil, fmt.Sprintf("Warmup %q should be yes or no.", raw)
		}
	}
	if v := get(models.ImportFieldReplyTo); v != "" {
		if !emailRe.MatchString(v) {
			return s, nil, fmt.Sprintf("Reply-to %q is not an email address.", v)
		}
		s.ReplyTo = &v
	}
	if v := get(models.ImportFieldSignature); v != "" {
		s.Signature = &v
	}
	if v := get(models.ImportFieldTimezone); v != "" {
		s.Timezone = &v
	}
	var tags []string
	if v := get(models.ImportFieldTags); v != "" {
		for _, t := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ';' || r == '|' }) {
			if t = strings.TrimSpace(t); t != "" && len(tags) < 20 {
				tags = append(tags, t)
			}
		}
	}
	return s, tags, ""
}

// storedRowSettings is what a queued row keeps: a batch warmup Off is written
// into the row, since the previous release's runner lets a row's value win.
func storedRowSettings(batch, row models.MailboxImportSettings) models.MailboxImportSettings {
	if batchWarmupOff(batch) {
		row.Warmup = batch.Warmup
		row.WarmupStart, row.WarmupMax, row.WarmupIncrease, row.WarmupReplyRate = nil, nil, nil, nil
	}
	return row
}

func batchWarmupOff(batch models.MailboxImportSettings) bool {
	return batch.Warmup != nil && !*batch.Warmup
}

// mergeSettings lays a row's own settings over the import's batch settings,
// except that warmup switched off for the batch ignores every row warmup value.
func mergeSettings(batch, row models.MailboxImportSettings) models.MailboxImportSettings {
	out := batch
	if batchWarmupOff(batch) {
		row.Warmup, row.WarmupStart, row.WarmupMax, row.WarmupIncrease, row.WarmupReplyRate = nil, nil, nil, nil, nil
	}
	if row.DailyLimit != nil {
		out.DailyLimit = row.DailyLimit
	}
	if row.MinWait != nil {
		out.MinWait = row.MinWait
	}
	if row.Warmup != nil {
		out.Warmup = row.Warmup
	}
	if row.WarmupStart != nil {
		out.WarmupStart = row.WarmupStart
	}
	if row.WarmupMax != nil {
		out.WarmupMax = row.WarmupMax
	}
	if row.WarmupIncrease != nil {
		out.WarmupIncrease = row.WarmupIncrease
	}
	if row.WarmupReplyRate != nil {
		out.WarmupReplyRate = row.WarmupReplyRate
	}
	if row.ReplyTo != nil {
		out.ReplyTo = row.ReplyTo
	}
	if row.Signature != nil {
		out.Signature = row.Signature
	}
	if row.Timezone != nil {
		out.Timezone = row.Timezone
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// nameFromEmail turns "alex.rivera@" into "Alex Rivera".
func nameFromEmail(email string) string {
	local := email
	if at := strings.Index(email, "@"); at >= 0 {
		local = email[:at]
	}
	parts := strings.FieldsFunc(local, func(r rune) bool { return r == '.' || r == '_' || r == '-' || r == '+' })
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}
