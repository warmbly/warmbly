package signuprisk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Assessment struct {
	IP              string
	UserAgent       string
	EmailRisk       int
	ASN             *int64
	NormalizedEmail string
	Score           int
	Signals         []string
}

type ASNResolver interface {
	LookupASN(ctx context.Context, ip net.IP) (*int64, error)
}

type Scorer struct {
	cache    redis.Cmdable
	resolver ASNResolver
}

func New(cache redis.Cmdable, resolver ASNResolver) *Scorer {
	if resolver == nil {
		resolver = DNSASNResolver{}
	}
	return &Scorer{cache: cache, resolver: resolver}
}

var disposableDomains = map[string]struct{}{
	"10minutemail.com": {}, "dispostable.com": {}, "emailondeck.com": {},
	"fakeinbox.com": {}, "getnada.com": {}, "guerrillamail.com": {},
	"maildrop.cc": {}, "mailinator.com": {}, "mintemail.com": {},
	"mohmal.com": {}, "sharklasers.com": {}, "temp-mail.org": {},
	"tempail.com": {}, "tempmail.com": {}, "throwawaymail.com": {},
	"trashmail.com": {}, "yopmail.com": {},
}

var knownHostingASNs = map[int64]struct{}{
	14061: {}, 14618: {}, 16276: {}, 16509: {}, 20473: {}, 24940: {},
	31898: {}, 36351: {}, 396982: {}, 45102: {}, 63949: {}, 8075: {},
}

func (s *Scorer) Assess(ctx context.Context, address, ipAddress, userAgent string) Assessment {
	assessment := Assessment{
		IP:              strings.TrimSpace(ipAddress),
		UserAgent:       strings.TrimSpace(userAgent),
		NormalizedEmail: NormalizeEmail(address),
	}

	domain := emailDomain(address)
	if IsDisposableDomain(domain) {
		assessment.EmailRisk = 30
		assessment.Score += 30
		assessment.Signals = append(assessment.Signals, "disposable_email")
	} else if looksDisposable(domain) {
		assessment.EmailRisk = 15
		assessment.Score += 15
		assessment.Signals = append(assessment.Signals, "disposable_email_heuristic")
	}

	ip := net.ParseIP(assessment.IP)
	if ip != nil && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsUnspecified() && s.resolver != nil {
		lookupCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
		asn, err := s.resolver.LookupASN(lookupCtx, ip)
		cancel()
		if err == nil && asn != nil {
			assessment.ASN = asn
			if _, risky := knownHostingASNs[*asn]; risky {
				assessment.Score += 20
				assessment.Signals = append(assessment.Signals, "datacenter_asn")
			}
		}
	}

	if s.cache != nil && ip != nil {
		if count := s.increment(ctx, "signup:risk:ip:"+digest(assessment.IP), time.Hour); count >= 5 {
			assessment.Score += 15
			assessment.Signals = append(assessment.Signals, "signup_ip_velocity")
		}
		if assessment.ASN != nil {
			if count := s.increment(ctx, "signup:risk:asn:"+strconv.FormatInt(*assessment.ASN, 10), time.Hour); count >= 20 {
				assessment.Score += 10
				assessment.Signals = append(assessment.Signals, "signup_asn_velocity")
			}
		}
	}
	if s.cache != nil && assessment.NormalizedEmail != "" {
		key := "signup:risk:email-aliases:" + digest(assessment.NormalizedEmail)
		member := digest(strings.ToLower(strings.TrimSpace(address)))
		if added, err := s.cache.SAdd(ctx, key, member).Result(); err == nil {
			if added > 0 {
				_ = s.cache.Expire(ctx, key, 24*time.Hour).Err()
			}
			if count, err := s.cache.SCard(ctx, key).Result(); err == nil && count >= 2 {
				assessment.Score += 20
				assessment.Signals = append(assessment.Signals, "normalized_email_reuse")
			}
		}
	}

	assessment.Score = min(assessment.Score, 100)
	return assessment
}

func (s *Scorer) increment(ctx context.Context, key string, ttl time.Duration) int64 {
	count, err := s.cache.Incr(ctx, key).Result()
	if err != nil {
		return 0
	}
	if count == 1 {
		_ = s.cache.Expire(ctx, key, ttl).Err()
	}
	return count
}

func NormalizeEmail(address string) string {
	parsed, err := mail.ParseAddress(strings.TrimSpace(address))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(address))
	}
	parts := strings.SplitN(strings.ToLower(parsed.Address), "@", 2)
	if len(parts) != 2 {
		return strings.ToLower(parsed.Address)
	}
	local, domain := parts[0], parts[1]
	if domain == "googlemail.com" {
		domain = "gmail.com"
	}
	if domain == "gmail.com" {
		local = strings.ReplaceAll(strings.SplitN(local, "+", 2)[0], ".", "")
	}
	return local + "@" + domain
}

func IsDisposableDomain(domain string) bool {
	_, ok := disposableDomains[strings.ToLower(strings.TrimSpace(domain))]
	return ok
}

type AddressQuality struct {
	Disposable bool
	Role       bool
	RiskyTLD   bool
}

var roleLocalParts = map[string]struct{}{
	"abuse": {}, "admin": {}, "billing": {}, "contact": {}, "help": {},
	"info": {}, "office": {}, "sales": {}, "security": {}, "support": {},
}

var riskyTLDs = map[string]struct{}{
	"click": {}, "country": {}, "download": {}, "gq": {}, "loan": {},
	"men": {}, "ml": {}, "party": {}, "review": {}, "science": {}, "stream": {},
	"tk": {}, "top": {}, "trade": {}, "work": {}, "xyz": {},
}

func ClassifyAddress(address string) AddressQuality {
	parsed, err := mail.ParseAddress(strings.TrimSpace(address))
	if err != nil {
		return AddressQuality{}
	}
	parts := strings.SplitN(strings.ToLower(parsed.Address), "@", 2)
	if len(parts) != 2 {
		return AddressQuality{}
	}
	local, domain := strings.SplitN(parts[0], "+", 2)[0], parts[1]
	_, role := roleLocalParts[local]
	tldParts := strings.Split(domain, ".")
	_, riskyTLD := riskyTLDs[tldParts[len(tldParts)-1]]
	return AddressQuality{
		Disposable: IsDisposableDomain(domain) || looksDisposable(domain),
		Role:       role,
		RiskyTLD:   riskyTLD,
	}
}

func emailDomain(address string) string {
	parsed, err := mail.ParseAddress(strings.TrimSpace(address))
	if err != nil {
		return ""
	}
	parts := strings.SplitN(parsed.Address, "@", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(parts[1])
}

func looksDisposable(domain string) bool {
	return strings.Contains(domain, "tempmail") || strings.Contains(domain, "throwaway") || strings.Contains(domain, "10minute")
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:12])
}

type DNSASNResolver struct{}

func (DNSASNResolver) LookupASN(ctx context.Context, ip net.IP) (*int64, error) {
	v4 := ip.To4()
	if v4 == nil {
		return nil, nil
	}
	query := fmt.Sprintf("%d.%d.%d.%d.origin.asn.cymru.com", v4[3], v4[2], v4[1], v4[0])
	records, err := net.DefaultResolver.LookupTXT(ctx, query)
	if err != nil || len(records) == 0 {
		return nil, err
	}
	fields := strings.Split(records[0], "|")
	if len(fields) == 0 {
		return nil, nil
	}
	asn, err := strconv.ParseInt(strings.TrimSpace(fields[0]), 10, 64)
	if err != nil {
		return nil, err
	}
	return &asn, nil
}
