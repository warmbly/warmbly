package emailverify

import "strings"

// External verification vocabularies. A customer who verified a list with
// another service brings a status column along; these tables read the words
// each service writes and fold them into Warmbly's four statuses, so nobody
// has to pick a provider or translate a column by hand.

// ExternalVerdict is one recognised external status.
type ExternalVerdict struct {
	Status    Status
	SubStatus SubStatus
}

// vocabularies maps a provider name to the statuses it emits. Keys are
// lower-cased with spaces, dashes and underscores removed (see vocabKey).
var vocabularies = map[string]map[string]ExternalVerdict{
	ProviderCleanMyList: {
		"deliverable":   {Status: StatusValid},
		"undeliverable": {Status: StatusInvalid},
		"risky":         {Status: StatusRisky},
		"unknown":       {Status: StatusUnknown},
	},
	ProviderMailVeri: {
		"valid":       {Status: StatusValid},
		"invalid":     {Status: StatusInvalid},
		"disposable":  {Status: StatusInvalid, SubStatus: SubStatusDisposable},
		"roleaccount": {Status: StatusRisky, SubStatus: SubStatusRole},
		"catchall":    {Status: StatusRisky, SubStatus: SubStatusCatchAll},
		"fullinbox":   {Status: StatusRisky, SubStatus: SubStatusMailboxFull},
		"unknown":     {Status: StatusUnknown},
	},
	ProviderMillionVerifier: {
		"ok":         {Status: StatusValid},
		"good":       {Status: StatusValid},
		"catchall":   {Status: StatusRisky, SubStatus: SubStatusCatchAll},
		"unknown":    {Status: StatusUnknown},
		"error":      {Status: StatusUnknown},
		"disposable": {Status: StatusInvalid, SubStatus: SubStatusDisposable},
		"invalid":    {Status: StatusInvalid},
		"bad":        {Status: StatusInvalid},
		"risky":      {Status: StatusRisky},
	},
	"zerobounce": {
		"valid":       {Status: StatusValid},
		"invalid":     {Status: StatusInvalid},
		"catchall":    {Status: StatusRisky, SubStatus: SubStatusCatchAll},
		"spamtrap":    {Status: StatusInvalid, SubStatus: SubStatusSpamTrap},
		"abuse":       {Status: StatusInvalid, SubStatus: SubStatusSpamTrap},
		"donotmail":   {Status: StatusInvalid},
		"unknown":     {Status: StatusUnknown},
		"disposable":  {Status: StatusInvalid, SubStatus: SubStatusDisposable},
		"toxic":       {Status: StatusInvalid, SubStatus: SubStatusSpamTrap},
		"rolebased":   {Status: StatusRisky, SubStatus: SubStatusRole},
		"mailboxfull": {Status: StatusRisky, SubStatus: SubStatusMailboxFull},
	},
	"neverbounce": {
		"valid":      {Status: StatusValid},
		"invalid":    {Status: StatusInvalid},
		"disposable": {Status: StatusInvalid, SubStatus: SubStatusDisposable},
		"catchall":   {Status: StatusRisky, SubStatus: SubStatusCatchAll},
		"unknown":    {Status: StatusUnknown},
	},
	"bouncer": {
		"deliverable":   {Status: StatusValid},
		"undeliverable": {Status: StatusInvalid},
		"risky":         {Status: StatusRisky},
		"unknown":       {Status: StatusUnknown},
		"acceptall":     {Status: StatusRisky, SubStatus: SubStatusCatchAll},
		"disposable":    {Status: StatusInvalid, SubStatus: SubStatusDisposable},
	},
	"kickbox": {
		"deliverable":   {Status: StatusValid},
		"undeliverable": {Status: StatusInvalid},
		"risky":         {Status: StatusRisky},
		"unknown":       {Status: StatusUnknown},
		"acceptall":     {Status: StatusRisky, SubStatus: SubStatusCatchAll},
	},
	"emailable": {
		"deliverable":   {Status: StatusValid},
		"undeliverable": {Status: StatusInvalid},
		"risky":         {Status: StatusRisky},
		"unknown":       {Status: StatusUnknown},
		"acceptall":     {Status: StatusRisky, SubStatus: SubStatusCatchAll},
	},
	"debounce": {
		"safetosend":    {Status: StatusValid},
		"valid":         {Status: StatusValid},
		"deliverable":   {Status: StatusValid},
		"invalid":       {Status: StatusInvalid},
		"disposable":    {Status: StatusInvalid, SubStatus: SubStatusDisposable},
		"spamtrap":      {Status: StatusInvalid, SubStatus: SubStatusSpamTrap},
		"acceptall":     {Status: StatusRisky, SubStatus: SubStatusCatchAll},
		"catchall":      {Status: StatusRisky, SubStatus: SubStatusCatchAll},
		"role":          {Status: StatusRisky, SubStatus: SubStatusRole},
		"unknown":       {Status: StatusUnknown},
		"risky":         {Status: StatusRisky},
		"undeliverable": {Status: StatusInvalid},
	},
	"clearout": {
		"valid":    {Status: StatusValid},
		"invalid":  {Status: StatusInvalid},
		"catchall": {Status: StatusRisky, SubStatus: SubStatusCatchAll},
		"unknown":  {Status: StatusUnknown},
	},
	"emaillistverify": {
		"ok":                  {Status: StatusValid},
		"valid":               {Status: StatusValid},
		"okforall":            {Status: StatusRisky, SubStatus: SubStatusCatchAll},
		"acceptall":           {Status: StatusRisky, SubStatus: SubStatusCatchAll},
		"catchall":            {Status: StatusRisky, SubStatus: SubStatusCatchAll},
		"invalid":             {Status: StatusInvalid},
		"invalidmx":           {Status: StatusInvalid, SubStatus: SubStatusNoMX},
		"invalidsyntax":       {Status: StatusInvalid, SubStatus: SubStatusSyntax},
		"emaildisabled":       {Status: StatusInvalid},
		"deadserver":          {Status: StatusInvalid, SubStatus: SubStatusNoMX},
		"disposable":          {Status: StatusInvalid, SubStatus: SubStatusDisposable},
		"spamtrap":            {Status: StatusInvalid, SubStatus: SubStatusSpamTrap},
		"unknown":             {Status: StatusUnknown},
		"role":                {Status: StatusRisky, SubStatus: SubStatusRole},
		"smtpprotocol":        {Status: StatusUnknown},
		"antispamsystem":      {Status: StatusUnknown},
		"unknownerror":        {Status: StatusUnknown},
		"attemptrejected":     {Status: StatusUnknown},
		"relaydenied":         {Status: StatusUnknown},
		"mailboxfull":         {Status: StatusRisky, SubStatus: SubStatusMailboxFull},
		"greylisted":          {Status: StatusUnknown},
		"noresponse":          {Status: StatusUnknown},
		"ipblocked":           {Status: StatusUnknown},
		"servicenotavailable": {Status: StatusUnknown},
	},
	// Warmbly's own statuses, so a Warmbly export re-imports losslessly.
	ProviderBuiltin: {
		"valid":   {Status: StatusValid},
		"risky":   {Status: StatusRisky},
		"invalid": {Status: StatusInvalid},
		"unknown": {Status: StatusUnknown},
	},
}

// vocabProviderOrder decides which provider wins when several recognise every
// value. Specific vocabularies first; the generic one last.
var vocabProviderOrder = []string{
	"zerobounce", ProviderMillionVerifier, "neverbounce", "emaillistverify",
	"debounce", "bouncer", "kickbox", "emailable", "clearout", ProviderMailVeri, ProviderCleanMyList, ProviderBuiltin,
}

// vocabProviderAliases maps how a provider is written in a column header to
// its vocabulary key.
var vocabProviderAliases = map[string]string{
	"cleanmylist":     ProviderCleanMyList,
	"mailveri":        ProviderMailVeri,
	"millionverifier": ProviderMillionVerifier, "mv": ProviderMillionVerifier,
	"zerobounce": "zerobounce", "zb": "zerobounce",
	"neverbounce": "neverbounce", "nb": "neverbounce",
	"bouncer": "bouncer", "usebouncer": "bouncer",
	"kickbox":         "kickbox",
	"emailable":       "emailable",
	"debounce":        "debounce",
	"clearout":        "clearout",
	"emaillistverify": "emaillistverify", "elv": "emaillistverify",
	"warmbly": ProviderBuiltin, "builtin": ProviderBuiltin,
}

// providerLabels are the names a member reads for each verifier.
var providerLabels = map[string]string{
	ProviderBuiltin:         "Warmbly's built-in check",
	ProviderMillionVerifier: "MillionVerifier",
	ProviderCleanMyList:     "CleanMyList",
	ProviderMailVeri:        "MailVeri",
	"zerobounce":            "ZeroBounce",
	"neverbounce":           "NeverBounce",
	"bouncer":               "Bouncer",
	"kickbox":               "Kickbox",
	"emailable":             "Emailable",
	"debounce":              "DeBounce",
	"clearout":              "Clearout",
	"emaillistverify":       "EmailListVerify",
}

// ProviderLabel is the display name of a verifier, or "" for one it does not know.
func ProviderLabel(name string) string {
	if l, ok := providerLabels[name]; ok {
		return l
	}
	if k, ok := KnownVocabulary(name); ok {
		return providerLabels[k]
	}
	return ""
}

func vocabKey(raw string) string {
	k := strings.ToLower(strings.TrimSpace(raw))
	return strings.NewReplacer(" ", "", "_", "", "-", "", ".", "").Replace(k)
}

// KnownVocabulary reports whether name (as written by a customer or an API
// caller) is a vocabulary this package can read, and returns its key.
func KnownVocabulary(name string) (string, bool) {
	k, ok := vocabProviderAliases[vocabKey(name)]
	return k, ok
}

// NormalizeExternal reads one external status. provider may be empty, in
// which case every vocabulary is consulted and the first that recognises the
// value wins. Returns false for a value nobody recognises.
func NormalizeExternal(provider, raw string) (ExternalVerdict, bool) {
	key := vocabKey(raw)
	if key == "" {
		return ExternalVerdict{}, false
	}
	if provider != "" {
		p, ok := KnownVocabulary(provider)
		if !ok {
			return ExternalVerdict{}, false
		}
		v, ok := vocabularies[p][key]
		return v, ok
	}
	for _, p := range vocabProviderOrder {
		if v, ok := vocabularies[p][key]; ok {
			return v, ok
		}
	}
	return ExternalVerdict{}, false
}

// DetectVocabulary looks at a column's values and names the provider whose
// vocabulary they all belong to. Blank cells are ignored; a column with no
// non-blank cell, or with one value nobody knows, is not a status column.
func DetectVocabulary(values []string) (string, bool) {
	seen := 0
	for _, p := range vocabProviderOrder {
		all := true
		seen = 0
		for _, raw := range values {
			key := vocabKey(raw)
			if key == "" {
				continue
			}
			seen++
			if _, ok := vocabularies[p][key]; !ok {
				all = false
				break
			}
		}
		if all && seen > 0 {
			return p, true
		}
	}
	return "", false
}

// IsStatusHeader reports whether a column header reads like a verification
// status column, and names the provider if the header says so.
func IsStatusHeader(header string) (provider string, ok bool) {
	k := vocabKey(header)
	if k == "" {
		return "", false
	}
	for alias, p := range vocabProviderAliases {
		if strings.HasPrefix(k, alias) {
			rest := strings.TrimPrefix(k, alias)
			if rest == "" || strings.Contains(rest, "status") || strings.Contains(rest, "result") || strings.Contains(rest, "verif") || strings.Contains(rest, "quality") {
				return p, true
			}
		}
	}
	switch k {
	case "verificationstatus", "verification", "verified", "emailstatus", "emailverification",
		"verifystatus", "verificationresult", "verifyresult", "validationstatus", "validation",
		"emailvalidation", "deliverability", "deliverabilitystatus", "result", "status", "quality":
		return "", true
	}
	return "", false
}
