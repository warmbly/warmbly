package admindevice

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

const TTL = 5 * time.Minute
const Interval = 5

var (
	ErrExpired  = errx.NewWithIdentifier(errx.NotFound, "admin_device_expired", "Device request expired or already consumed. Start again.")
	ErrSlow     = errx.NewWithIdentifier(errx.TooManyRequests, "admin_device_slow_down", "Wait five seconds before polling again.")
	ErrResolved = errx.NewWithIdentifier(errx.Conflict, "admin_device_resolved", "Device request has already been decided.")
	ErrRate     = errx.New(errx.TooManyRequests, "Device request attempt budget exceeded. Retry after the rate-limit window.")
)

type Service struct {
	cache    *cache.Cache
	issuer   token.AdminDeviceIssuer
	prefix   string
	instance string
}

func New(c *cache.Cache, tokens token.TokenService, instance, instanceSecret string) *Service {
	issuer, _ := tokens.(token.AdminDeviceIssuer)
	instance = strings.TrimRight(strings.TrimSpace(instance), "/")
	u, err := url.Parse(instance)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") || instanceSecret == "" {
		issuer = nil
	}
	return &Service{cache: c, issuer: issuer, instance: instance, prefix: "admin-device:" + crypt.SHA256(instance+instanceSecret) + ":"}
}

type Request struct {
	ClientName string `json:"client_name"`
}

type Start struct {
	DeviceSecret     string `json:"device_secret"`
	UserCode         string `json:"user_code"`
	InstanceURL      string `json:"instance_url"`
	VerificationPath string `json:"verification_path"`
	ExpiresIn        int    `json:"expires_in"`
	Interval         int    `json:"interval"`
}

type Record struct {
	ClientName  string    `json:"client_name"`
	InstanceURL string    `json:"instance_url"`
	ExpiresAt   time.Time `json:"expires_at"`
	Status      string    `json:"status"`
	SourceID    uuid.UUID `json:"source_id"`
	UserID      uuid.UUID `json:"user_id"`
	ProofAt     time.Time `json:"proof_at"`
	LastPoll    int64     `json:"last_poll"`
}

type Description struct {
	ClientName   string    `json:"client_name"`
	InstanceURL  string    `json:"instance_url"`
	ExpiresAt    time.Time `json:"expires_at"`
	Status       string    `json:"status"`
	ConsentToken string    `json:"consent_token"`
}

type Poll struct {
	Status string        `json:"status"`
	Token  *models.Token `json:"token,omitempty"`
	UserID uuid.UUID     `json:"user_id,omitempty"`
}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func normalizeCode(raw string) string {
	code := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(raw), "-", ""))
	if len(code) != 8 {
		return ""
	}
	for _, c := range code {
		if !strings.ContainsRune("23456789ABCDEFGHJKLMNPQRSTUVWXYZ", c) {
			return ""
		}
	}
	return code
}

func (s *Service) ready() bool { return s != nil && s.cache != nil && s.issuer != nil }

func (s *Service) LimitPublic(ctx context.Context, ip string, poll bool) *errx.Error {
	if !s.ready() {
		return errx.ErrServiceDown
	}
	limit, kind := int64(20), "start:"
	if poll {
		limit, kind = 360, "poll:"
	}
	ok, err := s.cache.ReserveAttempt(ctx, s.prefix+kind+crypt.SHA256(ip), limit, 10*time.Minute)
	if err != nil {
		return errx.ErrServiceDown
	}
	if !ok {
		return ErrRate
	}
	return nil
}

func (s *Service) Start(ctx context.Context, req Request) (*Start, *errx.Error) {
	if !s.ready() {
		return nil, errx.ErrServiceDown
	}
	name, xerr := displayname.Validate("client_name", req.ClientName, displayname.Workspace, false)
	if xerr != nil {
		return nil, xerr
	}
	req.ClientName = name
	secret, err := randomSecret()
	if err != nil {
		return nil, errx.InternalError()
	}
	key := s.prefix + "grant:" + crypt.SHA256(secret)
	record := Record{ClientName: req.ClientName, InstanceURL: s.instance, Status: "pending", ExpiresAt: time.Now().UTC().Add(TTL)}
	for range 3 {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			return nil, errx.InternalError()
		}
		const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
		for i := range b {
			b[i] = alphabet[int(b[i])%len(alphabet)]
		}
		code := string(b)
		ok, err := s.cache.SetNX(ctx, s.prefix+"code:"+code, key, TTL).Result()
		if err != nil {
			return nil, errx.ErrServiceDown
		}
		if !ok {
			continue
		}
		if err := s.cache.SetJSON(ctx, key, record, TTL); err != nil {
			return nil, errx.ErrServiceDown
		}
		return &Start{DeviceSecret: secret, UserCode: code[:4] + "-" + code[4:], InstanceURL: s.instance, VerificationPath: "/device", ExpiresIn: int(TTL.Seconds()), Interval: Interval}, nil
	}
	return nil, errx.InternalError()
}

func (s *Service) resolve(ctx context.Context, code string, session *models.Session) (string, *Record, *errx.Error) {
	if !s.ready() {
		return "", nil, errx.ErrServiceDown
	}
	if session == nil {
		return "", nil, errx.ErrUnauthorized
	}
	ok, err := s.cache.ReserveAttempt(ctx, s.prefix+"lookup:"+session.UserID.String(), 12, time.Minute)
	if err != nil {
		return "", nil, errx.ErrServiceDown
	}
	if !ok {
		return "", nil, ErrRate
	}
	code = normalizeCode(code)
	if code == "" {
		return "", nil, ErrExpired
	}
	key, err := s.cache.Get(ctx, s.prefix+"code:"+code).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil, ErrExpired
	}
	if err != nil {
		return "", nil, errx.ErrServiceDown
	}
	var r Record
	if err := s.cache.GetJSON(ctx, key, &r); err != nil {
		return "", nil, ErrExpired
	}
	return key, &r, nil
}

func (s *Service) Describe(ctx context.Context, code string, session *models.Session) (*Description, *errx.Error) {
	key, r, xerr := s.resolve(ctx, code, session)
	if xerr != nil {
		return nil, xerr
	}
	if r.Status != "pending" {
		return nil, ErrResolved
	}
	consent, err := randomSecret()
	if err != nil {
		return nil, errx.InternalError()
	}
	value := key + "|" + session.ID.String()
	if err := s.cache.Set(ctx, s.prefix+"consent:"+crypt.SHA256(consent), value, time.Until(r.ExpiresAt)).Err(); err != nil {
		return nil, errx.ErrServiceDown
	}
	return &Description{ClientName: r.ClientName, InstanceURL: r.InstanceURL, ExpiresAt: r.ExpiresAt, Status: r.Status, ConsentToken: consent}, nil
}

var decide = redis.NewScript(`
local consent = redis.call('GET', KEYS[2])
if consent ~= ARGV[1] then return 0 end
local raw = redis.call('GET', KEYS[1])
if not raw then return 0 end
local r = cjson.decode(raw)
if r.status ~= 'pending' then return 0 end
redis.call('DEL', KEYS[2])
r.status = ARGV[2]; r.source_id = ARGV[3]; r.user_id = ARGV[4]; r.proof_at = ARGV[5]
redis.call('SET', KEYS[1], cjson.encode(r), 'KEEPTTL')
return 1`)

func (s *Service) Decide(ctx context.Context, code, consent, decision string, session *models.Session) *errx.Error {
	if session == nil || !session.MFAVerified || session.ReauthAt == nil || time.Since(*session.ReauthAt) > token.ReauthWindow || session.ReauthAt.After(time.Now()) {
		return errx.ErrUnauthorized
	}
	if len(consent) != 43 || (decision != "approved" && decision != "denied") {
		return errx.New(errx.BadRequest, "explicit approval or denial and consent_token are required")
	}
	key, _, xerr := s.resolve(ctx, code, session)
	if xerr != nil {
		return xerr
	}
	n, err := decide.Run(ctx, s.cache.Client, []string{key, s.prefix + "consent:" + crypt.SHA256(consent)}, key+"|"+session.ID.String(), decision, session.ID.String(), session.UserID.String(), session.ReauthAt.UTC().Format(time.RFC3339Nano)).Int()
	if err != nil {
		return errx.ErrServiceDown
	}
	if n != 1 {
		return ErrResolved
	}
	return nil
}

var claim = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then return 'expired' end
local r = cjson.decode(raw)
if r.status == 'consumed' then return 'expired' end
if tonumber(ARGV[1]) - r.last_poll < 5 then return 'slow' end
r.last_poll = tonumber(ARGV[1])
local result = cjson.encode(r)
if r.status == 'approved' then r.status = 'consumed' end
redis.call('SET', KEYS[1], cjson.encode(r), 'KEEPTTL')
return result`)

func (s *Service) Poll(ctx context.Context, secret string) (*Poll, *errx.Error) {
	if !s.ready() {
		return nil, errx.ErrServiceDown
	}
	if len(secret) != 43 {
		return nil, ErrExpired
	}
	raw, err := claim.Run(ctx, s.cache.Client, []string{s.prefix + "grant:" + crypt.SHA256(secret)}, time.Now().Unix()).Text()
	if err != nil {
		return nil, errx.ErrServiceDown
	}
	if raw == "expired" {
		return nil, ErrExpired
	}
	if raw == "slow" {
		return nil, ErrSlow
	}
	var r Record
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil, errx.InternalError()
	}
	res := &Poll{Status: r.Status}
	if r.Status == "approved" {
		var xerr *errx.Error
		res.Token, xerr = s.issuer.GenerateAdminDeviceSession(ctx, r.SourceID, r.UserID, r.ProofAt)
		if xerr != nil {
			return nil, xerr
		}
		res.UserID = r.UserID
	}
	return res, nil
}
