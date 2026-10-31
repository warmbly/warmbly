package sendingdomain

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/safehttp"
)

// TrackingServiceHeader marks the tracking service's own answers, telling a visit that reached it from one a proxy answered.
const TrackingServiceHeader = "X-Warmbly-Service"

// TrackingHostHeader is the host the tracking service looked a 404 up under.
const TrackingHostHeader = "X-Warmbly-Host"

// ReachProber opens a verified redirect's domain the way a visitor does.
type ReachProber interface {
	Probe(ctx context.Context, domain, target string) models.RedirectReach
}

type httpReach struct{ transport http.RoundTripper }

// NewHTTPReach probes over the SSRF-hardened transport, on the two ports a browser uses, holding no idle sockets to customer hosts.
func NewHTTPReach() ReachProber {
	t := safehttp.NewTransport("80", "443")
	t.DisableKeepAlives = true
	return &httpReach{transport: t}
}

// hit is what one scheme answered; an empty hint on a nil error is the redirect working.
type hit struct {
	url      string
	hint     models.RedirectReachHint
	status   int
	location string
	blocked  bool
	// refused: the address answered that nothing listens there, which a failed hairpin never does.
	refused bool
	// transient: the tracking service, or a proxy in front of it (a 502 to 504), could not answer just now.
	transient bool
	// proxy is the web server that answered, when it says or shows which.
	proxy string
}

func (h hit) ok() bool { return h.hint == models.RedirectHintNone && !h.blocked && !h.transient }

func (p *httpReach) Probe(ctx context.Context, domain, target string) models.RedirectReach {
	var wg sync.WaitGroup
	var plain, secure hit
	wg.Add(2)
	go func() { defer wg.Done(); plain = p.fetch(ctx, "http://"+domain+"/", domain, target) }()
	go func() { defer wg.Done(); secure = p.fetch(ctx, "https://"+domain+"/", domain, target) }()
	wg.Wait()
	return combineReach(domain, target, plain, secure)
}

func (p *httpReach) fetch(ctx context.Context, raw, domain, target string) hit {
	out := hit{url: raw}
	client := &http.Client{
		Transport: p.transport,
		Timeout:   8 * time.Second,
		// Same-host hops are a proxy upgrading http to https; the first hop off the domain is the answer.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 || !sameSite(req.URL.Hostname(), domain) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		out.hint = models.RedirectHintNoListener
		return out
	}
	req.Header.Set("User-Agent", "Warmbly-RedirectCheck/1.0")
	res, err := client.Do(req)
	if err != nil {
		switch {
		case errors.Is(err, safehttp.ErrBlockedAddress):
			out.blocked = true
		case certificateError(err):
			out.hint, out.proxy = models.RedirectHintCertificate, proxyOfCertificate(err)
		default:
			out.hint, out.refused = models.RedirectHintNoListener, errors.Is(err, syscall.ECONNREFUSED)
		}
		return out
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	out.status, out.proxy = res.StatusCode, proxyOf(res, body)
	tracking := res.Header.Get(TrackingServiceHeader) == "tracking"
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		if loc, err := res.Location(); err == nil && !sameSite(loc.Hostname(), domain) {
			out.location = loc.String()
			// Warmbly's own redirect reaches the visitor; a changed target reaches the tracking cache within minutes.
			if !tracking && !sameTarget(out.location, target) {
				out.hint = models.RedirectHintWrongTarget
			}
			return out
		}
	}
	switch {
	case res.StatusCode >= 500 && (tracking || res.StatusCode == http.StatusBadGateway || res.StatusCode == http.StatusServiceUnavailable || res.StatusCode == http.StatusGatewayTimeout):
		// A gateway error means the proxy tried to pass the visit on, which is routing that works.
		out.transient = true
	case !tracking:
		out.hint = models.RedirectHintNotRouted
	case sameSite(res.Header.Get(TrackingHostHeader), domain):
		// The visit arrived under the right name; the lookup behind it has not caught up yet.
		out.hint = models.RedirectHintSettling
	default:
		out.hint = models.RedirectHintHostHeader
	}
	return out
}

func certificateError(err error) bool {
	var verify *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var alert tls.AlertError
	return errors.As(err, &verify) || errors.As(err, &unknown) || errors.As(err, &hostname) || errors.As(err, &invalid) || errors.As(err, &alert)
}

// proxyOf names the web server from its Server header, or from Traefik's default 404, which carries none.
func proxyOf(res *http.Response, body []byte) string {
	server := strings.ToLower(res.Header.Get("Server"))
	for _, name := range []string{"traefik", "nginx", "caddy", "apache", "cloudflare", "litespeed"} {
		if strings.Contains(server, name) {
			return name
		}
	}
	if strings.Contains(server, "microsoft-iis") {
		return "iis"
	}
	if server == "" && res.StatusCode == http.StatusNotFound && string(body) == "404 page not found\n" {
		return "traefik"
	}
	return ""
}

// proxyOfCertificate recognises the placeholder certificate Traefik serves for a name it has no route for.
func proxyOfCertificate(err error) string {
	var leaf *x509.Certificate
	var hostname x509.HostnameError
	var authority x509.UnknownAuthorityError
	var verify *tls.CertificateVerificationError
	switch {
	case errors.As(err, &verify) && len(verify.UnverifiedCertificates) > 0:
		leaf = verify.UnverifiedCertificates[0]
	case errors.As(err, &hostname):
		leaf = hostname.Certificate
	case errors.As(err, &authority):
		leaf = authority.Cert
	}
	if leaf != nil && leaf.Subject.CommonName == "TRAEFIK DEFAULT CERT" {
		return "traefik"
	}
	return ""
}

var proxyNames = map[string]string{
	"traefik": "Traefik", "nginx": "nginx", "caddy": "Caddy", "apache": "Apache",
	"cloudflare": "Cloudflare", "litespeed": "LiteSpeed", "iis": "IIS",
}

func sameSite(host, domain string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == domain || host == "www."+domain
}

// sameTarget compares two addresses the way a visitor would notice a difference.
func sameTarget(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return false
	}
	norm := func(u *url.URL) string {
		return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + strings.TrimSuffix(u.EscapedPath(), "/") + "?" + u.RawQuery
	}
	return norm(ua) == norm(ub)
}

// combineReach turns the two answers into one verdict, naming the scheme that failed.
func combineReach(domain, target string, plain, secure hit) models.RedirectReach {
	proxy := plain.proxy
	if proxy == "" {
		proxy = secure.proxy
	}
	reach := combineHits(domain, target, plain, secure)
	if reach.Status != models.RedirectReachOK {
		reach.Proxy = proxy
	}
	return reach
}

func combineHits(domain, target string, plain, secure hit) models.RedirectReach {
	if plain.blocked || secure.blocked {
		return models.RedirectReach{Status: models.RedirectReachUnreachable, Hint: models.RedirectHintNoListener,
			Detail: fmt.Sprintf("%s resolves to a private address, so this server does not open it to check.", domain)}
	}
	if plain.ok() && secure.ok() {
		return models.RedirectReach{Status: models.RedirectReachOK}
	}
	// A proxy answering in Warmbly's place is the most useful thing to report, whichever scheme showed it.
	for _, h := range []hit{plain, secure} {
		switch h.hint {
		case models.RedirectHintNotRouted:
			by := ""
			if name := proxyNames[firstNonEmpty(h.proxy, plain.proxy, secure.proxy)]; name != "" {
				by = " from " + name
			}
			return models.RedirectReach{Status: models.RedirectReachNotReaching, Hint: h.hint,
				Detail: fmt.Sprintf("Opening %s answered %d%s without sending the visit to Warmbly.", h.url, h.status, by)}
		case models.RedirectHintHostHeader:
			return models.RedirectReach{Status: models.RedirectReachNotReaching, Hint: h.hint,
				Detail: fmt.Sprintf("Warmbly's tracking service received the visit to %s under a different hostname, so it did not recognise the domain.", domain)}
		case models.RedirectHintWrongTarget:
			return models.RedirectReach{Status: models.RedirectReachNotReaching, Hint: h.hint,
				Detail: fmt.Sprintf("%s redirects to %s, not to %s.", domain, h.location, target)}
		}
	}
	switch {
	case plain.transient || secure.transient:
		return models.RedirectReach{Status: models.RedirectReachUnreachable,
			Detail: fmt.Sprintf("Opening %s got an answer, but the service behind it could not respond just now, which is usually Warmbly restarting. It is checked again later.", domain)}
	case plain.hint == models.RedirectHintSettling || secure.hint == models.RedirectHintSettling:
		return models.RedirectReach{Status: models.RedirectReachUnreachable, Hint: models.RedirectHintSettling,
			Detail: fmt.Sprintf("Warmbly's tracking service received the visit to %s but has not picked up the redirect yet. It is checked again in a few minutes.", domain)}
	case secure.hint == models.RedirectHintCertificate && (plain.ok() || plain.hint == models.RedirectHintCertificate):
		// Also when http only upgrades to https: the visitor still meets the certificate.
		return models.RedirectReach{Status: models.RedirectReachHTTPSError, Hint: models.RedirectHintCertificate,
			Detail: fmt.Sprintf("https://%s has no valid certificate, so browsers show a security warning instead of the redirect.", domain)}
	case plain.ok():
		return models.RedirectReach{Status: models.RedirectReachHTTPSError, Hint: models.RedirectHintNoListener,
			Detail: fmt.Sprintf("https://%s does not answer, so browsers that try https first get nothing.", domain)}
	case secure.ok():
		return models.RedirectReach{Status: models.RedirectReachNotReaching, Hint: models.RedirectHintNoListener,
			Detail: fmt.Sprintf("http://%s does not answer, so visitors who type the domain without https get nothing.", domain)}
	case secure.hint == models.RedirectHintCertificate:
		return models.RedirectReach{Status: models.RedirectReachNotReaching, Hint: models.RedirectHintNoListener,
			Detail: fmt.Sprintf("http://%s does not answer, and https://%s has no valid certificate.", domain, domain)}
	case plain.refused || secure.refused:
		return models.RedirectReach{Status: models.RedirectReachNotReaching, Hint: models.RedirectHintNoListener,
			Detail: fmt.Sprintf("Nothing listens on ports 80 and 443 at %s's address, so visitors get no answer.", domain)}
	}
	return models.RedirectReach{Status: models.RedirectReachUnreachable, Hint: models.RedirectHintNoListener,
		Detail: fmt.Sprintf("Warmbly could not open %s from this server to confirm it.", domain)}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
