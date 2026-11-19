package store

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
	"github.com/warmbly/warmbly/internal/repository"
)

// ConnectionSettings is where the admin panel's GraphKit connection is kept, secrets sealed under the instance key.
type ConnectionSettings interface {
	CanSeal() bool
	GetConnection(ctx context.Context) (*repository.StoreConnection, error)
	PutConnection(ctx context.Context, c repository.StoreConnection) error
	DeleteConnection(ctx context.Context) error
	CountLivePurchases(ctx context.Context, orgID *uuid.UUID) (mailboxes, domains int, err error)
}

// Codes the connection settings answer with.
const (
	CodeConfiguredByEnv = "store_configured_by_environment"
	CodeSealingMissing  = "store_sealing_unavailable"
	CodeGraphKitURL     = "graphkit_url_invalid"
	CodeGraphKitKey     = "graphkit_key_required"
	// CodeGraphKitKeyReentry asks for the key again with a changed API URL; a stored key is only sent where it was saved.
	CodeGraphKitKeyReentry = "graphkit_key_reentry_required"
	CodeGraphKitRejected   = "graphkit_key_rejected"
	CodeGraphKitNoAnswer   = "graphkit_unreachable"
)

const (
	connectionPollEvery   = 30 * time.Second
	connectionTestTimeout = 15 * time.Second
	// webhookPreviousSecretTTL is how long a replaced webhook secret saved here keeps verifying; GraphKit's overlap is 24 hours.
	webhookPreviousSecretTTL = 24 * time.Hour
)

// WebhookPath is where GraphKit delivers events, under the API's public origin.
const WebhookPath = "/v1/webhooks/graphkit"

// ConnectionView is the connection as the operator sees it; secrets only as set or not.
type ConnectionView struct {
	Source           string     `json:"source"`
	Configured       bool       `json:"configured"`
	Mode             string     `json:"mode"`
	APIURL           string     `json:"api_url"`
	DefaultAPIURL    string     `json:"default_api_url"`
	APIKeySet        bool       `json:"api_key_set"`
	WebhookSecretSet bool       `json:"webhook_secret_set"`
	WebhookURL       string     `json:"webhook_url"`
	SealingAvailable bool       `json:"sealing_available"`
	UpdatedAt        *time.Time `json:"updated_at"`
	UpdatedBy        *uuid.UUID `json:"updated_by"`
	// PurchasesWithoutConnection is set while the store is off and purchases made here are still live.
	PurchasesWithoutConnection *Disconnected `json:"purchases_without_connection"`
}

// ConnectionInput is what the operator typed; an empty secret keeps the stored one.
type ConnectionInput struct {
	APIURL        string `json:"api_url"`
	APIKey        string `json:"api_key"`
	WebhookSecret string `json:"webhook_secret"`
}

// ConnectionTest is what a working key reports.
type ConnectionTest struct {
	Mode              string `json:"mode"`
	BalanceMicros     int64  `json:"balance_micros"`
	Currency          string `json:"currency"`
	ComRegisterMicros *int64 `json:"com_register_micros"`
	ComRenewMicros    *int64 `json:"com_renew_micros"`
}

// Disconnected counts purchases GraphKit no longer reaches: their renewals, billing and Google connects are paused.
type Disconnected struct {
	Mailboxes int `json:"mailboxes"`
	Domains   int `json:"domains"`
}

var (
	errConfiguredByEnv = errx.NewWithIdentifier(errx.Conflict, CodeConfiguredByEnv,
		"GraphKit is configured by the GRAPHKIT_* environment variables, which take precedence. Change it there.")
	errSealingMissing = errx.NewWithIdentifier(errx.Conflict, CodeSealingMissing,
		"Set CREDENTIALS_ENCRYPTION_KEY before saving a GraphKit API key here; the key is stored sealed under it.")
	errURLInvalid  = errx.NewWithIdentifier(errx.BadRequest, CodeGraphKitURL, "The GraphKit API URL must be an https address.")
	errKeyRequired = errx.NewWithIdentifier(errx.BadRequest, CodeGraphKitKey, "Enter a GraphKit API key.")
	errKeyReentry  = errx.NewWithIdentifier(errx.BadRequest, CodeGraphKitKeyReentry,
		"Enter the GraphKit API key again to use a different API URL; the stored key is only sent to the URL it was saved with.")
	errKeyRejected = errx.NewWithIdentifier(errx.BadRequest, CodeGraphKitRejected, "GraphKit did not accept this API key.")
	errNoAnswer    = errx.NewWithIdentifier(errx.ServiceUnavailable, CodeGraphKitNoAnswer,
		"GraphKit could not be reached at this API URL. Check the URL and try again.")
)

// WireConnectionSettings attaches the admin panel's stored connection; env is GRAPHKIT_* as the process read it.
func (s *Service) WireConnectionSettings(repo ConnectionSettings, env config.GraphKit) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.settings, s.envCfg = repo, env
	s.mu.Unlock()
}

func (s *Service) connectionSettings() (ConnectionSettings, config.GraphKit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings, s.envCfg
}

// envOwned reports GRAPHKIT_* naming a connection, which wins over anything saved in the panel.
func (s *Service) envOwned() bool {
	_, env := s.connectionSettings()
	return env.Enabled()
}

// LoadConnection applies the stored connection when the environment names none, and turns the store off when it was removed.
func (s *Service) LoadConnection(ctx context.Context) error {
	repo, _ := s.connectionSettings()
	if repo == nil || s.envOwned() {
		return nil
	}
	stored, err := repo.GetConnection(ctx)
	if err != nil {
		return err
	}
	cur := s.current()
	fromSettings := cur != nil && cur.on && cur.source == SourceSettings
	if stored == nil {
		if fromSettings {
			s.Disconnect()
			log.Info().Msg("store: the GraphKit connection was removed; the store is off")
		}
		return nil
	}
	cfg := storedConfig(stored)
	if fromSettings && cur.fingerprint == fingerprint(cfg) {
		return nil
	}
	if err := s.Connect(cfg, SourceSettings); err != nil {
		return err
	}
	log.Info().Str("mode", s.Mode()).Msg("store: connected to GraphKit with the admin panel's settings")
	return nil
}

// WatchConnection rereads the stored connection every 30 seconds, so a change saved on another replica reaches this one.
func (s *Service) WatchConnection(ctx context.Context) {
	if s == nil || s.envOwned() {
		return
	}
	t := time.NewTicker(connectionPollEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.LoadConnection(ctx); err != nil {
				log.Warn().Err(err).Msg("store: reading the stored GraphKit connection failed")
			}
		}
	}
}

func storedConfig(c *repository.StoreConnection) config.GraphKit {
	return config.GraphKit{APIURL: c.APIURL, APIKey: c.APIKey, WebhookSecret: c.WebhookSecret, WebhookSecretPrevious: c.WebhookSecretPrevious,
		WebhookSecretRotatedAt: c.WebhookSecretRotatedAt}
}

// ConnectionView is the connection in use and where it came from, never a secret.
func (s *Service) ConnectionView(ctx context.Context) (*ConnectionView, error) {
	repo, env := s.connectionSettings()
	out := &ConnectionView{Source: s.Source(), Configured: s.Enabled(), Mode: s.Mode(), APIURL: graphkit.DefaultBaseURL,
		DefaultAPIURL: graphkit.DefaultBaseURL, WebhookURL: config.BackendPublicURL() + WebhookPath, SealingAvailable: repo != nil && repo.CanSeal()}
	switch {
	case env.Enabled():
		out.Source = SourceEnvironment
		out.APIURL = orDefault(env.APIURL)
		out.APIKeySet = env.APIKey != "" || env.Fake()
		out.WebhookSecretSet = env.WebhookSecret != ""
	case repo != nil:
		stored, err := repo.GetConnection(ctx)
		if err != nil {
			return nil, err
		}
		if stored != nil {
			out.APIURL = orDefault(stored.APIURL)
			out.APIKeySet, out.WebhookSecretSet = stored.APIKey != "" || storedConfig(stored).Fake(), stored.WebhookSecret != ""
			at := stored.UpdatedAt
			out.UpdatedAt, out.UpdatedBy = &at, stored.UpdatedBy
		}
	}
	d, err := s.PurchasesWithoutConnection(ctx, nil)
	if err != nil {
		return nil, err
	}
	out.PurchasesWithoutConnection = d
	return out, nil
}

func orDefault(u string) string {
	if strings.TrimSpace(u) == "" {
		return graphkit.DefaultBaseURL
	}
	return u
}

// normalizeURL accepts an https GraphKit address; the simulator and plain http only in development.
func normalizeURL(raw string) (string, error) {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	if u == "" {
		return graphkit.DefaultBaseURL, nil
	}
	if strings.EqualFold(u, "fake") {
		if !config.DevEnv() {
			return "", errURLInvalid
		}
		return "fake", nil
	}
	p, err := url.Parse(u)
	if err != nil || p.Host == "" || p.User != nil || p.RawQuery != "" || p.Fragment != "" {
		return "", errURLInvalid
	}
	if p.Scheme != "https" && (p.Scheme != "http" || !config.DevEnv()) {
		return "", errURLInvalid
	}
	return u, nil
}

// merge lays what the operator typed over the stored connection; a new webhook secret keeps the old one verifying through the rotation.
func merge(in ConnectionInput, stored *repository.StoreConnection, now time.Time) (config.GraphKit, error) {
	u, err := normalizeURL(in.APIURL)
	if err != nil {
		return config.GraphKit{}, err
	}
	cfg := config.GraphKit{APIURL: u, APIKey: strings.TrimSpace(in.APIKey), WebhookSecret: strings.TrimSpace(in.WebhookSecret)}
	if stored != nil {
		if cfg.APIKey == "" && stored.APIKey != "" {
			if prev, err := normalizeURL(stored.APIURL); err != nil || prev != u {
				return config.GraphKit{}, errKeyReentry
			}
			cfg.APIKey = stored.APIKey
		}
		switch {
		case cfg.WebhookSecret == "" || cfg.WebhookSecret == stored.WebhookSecret:
			cfg.WebhookSecret = stored.WebhookSecret
			if stored.WebhookSecretRotatedAt != nil && now.Sub(*stored.WebhookSecretRotatedAt) < webhookPreviousSecretTTL {
				cfg.WebhookSecretPrevious, cfg.WebhookSecretRotatedAt = stored.WebhookSecretPrevious, stored.WebhookSecretRotatedAt
			}
		case stored.WebhookSecret != "":
			at := now
			cfg.WebhookSecretPrevious, cfg.WebhookSecretRotatedAt = stored.WebhookSecret, &at
		}
	}
	if cfg.APIKey == "" && !cfg.Fake() {
		return config.GraphKit{}, errKeyRequired
	}
	return cfg, nil
}

// TestConnection asks GraphKit for the partner balance and price list with what the operator typed, saving nothing.
func (s *Service) TestConnection(ctx context.Context, in ConnectionInput) (*ConnectionTest, error) {
	if s.envOwned() {
		return nil, errConfiguredByEnv
	}
	repo, _ := s.connectionSettings()
	var stored *repository.StoreConnection
	if repo != nil {
		var err error
		if stored, err = repo.GetConnection(ctx); err != nil {
			return nil, err
		}
	}
	cfg, err := merge(in, stored, s.now())
	if err != nil {
		return nil, err
	}
	return probe(ctx, cfg)
}

func probe(ctx context.Context, cfg config.GraphKit) (*ConnectionTest, error) {
	var api graphkit.API
	if cfg.Fake() {
		api = graphkit.NewFake()
	} else {
		api = graphkit.New(cfg.APIURL, cfg.APIKey, graphkit.WithRetries(1, 300*time.Millisecond))
	}
	pctx, cancel := context.WithTimeout(ctx, connectionTestTimeout)
	defer cancel()
	bal, err := api.Balance(pctx)
	if err != nil {
		return nil, probeError(err)
	}
	out := &ConnectionTest{Mode: "test", BalanceMicros: bal.BalanceMicros, Currency: bal.Currency}
	if bal.Livemode {
		out.Mode = "live"
	}
	p, err := api.Pricing(pctx)
	if err != nil {
		return nil, probeError(err)
	}
	for _, d := range p.Domains {
		if strings.TrimPrefix(d.TLD, ".") == "com" {
			reg, renew := d.RegisterMicros, d.RenewMicros
			out.ComRegisterMicros, out.ComRenewMicros = &reg, &renew
		}
	}
	return out, nil
}

func probeError(err error) error {
	logUpstream("connection.test", err)
	if ge, ok := graphkit.AsError(err); ok && (ge.Status == http.StatusUnauthorized || ge.Status == http.StatusForbidden || ge.Code == graphkit.CodeUnauthorized) {
		return errKeyRejected
	}
	return errNoAnswer
}

// SaveConnection checks the key against GraphKit, stores it sealed, and switches the store onto it at once.
func (s *Service) SaveConnection(ctx context.Context, in ConnectionInput, by uuid.UUID) (*ConnectionView, error) {
	if s.envOwned() {
		return nil, errConfiguredByEnv
	}
	repo, _ := s.connectionSettings()
	if repo == nil || !repo.CanSeal() {
		return nil, errSealingMissing
	}
	stored, err := repo.GetConnection(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := merge(in, stored, s.now())
	if err != nil {
		return nil, err
	}
	if _, err := probe(ctx, cfg); err != nil {
		return nil, err
	}
	row := repository.StoreConnection{APIURL: cfg.APIURL, APIKey: cfg.APIKey, WebhookSecret: cfg.WebhookSecret,
		WebhookSecretPrevious: cfg.WebhookSecretPrevious, WebhookSecretRotatedAt: cfg.WebhookSecretRotatedAt, UpdatedBy: &by}
	if err := repo.PutConnection(ctx, row); err != nil {
		return nil, err
	}
	if err := s.Connect(cfg, SourceSettings); err != nil {
		return nil, err
	}
	log.Info().Str("mode", s.Mode()).Msg("store: connected to GraphKit from the admin panel")
	return s.ConnectionView(ctx)
}

// ClearConnection deletes the stored connection and turns the store off; purchases stay recorded.
func (s *Service) ClearConnection(ctx context.Context) (*ConnectionView, error) {
	if s.envOwned() {
		return nil, errConfiguredByEnv
	}
	repo, _ := s.connectionSettings()
	if repo != nil {
		if err := repo.DeleteConnection(ctx); err != nil {
			return nil, err
		}
	}
	if s.Source() == SourceSettings {
		s.Disconnect()
		log.Info().Msg("store: the GraphKit connection was removed from the admin panel; the store is off")
	}
	return s.ConnectionView(ctx)
}

// PurchasesWithoutConnection counts live purchases while the store is off, for one workspace or with nil the instance; nil when there are none.
func (s *Service) PurchasesWithoutConnection(ctx context.Context, orgID *uuid.UUID) (*Disconnected, error) {
	if s == nil || s.Enabled() {
		return nil, nil
	}
	repo, _ := s.connectionSettings()
	if repo == nil {
		return nil, nil
	}
	m, d, err := repo.CountLivePurchases(ctx, orgID)
	if err != nil || m+d == 0 {
		return nil, err
	}
	return &Disconnected{Mailboxes: m, Domains: d}, nil
}

// UnreachablePurchases is the instance's live purchases while GraphKit is not configured, for the setup and health check.
func (s *Service) UnreachablePurchases(ctx context.Context) (mailboxes, domains int, err error) {
	d, err := s.PurchasesWithoutConnection(ctx, nil)
	if err != nil || d == nil {
		return 0, 0, err
	}
	return d.Mailboxes, d.Domains, nil
}
