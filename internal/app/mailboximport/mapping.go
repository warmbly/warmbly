package mailboximport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/pkg/typesafe"
)

// Where a column's meaning came from, strongest first.
const (
	sourceSaved = "saved"
	sourceUser  = "user"
	sourceVndr  = "vendor"
	sourceAlias = "alias"
	sourceShape = "shape"
	sourceJev   = "jev"
	sourceNone  = "none"
)

// jevMinConfidence is the least confidence at which Jev's reading of a column is applied.
const jevMinConfidence = 0.7

var (
	emailRe    = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	hostRe     = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	securityRe = regexp.MustCompile(`^(?i)(ssl|tls|starttls|start_tls|ssl/tls|implicit|none|plain)$`)
)

// headerSignature identifies a header set, so a mapping confirmed once is reused.
func headerSignature(headers []string) string {
	norm := make([]string, len(headers))
	for i, h := range headers {
		norm[i] = normalizeHeader(h)
	}
	sum := sha256.Sum256([]byte(strings.Join(norm, "\x1f")))
	return hex.EncodeToString(sum[:16])
}

// shape describes a column's values in words, without repeating any of them.
func shape(values []string) string {
	var n, emails, hosts, ints, appPw, security, bools int
	for _, v := range values {
		if v == "" {
			continue
		}
		n++
		switch {
		case emailRe.MatchString(v):
			emails++
		case mailhost.LooksLikeGoogleAppPassword(v):
			appPw++
		case securityRe.MatchString(v):
			security++
		case isBool(v):
			bools++
		case isInt(v):
			ints++
		case hostRe.MatchString(v):
			hosts++
		}
	}
	if n == 0 {
		return "empty"
	}
	most := func(k int) bool { return k*5 >= n*4 }
	switch {
	case most(emails):
		return "email addresses"
	case most(appPw):
		return "16 lowercase letters, often in four groups of four"
	case most(security):
		return "encryption names such as SSL, TLS or STARTTLS"
	case most(bools):
		return "yes/no values"
	case most(ints):
		return "whole numbers"
	case most(hosts):
		return "hostnames"
	}
	return "short free text"
}

func isInt(v string) bool {
	_, err := strconv.Atoi(strings.TrimSpace(v))
	return err == nil
}

func isBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "false", "yes", "no", "y", "n", "on", "off", "enabled", "disabled":
		return true
	}
	return false
}

// guessByShape places a column from its values when its header said nothing.
func guessByShape(header string, values []string, taken map[models.MailboxImportField]bool) (models.MailboxImportField, float64) {
	var n, emails, appPw, smtpHosts, imapHosts, smtpPorts, imapPorts, secret int
	h := normalizeHeader(header)
	for _, v := range values {
		if v == "" {
			continue
		}
		n++
		lv := strings.ToLower(v)
		switch {
		case emailRe.MatchString(v):
			emails++
		case mailhost.LooksLikeGoogleAppPassword(v):
			appPw++
		case hostRe.MatchString(v) && (strings.HasPrefix(lv, "smtp") || strings.HasPrefix(lv, "outgoing") || strings.HasPrefix(lv, "smtpout")):
			smtpHosts++
		case hostRe.MatchString(v) && (strings.HasPrefix(lv, "imap") || strings.HasPrefix(lv, "incoming") || lv == "outlook.office365.com"):
			imapHosts++
		case lv == "25" || lv == "465" || lv == "587" || lv == "2525":
			smtpPorts++
		case lv == "993" || lv == "143":
			imapPorts++
		case looksSecret(v):
			secret++
		}
	}
	if n == 0 {
		return "", 0
	}
	most := func(k int) bool { return k*5 >= n*4 }
	pick := func(f models.MailboxImportField, conf float64) (models.MailboxImportField, float64) {
		if taken[f] {
			return "", 0
		}
		return f, conf
	}
	switch {
	case most(emails):
		if !taken[models.ImportFieldEmail] {
			return models.ImportFieldEmail, 0.95
		}
		if strings.Contains(h, "reply") {
			return pick(models.ImportFieldReplyTo, 0.8)
		}
	case most(appPw):
		return pick(models.ImportFieldAppPassword, 0.9)
	case most(smtpHosts):
		return pick(models.ImportFieldSMTPHost, 0.9)
	case most(imapHosts):
		return pick(models.ImportFieldIMAPHost, 0.9)
	case most(smtpPorts):
		return pick(models.ImportFieldSMTPPort, 0.9)
	case most(imapPorts):
		return pick(models.ImportFieldIMAPPort, 0.9)
	case most(secret) && h == "":
		// A headerless second column next to the address is the password.
		return pick(models.ImportFieldPassword, 0.8)
	}
	return "", 0
}

// looksSecret is a token with no spaces that is not an address, a host or a number.
func looksSecret(v string) bool {
	if len(v) < 6 || len(v) > 128 || strings.ContainsAny(v, " \t") || emailRe.MatchString(v) || isInt(v) {
		return false
	}
	return !hostRe.MatchString(v) || strings.ContainsAny(v, "!#$%&*+=?^~")
}

// suggestion is the column list and the mapping a preview answers with.
type suggestion struct {
	headers      []string
	columns      []models.MailboxImportColumn
	mapping      models.MailboxImportMapping
	vendor       *models.MailboxImportVendor
	savedMapping bool
}

// suggest decides what each column means: a mapping the caller sent wins,
// then one saved for this header set, then a vendor or alias match on the
// header, then the shape of the values, then Jev for what is left.
func (s *Service) suggest(ctx context.Context, headers []string, body [][]string, given models.MailboxImportMapping, saved models.MailboxImportMapping) suggestion {
	width := len(headers)
	values := make([][]string, width)
	for _, rec := range body {
		for i := 0; i < width && i < len(rec); i++ {
			if len(values[i]) < 50 {
				values[i] = append(values[i], rec[i])
			}
		}
	}

	normalized := make(map[string]bool, width)
	for _, h := range headers {
		normalized[normalizeHeader(h)] = true
	}
	vendor := detectVendor(normalized)

	cols := make([]models.MailboxImportColumn, width)
	for i := range cols {
		cols[i] = models.MailboxImportColumn{Index: i, Header: headers[i], Field: models.ImportFieldIgnore, Source: sourceNone}
	}

	switch {
	case len(given) > 0:
		applyMapping(cols, given, sourceUser)
	case len(saved) > 0:
		applyMapping(cols, saved, sourceSaved)
	default:
		taken := map[models.MailboxImportField]bool{}
		for i, h := range headers {
			if f, ok := aliases[normalizeHeader(h)]; ok && !taken[f] {
				src := sourceAlias
				if vendor != nil {
					src = sourceVndr
				}
				cols[i].Field, cols[i].Source, cols[i].Confidence = f, src, 1
				taken[f] = true
			}
		}
		for i := range cols {
			if cols[i].Source != sourceNone {
				continue
			}
			if f, conf := guessByShape(headers[i], values[i], taken); f != "" {
				cols[i].Field, cols[i].Source, cols[i].Confidence = f, sourceShape, conf
				taken[f] = true
			}
		}
		s.askJev(ctx, cols, values, taken)
	}

	mapping := make(models.MailboxImportMapping, width)
	for i := range cols {
		f := cols[i].Field
		cols[i].Secret = secretFields[f] || (f == models.ImportFieldIgnore && most(values[i], looksSecret))
		cols[i].Samples = samples(values[i], cols[i].Secret)
		mapping[strconv.Itoa(i)] = f
	}
	return suggestion{headers: headers, columns: cols, mapping: mapping, vendor: vendor, savedMapping: len(given) == 0 && len(saved) > 0}
}

func most(values []string, pred func(string) bool) bool {
	var n, k int
	for _, v := range values {
		if v == "" {
			continue
		}
		n++
		if pred(v) {
			k++
		}
	}
	return n > 0 && k*5 >= n*4
}

func applyMapping(cols []models.MailboxImportColumn, m models.MailboxImportMapping, source string) {
	taken := map[models.MailboxImportField]bool{}
	for key, f := range m {
		i, err := strconv.Atoi(key)
		if err != nil || i < 0 || i >= len(cols) || !validField(f) {
			continue
		}
		if f != models.ImportFieldIgnore {
			if taken[f] {
				continue
			}
			taken[f] = true
		}
		cols[i].Field, cols[i].Source, cols[i].Confidence = f, source, 1
	}
}

// samples are up to three values to show next to a column; a secret column shows none of its own.
func samples(values []string, secret bool) []string {
	out := make([]string, 0, 3)
	for _, v := range values {
		if v == "" {
			continue
		}
		if secret {
			out = append(out, strings.Repeat("•", 8))
		} else {
			if len([]rune(v)) > 60 {
				v = string([]rune(v)[:60]) + "…"
			}
			out = append(out, v)
		}
		if len(out) == 3 {
			break
		}
	}
	return out
}

// askJev offers the columns no rule placed to Jev as multiple choice. It sees
// the header and a description of the values, never a value.
func (s *Service) askJev(ctx context.Context, cols []models.MailboxImportColumn, values [][]string, taken map[models.MailboxImportField]bool) {
	if s.asker == nil {
		return
	}
	criteria := make(map[string]string, len(fieldDescriptions))
	for f, d := range fieldDescriptions {
		criteria[string(f)] = d
	}
	questions := map[string]typesafe.Question{}
	state := map[string]string{}
	for i := range cols {
		if cols[i].Source != sourceNone || len(values[i]) == 0 {
			continue
		}
		key := fmt.Sprintf("column_%d", i)
		desc := shape(values[i])
		if desc == "empty" {
			continue
		}
		state[key] = fmt.Sprintf("Header: %q. Values: %s.", cols[i].Header, desc)
		questions[key] = typesafe.Choice(
			fmt.Sprintf("A spreadsheet of email accounts to connect to a sending tool has a column described in state.%s. What does that column hold?", key),
			criteria)
	}
	if len(questions) == 0 {
		return
	}
	askCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	resp, err := s.asker.Ask(typesafe.WithUsage(askCtx, "mailbox_import_mapping", ""), state, questions)
	if err != nil || resp == nil {
		return
	}
	// Highest confidence first, so two columns wanting one field resolve in favour of the surer one.
	keys := make([]string, 0, len(resp.Answers))
	for k := range resp.Answers {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool { return resp.Answers[keys[a]].Confidence > resp.Answers[keys[b]].Confidence })
	for _, k := range keys {
		ans := resp.Answers[k]
		var i int
		if _, err := fmt.Sscanf(k, "column_%d", &i); err != nil || i < 0 || i >= len(cols) {
			continue
		}
		f := models.MailboxImportField(ans.Choice)
		if !validField(f) || ans.Confidence < jevMinConfidence || (f != models.ImportFieldIgnore && taken[f]) {
			continue
		}
		cols[i].Field, cols[i].Source, cols[i].Confidence = f, sourceJev, ans.Confidence
		if f != models.ImportFieldIgnore {
			taken[f] = true
		}
	}
}
