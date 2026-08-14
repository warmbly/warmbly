package whatsapp

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Env keys for the WhatsApp channel. Documented in deploy/config/env.example.
const (
	EnvEnabled               = "CONFENGE_WHATSAPP_ENABLED"
	EnvAutoSend              = "CONFENGE_WHATSAPP_AUTO_SEND_ENABLED"
	EnvAutoReply             = "CONFENGE_WHATSAPP_AUTO_REPLY_ENABLED"
	EnvProvider              = "WHATSAPP_PROVIDER"
	EnvEvolutionBaseURL      = "WHATSAPP_EVOLUTION_BASE_URL"
	EnvEvolutionAPIKey       = "WHATSAPP_EVOLUTION_API_KEY"
	EnvEvolutionInstance     = "WHATSAPP_EVOLUTION_INSTANCE"
	EnvEvolutionAllowBaileys = "WHATSAPP_EVOLUTION_ALLOW_BAILEYS"
	EnvWebhookSecret         = "WHATSAPP_WEBHOOK_SECRET"
	EnvCrossChannelHours     = "CONFENGE_CROSS_CHANNEL_MIN_INTERVAL_HOURS"
	EnvServiceWindowHours    = "WHATSAPP_SERVICE_WINDOW_HOURS"
	EnvMaxWebhookBytes       = "WHATSAPP_MAX_WEBHOOK_BYTES"
	EnvAppEnv                = "APP_ENV"
)

// Defaults keep the channel fully off and conservative.
const (
	DefaultCrossChannelHours  = 24
	DefaultServiceWindowHours = 24
	DefaultMaxWebhookBytes    = 1 << 20 // 1 MiB
	DefaultProvider           = ProviderEvolution
)

// Config is runtime configuration for WhatsApp transport + policy.
type Config struct {
	Enabled              bool
	AutoSendEnabled      bool
	AutoReplyEnabled     bool
	Provider             string
	EvolutionBaseURL     string
	EvolutionAPIKey      string
	EvolutionInstance    string
	AllowBaileys         bool
	WebhookSecret        string
	CrossChannelInterval time.Duration
	ServiceWindow        time.Duration
	MaxWebhookBytes      int64
	AppEnv               string
}

// LoadConfig reads WhatsApp-related env vars. Safe defaults keep features off.
func LoadConfig() Config {
	hours := envInt(EnvCrossChannelHours, DefaultCrossChannelHours)
	if hours < 0 {
		hours = DefaultCrossChannelHours
	}
	sw := envInt(EnvServiceWindowHours, DefaultServiceWindowHours)
	if sw < 1 {
		sw = DefaultServiceWindowHours
	}
	maxBytes := envInt(EnvMaxWebhookBytes, DefaultMaxWebhookBytes)
	if maxBytes < 1024 {
		maxBytes = DefaultMaxWebhookBytes
	}
	provider := strings.ToLower(strings.TrimSpace(os.Getenv(EnvProvider)))
	if provider == "" {
		provider = DefaultProvider
	}
	return Config{
		Enabled:              envBool(EnvEnabled, false),
		AutoSendEnabled:      envBool(EnvAutoSend, false),
		AutoReplyEnabled:     envBool(EnvAutoReply, false),
		Provider:             provider,
		EvolutionBaseURL:     strings.TrimSpace(os.Getenv(EnvEvolutionBaseURL)),
		EvolutionAPIKey:      strings.TrimSpace(os.Getenv(EnvEvolutionAPIKey)),
		EvolutionInstance:    strings.TrimSpace(os.Getenv(EnvEvolutionInstance)),
		AllowBaileys:         envBool(EnvEvolutionAllowBaileys, false),
		WebhookSecret:        strings.TrimSpace(os.Getenv(EnvWebhookSecret)),
		CrossChannelInterval: time.Duration(hours) * time.Hour,
		ServiceWindow:        time.Duration(sw) * time.Hour,
		MaxWebhookBytes:      int64(maxBytes),
		AppEnv:               strings.TrimSpace(os.Getenv(EnvAppEnv)),
	}
}

// IsProduction reports whether APP_ENV is production-like.
func (c Config) IsProduction() bool {
	e := strings.ToLower(c.AppEnv)
	return e == "prod" || e == "production"
}

// ValidateStartup fails closed on insecure or policy-violating combinations.
// Called when the WhatsApp feature is enabled at boot.
func (c Config) ValidateStartup() error {
	if !c.Enabled {
		return nil
	}
	// Baileys must never enable under production.
	if c.IsProduction() && c.AllowBaileys {
		return fmt.Errorf("%s cannot be true when APP_ENV is production (Cloud API only)", EnvEvolutionAllowBaileys)
	}
	if c.AllowBaileys && !c.IsProduction() {
		// Visible warning is logged by the caller; config still validates.
	}
	if c.Provider == ProviderEvolution || c.Provider == "" {
		if c.EvolutionBaseURL == "" {
			return fmt.Errorf("%s is required when WhatsApp is enabled with evolution provider", EnvEvolutionBaseURL)
		}
		if c.IsProduction() && !strings.HasPrefix(strings.ToLower(c.EvolutionBaseURL), "https://") {
			// Allow http only for private network URLs in non-strict self-host;
			// production prefers https or private network — require non-empty.
			if !isPrivateOrLocalURL(c.EvolutionBaseURL) {
				return fmt.Errorf("%s must be https or a private-network URL in production", EnvEvolutionBaseURL)
			}
		}
		if c.EvolutionAPIKey == "" {
			return fmt.Errorf("%s is required when WhatsApp is enabled", EnvEvolutionAPIKey)
		}
		if c.EvolutionInstance == "" {
			return fmt.Errorf("%s is required when WhatsApp is enabled", EnvEvolutionInstance)
		}
	}
	if c.WebhookSecret == "" && c.IsProduction() {
		return fmt.Errorf("%s is required in production when WhatsApp is enabled", EnvWebhookSecret)
	}
	if c.CrossChannelInterval < 0 {
		return fmt.Errorf("%s must be >= 0", EnvCrossChannelHours)
	}
	return nil
}

// BaileysWarning returns a non-empty operator warning when Baileys is allowed.
func (c Config) BaileysWarning() string {
	if !c.AllowBaileys {
		return ""
	}
	return "WHATSAPP_EVOLUTION_ALLOW_BAILEYS=true: Baileys/WhatsApp-Web sessions are LAB ONLY. " +
		"Do not use as a substitute for WhatsApp Business Platform policies. " +
		"Cloud API remains the production path."
}

func isPrivateOrLocalURL(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.Contains(lower, "://10.") ||
		strings.Contains(lower, "://192.168.") ||
		strings.Contains(lower, "://172.") ||
		strings.Contains(lower, "://localhost") ||
		strings.Contains(lower, "://127.0.0.1") ||
		strings.Contains(lower, "://evolution")
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
