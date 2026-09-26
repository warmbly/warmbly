// Package mailhost works out where a domain's mail is hosted and which IMAP and
// SMTP servers to use for it, so a mailbox import needs only an address and a
// password.
//
// Control-plane only: it performs outbound DNS and HTTPS lookups against
// user-supplied domains and belongs in the backend, never in the worker.
package mailhost

import (
	"strings"

	"golang.org/x/net/publicsuffix"
)

// Host names who serves a domain's mail. The values are stored as mail_host.
type Host string

const (
	Unknown         Host = ""
	GoogleWorkspace Host = "google_workspace"
	Gmail           Host = "gmail"
	Microsoft365    Host = "microsoft365"
	Outlook         Host = "outlook"
	Zoho            Host = "zoho"
	Yahoo           Host = "yahoo"
	AOL             Host = "aol"
	ICloud          Host = "icloud"
	Fastmail        Host = "fastmail"
	GoDaddy         Host = "godaddy"
	Namecheap       Host = "namecheap"
	IONOS           Host = "ionos"
	Hostinger       Host = "hostinger"
	OVH             Host = "ovh"
	Migadu          Host = "migadu"
	Purelymail      Host = "purelymail"
	Rackspace       Host = "rackspace"
	Yandex          Host = "yandex"
	GMX             Host = "gmx"
	Proton          Host = "proton"
	Other           Host = "other"
)

var labels = map[Host]string{
	GoogleWorkspace: "Google Workspace",
	Gmail:           "Gmail",
	Microsoft365:    "Microsoft 365",
	Outlook:         "Outlook.com",
	Zoho:            "Zoho Mail",
	Yahoo:           "Yahoo Mail",
	AOL:             "AOL Mail",
	ICloud:          "iCloud Mail",
	Fastmail:        "Fastmail",
	GoDaddy:         "GoDaddy",
	Namecheap:       "Namecheap Private Email",
	IONOS:           "IONOS",
	Hostinger:       "Hostinger",
	OVH:             "OVHcloud",
	Migadu:          "Migadu",
	Purelymail:      "Purelymail",
	Rackspace:       "Rackspace Email",
	Yandex:          "Yandex Mail",
	GMX:             "GMX",
	Proton:          "Proton Mail",
	Other:           "Other provider",
}

// Hosts lists every known non-empty value in a stable order.
func Hosts() []Host {
	return []Host{
		GoogleWorkspace, Gmail, Microsoft365, Outlook, Zoho, Yahoo, AOL, ICloud,
		Fastmail, GoDaddy, Namecheap, IONOS, Hostinger, OVH, Migadu, Purelymail,
		Rackspace, Yandex, GMX, Proton, Other,
	}
}

// Label is the provider's display name, "" for Unknown.
func (h Host) Label() string { return labels[h] }

// Valid reports whether s is a known mail_host value or "".
func Valid(s string) bool {
	if s == "" {
		return true
	}
	_, ok := labels[Host(s)]
	return ok
}

// Google reports whether h is either Google product.
func (h Host) Google() bool { return h == GoogleWorkspace || h == Gmail }

// Microsoft reports whether h is either Microsoft product.
func (h Host) Microsoft() bool { return h == Microsoft365 || h == Outlook }

// ForMailbox is who hosts a connected mailbox: its stored mail_host when the
// connect path recorded one, else read from how it connects and its address.
// provider is the email_provider value (gmail, outlook, smtp_imap).
func ForMailbox(stored, provider, address string) Host {
	domain := ""
	if at := strings.LastIndex(address, "@"); at >= 0 {
		domain = NormalizeDomain(address[at+1:])
	}
	if h := Host(stored); stored != "" && Valid(stored) {
		return Refine(h, domain)
	}
	switch provider {
	case "gmail":
		return Refine(Gmail, domain)
	case "outlook":
		return Refine(Microsoft365, domain)
	}
	if h, ok := knownDomain(domain); ok {
		return h
	}
	return Other
}

// PasswordAuth says how a password signs in on a host.
type PasswordAuth string

const (
	AppPassword PasswordAuth = "app_password"
	Password    PasswordAuth = "password"
	OAuthOnly   PasswordAuth = "oauth_only"
	Unsupported PasswordAuth = "unsupported"
)

// Security modes, matching models.MailSecurity*.
const (
	SecurityTLS      = "tls"
	SecurityStartTLS = "starttls"
)

// Endpoint is one server to dial.
type Endpoint struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"`
}

// Settings are the servers a mailbox on a host signs in to.
type Settings struct {
	SMTP Endpoint `json:"smtp"`
	IMAP Endpoint `json:"imap"`
}

// Detection sources.
const (
	SourceKnown      = "known"
	SourceMX         = "mx"
	SourceAutoconfig = "autoconfig"
	SourceISPDB      = "ispdb"
	SourceSRV        = "srv"
	SourceNone       = "none"
)

// Detection is what is known about one domain's mail.
type Detection struct {
	Domain         string       `json:"domain"`
	Host           Host         `json:"mail_host"`
	Source         string       `json:"source"`
	Settings       *Settings    `json:"settings,omitempty"`
	PasswordAuth   PasswordAuth `json:"password_auth"`
	AppPasswordURL string       `json:"app_password_url,omitempty"`
	MX             []string     `json:"mx,omitempty"`
}

// variant carries what differs between deployments of one provider.
type variant struct {
	// zone is Zoho's data-centre domain ("zoho.eu"), IONOS's tld ("co.uk") or GMX's tld ("net").
	zone string
	// personal marks a free consumer account (Zoho's personal servers differ).
	personal bool
}

func ep(host string, port int, sec string) Endpoint {
	return Endpoint{Host: host, Port: port, Security: sec}
}

func pair(smtpHost string, smtpPort int, smtpSec, imapHost string) *Settings {
	return &Settings{SMTP: ep(smtpHost, smtpPort, smtpSec), IMAP: ep(imapHost, 993, SecurityTLS)}
}

// settingsFor returns a host's conventional servers, nil when there are none.
func settingsFor(h Host, v variant) *Settings {
	switch h {
	case GoogleWorkspace, Gmail:
		return pair("smtp.gmail.com", 587, SecurityStartTLS, "imap.gmail.com")
	case Microsoft365:
		return pair("smtp.office365.com", 587, SecurityStartTLS, "outlook.office365.com")
	case Outlook:
		return pair("smtp-mail.outlook.com", 587, SecurityStartTLS, "outlook.office365.com")
	case Zoho:
		zone := v.zone
		if zone == "" {
			zone = "zoho.com"
		}
		if v.personal {
			return pair("smtp."+zone, 465, SecurityTLS, "imap."+zone)
		}
		return pair("smtppro."+zone, 465, SecurityTLS, "imappro."+zone)
	case Yahoo:
		return pair("smtp.mail.yahoo.com", 465, SecurityTLS, "imap.mail.yahoo.com")
	case AOL:
		return pair("smtp.aol.com", 465, SecurityTLS, "imap.aol.com")
	case ICloud:
		return pair("smtp.mail.me.com", 587, SecurityStartTLS, "imap.mail.me.com")
	case Fastmail:
		return pair("smtp.fastmail.com", 465, SecurityTLS, "imap.fastmail.com")
	case GoDaddy:
		return pair("smtpout.secureserver.net", 465, SecurityTLS, "imap.secureserver.net")
	case Namecheap:
		return pair("mail.privateemail.com", 465, SecurityTLS, "mail.privateemail.com")
	case IONOS:
		tld := v.zone
		if tld == "" {
			tld = "com"
		}
		return pair("smtp.ionos."+tld, 587, SecurityStartTLS, "imap.ionos."+tld)
	case Hostinger:
		return pair("smtp.hostinger.com", 465, SecurityTLS, "imap.hostinger.com")
	case OVH:
		return pair("ssl0.ovh.net", 465, SecurityTLS, "ssl0.ovh.net")
	case Migadu:
		return pair("smtp.migadu.com", 465, SecurityTLS, "imap.migadu.com")
	case Purelymail:
		return pair("smtp.purelymail.com", 465, SecurityTLS, "imap.purelymail.com")
	case Rackspace:
		return pair("secure.emailsrvr.com", 465, SecurityTLS, "secure.emailsrvr.com")
	case Yandex:
		return pair("smtp.yandex.com", 465, SecurityTLS, "imap.yandex.com")
	case GMX:
		tld := v.zone
		if tld == "" {
			tld = "com"
		}
		return pair("mail.gmx."+tld, 587, SecurityStartTLS, "imap.gmx."+tld)
	}
	return nil
}

// SettingsFor returns a host's default servers (US data centre where it
// varies), nil for Proton, Other and Unknown.
func SettingsFor(h Host) *Settings { return settingsFor(h, variant{}) }

// PasswordAuthFor says how a password signs in on h.
func PasswordAuthFor(h Host) PasswordAuth {
	switch h {
	case GoogleWorkspace, Gmail, Yahoo, AOL, ICloud, Fastmail, Yandex:
		return AppPassword
	case Microsoft365, Outlook:
		return OAuthOnly
	case Proton:
		return Unsupported
	}
	return Password
}

// AppPasswordURL is where an app password for h is created, "" when h has none.
func AppPasswordURL(h Host) string {
	switch h {
	case GoogleWorkspace, Gmail:
		return "https://myaccount.google.com/apppasswords"
	case Yahoo:
		return "https://login.yahoo.com/account/security"
	case AOL:
		return "https://login.aol.com/account/security"
	case ICloud:
		return "https://account.apple.com/account/manage"
	case Fastmail:
		return "https://app.fastmail.com/settings/security/apps"
	case Yandex:
		return "https://id.yandex.com/security/app-passwords"
	case Zoho:
		return "https://accounts.zoho.com/home#security/app_password"
	}
	return ""
}

func zohoAppPasswordURL(zone string) string {
	switch zone {
	case "", "zoho.com":
		return AppPasswordURL(Zoho)
	case "zohocloud.ca":
		return "https://accounts.zohocloud.ca/home#security/app_password"
	}
	return "https://accounts." + zone + "/home#security/app_password"
}

// detection fills in a known host's settings and password rules.
func detection(domain string, h Host, v variant, source string, mx []string) Detection {
	d := Detection{
		Domain:         domain,
		Host:           h,
		Source:         source,
		Settings:       settingsFor(h, v),
		PasswordAuth:   PasswordAuthFor(h),
		AppPasswordURL: AppPasswordURL(h),
		MX:             mx,
	}
	if h == Zoho {
		d.AppPasswordURL = zohoAppPasswordURL(v.zone)
	}
	return d
}

// normHost lower-cases a hostname and drops a trailing dot.
func normHost(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}

// under reports whether host is d or a subdomain of it.
func under(host, d string) bool {
	return host == d || strings.HasSuffix(host, "."+d)
}

// registrable splits host into its registrable domain's first label and the
// public suffix after it: "smtp.ionos.co.uk" -> "ionos", "co.uk".
func registrable(host string) (label, suffix string) {
	etld1, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return "", ""
	}
	i := strings.IndexByte(etld1, '.')
	if i <= 0 {
		return "", ""
	}
	return etld1[:i], etld1[i+1:]
}

var zohoZones = []string{
	"zoho.com", "zoho.eu", "zoho.in", "zoho.com.au", "zoho.jp", "zohocloud.ca", "zoho.sa", "zoho.uk",
}

var ionosTLDs = map[string]bool{
	"com": true, "de": true, "co.uk": true, "es": true, "fr": true, "it": true, "ca": true, "mx": true,
}

// classify maps a mail server or MX hostname to its provider. Google hosts
// come back as Gmail and Microsoft ones as Microsoft365; Refine picks the
// product from the address's domain.
func classify(host string) (Host, variant) {
	h := normHost(host)
	if h == "" {
		return Unknown, variant{}
	}
	switch {
	case under(h, "google.com"), under(h, "gmail.com"), under(h, "googlemail.com"):
		return Gmail, variant{}
	case h == "outlook-com.olc.protection.outlook.com",
		h == "smtp-mail.outlook.com", h == "imap-mail.outlook.com":
		return Outlook, variant{}
	case under(h, "outlook.com"), under(h, "office365.com"), under(h, "office.com"),
		under(h, "mx.microsoft"), under(h, "hotmail.com"):
		return Microsoft365, variant{}
	case under(h, "yahoodns.net"), under(h, "yahoo.com"):
		return Yahoo, variant{}
	case under(h, "aol.com"):
		return AOL, variant{}
	case under(h, "icloud.com"), under(h, "me.com"), under(h, "mac.com"):
		return ICloud, variant{}
	case under(h, "messagingengine.com"), under(h, "fastmail.com"), under(h, "fastmail.fm"):
		return Fastmail, variant{}
	case under(h, "secureserver.net"):
		return GoDaddy, variant{}
	case under(h, "privateemail.com"):
		return Namecheap, variant{}
	case under(h, "perfora.net"):
		return IONOS, variant{zone: "com"}
	case under(h, "kundenserver.de"):
		return IONOS, variant{zone: "de"}
	case under(h, "hostinger.com"):
		return Hostinger, variant{}
	case under(h, "ovh.net"):
		return OVH, variant{}
	case under(h, "migadu.com"):
		return Migadu, variant{}
	case under(h, "purelymail.com"):
		return Purelymail, variant{}
	case under(h, "emailsrvr.com"):
		return Rackspace, variant{}
	case under(h, "protonmail.ch"), under(h, "proton.me"), under(h, "protonmail.com"):
		return Proton, variant{}
	}
	for _, z := range zohoZones {
		if under(h, z) {
			return Zoho, variant{zone: z}
		}
	}
	switch label, suffix := registrable(h); label {
	case "ionos", "1and1":
		if !ionosTLDs[suffix] {
			suffix = "com"
		}
		return IONOS, variant{zone: suffix}
	case "yandex":
		return Yandex, variant{}
	case "gmx":
		return GMX, variant{zone: gmxZone(suffix)}
	}
	return Unknown, variant{}
}

// gmxZone picks GMX's German servers for its German-speaking domains.
func gmxZone(suffix string) string {
	switch suffix {
	case "net", "de", "at", "ch":
		return "net"
	}
	return "com"
}

// FromServer classifies an SMTP or IMAP server hostname. Google servers
// return Gmail and Microsoft's return Microsoft365; pass the result through
// Refine with the address's domain to pick the product.
func FromServer(host string) Host {
	h, _ := classify(host)
	return h
}

// Refine picks the product within a provider family from the address's domain.
func Refine(h Host, domain string) Host {
	domain = normHost(domain)
	if domain == "" {
		return h
	}
	switch h {
	case Gmail, GoogleWorkspace:
		if domain == "gmail.com" || domain == "googlemail.com" {
			return Gmail
		}
		return GoogleWorkspace
	case Microsoft365:
		if k, ok := knownDomain(domain); ok && k == Outlook {
			return Outlook
		}
	}
	return h
}

// knownDomain recognises the free consumer mail domains.
// SharedProvider reports whether a domain belongs to a consumer mail service
// (gmail.com, outlook.com, ...), decided from the built-in list with no DNS.
func SharedProvider(domain string) bool {
	_, ok := knownDomain(NormalizeDomain(domain))
	return ok
}

func knownDomain(domain string) (Host, bool) {
	h, _, ok := known(domain)
	return h, ok
}

func known(domain string) (Host, variant, bool) {
	switch domain {
	case "gmail.com", "googlemail.com":
		return Gmail, variant{}, true
	case "msn.com", "passport.com", "windowslive.com":
		return Outlook, variant{}, true
	case "ymail.com", "rocketmail.com":
		return Yahoo, variant{}, true
	case "aol.com", "aim.com":
		return AOL, variant{}, true
	case "icloud.com", "me.com", "mac.com":
		return ICloud, variant{}, true
	case "zoho.com", "zohomail.com":
		return Zoho, variant{zone: "zoho.com", personal: true}, true
	case "zoho.eu", "zohomail.eu":
		return Zoho, variant{zone: "zoho.eu", personal: true}, true
	case "zohomail.in":
		return Zoho, variant{zone: "zoho.in", personal: true}, true
	case "ya.ru":
		return Yandex, variant{}, true
	case "proton.me", "protonmail.com", "protonmail.ch", "pm.me":
		return Proton, variant{}, true
	case "fastmail.com", "fastmail.fm":
		return Fastmail, variant{}, true
	case "yahoo.co.jp":
		// Yahoo Japan is a separate company with its own servers.
		return Unknown, variant{}, false
	}
	// The consumer brands register one domain per country (hotmail.co.uk, gmx.de).
	etld1, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil || etld1 != domain {
		return Unknown, variant{}, false
	}
	label, suffix := registrable(domain)
	switch label {
	case "hotmail", "live", "outlook":
		return Outlook, variant{}, true
	case "yahoo":
		return Yahoo, variant{}, true
	case "gmx":
		return GMX, variant{zone: gmxZone(suffix)}, true
	case "yandex":
		return Yandex, variant{}, true
	}
	return Unknown, variant{}, false
}

// LooksLikeGoogleAppPassword reports whether s has the shape Google prints an
// app password in: 16 ASCII letters, optionally in groups split by spaces.
func LooksLikeGoogleAppPassword(s string) bool {
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	if len(s) != 16 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

// NormalizeAppPassword strips the spaces Google prints inside an app password;
// every other host's password is returned untouched.
func NormalizeAppPassword(h Host, s string) string {
	if h.Google() && LooksLikeGoogleAppPassword(s) {
		return strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	}
	return s
}

// AuthMethodFor is the auth_method a password connect on h is stored with.
func AuthMethodFor(h Host, pa PasswordAuth) string {
	if pa == "" {
		pa = PasswordAuthFor(h)
	}
	if pa == AppPassword {
		return "app_password"
	}
	return "password"
}
