package instanceconfig

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/config"
)

// Docs anchors. Kept as constants so a renamed section is one edit.
const (
	docsDeployment = "/development/configuration/#deployment"
	docsSecrets    = "/development/configuration/#secrets"
	docsAddresses  = "/development/configuration/#addresses"
	docsProxy      = "/development/configuration/#network-and-proxy"
	docsSignIn     = "/development/configuration/#authentication"
	docsMail       = "/development/configuration/#platform-mail"
	docsEncryption = "/development/configuration/#encryption"
	docsStorage    = "/development/configuration/#storage"
	docsEventBus   = "/development/configuration/#event-bus"
	docsDatabase   = "/development/configuration/#database"
	docsGeoIP      = "/development/configuration/#geoip"
	docsWorkers    = "/development/configuration/#workers"
	docsCaptcha    = "/development/configuration/#captcha"
	docsSSO        = "/development/accounts-and-access/#single-sign-on"
	docsFirstOwner = "/development/accounts-and-access/#first-owner"
)

// table is the static inventory. Declaration order is display order.
var table = []Entry{
	// Deployment.
	{
		Key: "APP_ENV", Group: GroupDeployment, RuntimeChangeable: ChangeBootOnly,
		Effect:     "dev tolerates the published default secrets and turns on Gin debug logging. Set prod for anything other people can reach.",
		DocsAnchor: docsDeployment,
		Resolve:    envOr("APP_ENV", "dev"),
	},
	{
		Key: "DEPLOYMENT_MODE", Group: GroupDeployment, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Picks the auth defaults (login code, signup lockdown, email verification). Each stays individually overridable.",
		DocsAnchor: docsDeployment, WhenUnset: SourceDerived,
		Resolve: func(*Runtime) string {
			if v := trimmed("DEPLOYMENT_MODE"); v != "" {
				return v
			}
			if config.SelfHosted() {
				return "self_hosted"
			}
			return "cloud"
		},
	},
	{
		Key: "ALLOW_INSECURE_DEFAULTS", Group: GroupDeployment, RuntimeChangeable: ChangeBootOnly,
		Effect:     "true lets the backend start while a published default secret is still in use, instead of refusing.",
		DocsAnchor: docsSecrets,
		Resolve:    boolOr("ALLOW_INSECURE_DEFAULTS", false),
	},
	{
		Key: "GIN_MODE", Group: GroupDeployment, RuntimeChangeable: ChangeBootOnly,
		Effect:     "debug logs every route and request body; release is the production posture.",
		DocsAnchor: docsDeployment, WhenUnset: SourceDerived,
		Resolve: func(*Runtime) string {
			if v := trimmed("GIN_MODE"); v != "" {
				return v
			}
			if trimmed("APP_ENV") == "prod" {
				return "release"
			}
			return "debug"
		},
	},
	{
		Key: "ENV_LABEL", Group: GroupDeployment, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The badge the admin panel shows so a production panel is not mistaken for a local one.",
		DocsAnchor: docsDeployment,
		Resolve:    envValue("ENV_LABEL"),
	},
	{
		Key: "BILLING_PROVIDER", Group: GroupDeployment, RuntimeChangeable: ChangeBootOnly,
		Effect:     "none runs with no Stripe, every feature unlocked and no trial expiry.",
		DocsAnchor: docsDeployment,
		Resolve:    func(*Runtime) string { return config.BillingProvider() },
	},
	{
		Key: "TASKS_PROVIDER", Group: GroupDeployment, RuntimeChangeable: ChangeBootOnly,
		Effect:     "local runs an in-process Postgres poller for campaign ticks and scheduled sends, with no external service.",
		DocsAnchor: docsDeployment,
		Resolve:    func(*Runtime) string { return config.TasksProvider() },
	},
	{
		Key: "TASKS_LOCAL_POLL_INTERVAL", Group: GroupDeployment, RuntimeChangeable: ChangeBootOnly,
		Effect:     "How often the local task poller looks for due work.",
		DocsAnchor: docsDeployment,
		Resolve:    envOr("TASKS_LOCAL_POLL_INTERVAL", "1s"),
	},
	{
		Key: "WARMBLY_ALLOW_UNSAFE_WEBHOOK_URLS", Group: GroupDeployment, RuntimeChangeable: ChangePerRequest,
		Effect:     "true lets customer webhooks point at http:// and private addresses. Development only.",
		DocsAnchor: docsDeployment,
		Resolve:    boolOr("WARMBLY_ALLOW_UNSAFE_WEBHOOK_URLS", false),
	},
	{
		Key: "AI_PROVIDER", Group: GroupDeployment, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Selects the LLM backend for the writing assistant, the dashboard agent and warmup content. Empty runs with AI off.",
		DocsAnchor: docsDeployment,
		Resolve:    envValue("AI_PROVIDER"),
	},
	{
		Key: "AI_MODEL", Group: GroupDeployment, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The model id sent to the AI provider.",
		DocsAnchor: docsDeployment,
		Resolve:    envValue("AI_MODEL"),
	},
	{
		Key: "AI_BASE_URL", Group: GroupDeployment, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The API base for a custom or self-hosted AI provider.",
		DocsAnchor: docsDeployment,
		Resolve:    envValue("AI_BASE_URL"),
	},
	{
		Key: "AI_API_KEY", Group: GroupDeployment, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Credential for the AI provider. An empty AI_PROVIDER with this set sends the key to api.openai.com.",
		DocsAnchor: docsDeployment,
		Resolve:    envValue("AI_API_KEY"),
	},

	// Addresses.
	{
		Key: "APP_URL", Aliases: []string{"FRONTEND_BASE_URL"}, Group: GroupAddresses, RuntimeChangeable: ChangePerRequest,
		Effect:     "The dashboard origin every emailed link is built from: password resets, invitations and the setup link.",
		DocsAnchor: docsAddresses,
		Resolve:    func(*Runtime) string { return config.AppBaseURL() },
	},
	{
		Key: "API_PUBLIC_URL", Group: GroupAddresses, RuntimeChangeable: ChangeBootOnly,
		Effect:     "This backend's public base. The OIDC redirect URL and the frontends' API base derive from it.",
		DocsAnchor: docsAddresses,
		Resolve:    envValue("API_PUBLIC_URL"),
	},
	{
		Key: "API_HOST", Group: GroupAddresses, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The address the API binds. 0.0.0.0:8080 already binds every interface.",
		DocsAnchor: docsAddresses,
		Resolve:    envOr("API_HOST", "0.0.0.0:8080"),
	},
	{
		Key: "APP_ORIGIN", Group: GroupAddresses, RuntimeChangeable: ChangePerRequest,
		Effect:     "The target origin the mailbox OAuth callback posts the authorization code back to. Empty means a wildcard target.",
		DocsAnchor: docsAddresses,
		Resolve:    envValue("APP_ORIGIN"),
	},
	{
		Key: "WEBSOCKET_URL", Group: GroupAddresses, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The realtime endpoint the dashboard connects to for live updates and presence.",
		DocsAnchor: docsAddresses,
		Resolve: func(rt *Runtime) string {
			if rt != nil && rt.WebsocketURL != "" {
				return rt.WebsocketURL
			}
			return os.Getenv("WEBSOCKET_URL")
		},
	},
	{
		Key: "CORS_ALLOW_ORIGINS", Group: GroupAddresses, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The browser origins allowed to call this API. When unset it derives from APP_URL, the websocket origin and, outside prod, the local dev ports.",
		DocsAnchor: docsAddresses, WhenUnset: SourceDerived,
		Resolve: func(rt *Runtime) string {
			if rt != nil && len(rt.CORSOrigins) > 0 {
				return strings.Join(rt.CORSOrigins, ", ")
			}
			return os.Getenv("CORS_ALLOW_ORIGINS")
		},
	},
	{
		Key: "TRUSTED_PROXIES", Group: GroupAddresses, RuntimeChangeable: ChangeBootOnly,
		Effect:     "CIDRs allowed to set X-Forwarded-For. Empty trusts nothing, which is correct for a directly exposed backend.",
		DocsAnchor: docsProxy,
		Resolve:    envValue("TRUSTED_PROXIES"),
	},

	// Database, cache and GeoIP.
	{
		Key: "PRIMARY_DB", Group: GroupDatabase, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The Postgres connection string. Migrations are applied against it at boot.",
		DocsAnchor: docsDatabase,
		Resolve:    envValue("PRIMARY_DB"),
	},
	{
		Key: "GEODB_PATH", Group: GroupDatabase, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Path to a GeoLite2 database. It must be set on the backend in every environment; a missing file at that path is tolerated and only costs city labels.",
		DocsAnchor: docsGeoIP,
		Resolve:    envValue("GEODB_PATH"),
	},
	{
		Key: "REDIS", Group: GroupCache, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The Redis connection string. Rate limits, the organization key cache, the setup token and the realtime bridge all live there.",
		DocsAnchor: docsDatabase,
		Resolve:    envValue("REDIS"),
	},

	// Platform mail.
	{
		Key: "MAIL_TRANSPORT", Group: GroupMail, RuntimeChangeable: ChangeBootOnly,
		Effect:     "smtp delivers through a relay, ses through AWS, log writes messages to the backend log and delivers nothing.",
		DocsAnchor: docsMail, WhenUnset: SourceDerived,
		Resolve: func(rt *Runtime) string {
			if rt != nil && rt.MailTransportKind != "" {
				return rt.MailTransportKind
			}
			return config.MailTransport()
		},
	},
	{
		Key: "EMAIL_NAME", Group: GroupMail, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The display name on platform mail. The backend refuses to start without it; the consumer only warns and silently sends nothing.",
		DocsAnchor: docsMail,
		Resolve:    envValue("EMAIL_NAME"),
	},
	{
		Key: "EMAIL_ADDRESS", Group: GroupMail, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The From address on platform mail.",
		DocsAnchor: docsMail,
		Resolve:    envValue("EMAIL_ADDRESS"),
	},
	{
		Key: "SMTP_HOST", Group: GroupMail, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The submission relay. Setting it selects the smtp transport when MAIL_TRANSPORT is unset.",
		DocsAnchor: docsMail,
		Resolve:    envValue("SMTP_HOST"),
	},
	{
		Key: "SMTP_PORT", Group: GroupMail, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Defaults per security mode: 465 for tls, 25 for none, 587 otherwise.",
		DocsAnchor: docsMail, WhenUnset: SourceDerived,
		Resolve: func(*Runtime) string {
			if s := smtpConfig(); s != nil {
				return s.Port
			}
			return ""
		},
	},
	{
		Key: "SMTP_SECURITY", Group: GroupMail, RuntimeChangeable: ChangeBootOnly,
		Effect:     "starttls upgrades in-band on 587, tls is implicit TLS on 465, none is cleartext and only legitimate for a local sink.",
		DocsAnchor: docsMail, WhenUnset: SourceDerived,
		Resolve: func(*Runtime) string {
			if s := smtpConfig(); s != nil {
				return s.Security
			}
			return ""
		},
	},
	{
		Key: "SMTP_AUTH", Group: GroupMail, RuntimeChangeable: ChangeBootOnly,
		Effect:     "auto picks the strongest mechanism the relay advertises.",
		DocsAnchor: docsMail, WhenUnset: SourceDerived,
		Resolve: func(*Runtime) string {
			if s := smtpConfig(); s != nil {
				return s.Auth
			}
			return ""
		},
	},
	{
		Key: "SMTP_USERNAME", Group: GroupMail, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The relay account. Credentials are never sent over an unencrypted connection.",
		DocsAnchor: docsMail,
		Resolve:    envValue("SMTP_USERNAME"),
	},
	{
		Key: "SMTP_PASSWORD", Group: GroupMail, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The relay password.",
		DocsAnchor: docsMail,
		Resolve:    envValue("SMTP_PASSWORD"),
	},
	{
		Key: "SMTP_EHLO_NAME", Group: GroupMail, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The name announced to the relay. Defaults to the sender domain, because announcing localhost scores as suspicious.",
		DocsAnchor: docsMail, WhenUnset: SourceDerived,
		Resolve: envValue("SMTP_EHLO_NAME"),
	},
	{
		Key: "SMTP_TLS_INSECURE_SKIP_VERIFY", Group: GroupMail, RuntimeChangeable: ChangeBootOnly,
		Effect:     "true disables certificate verification on the platform relay. Only for a relay with a private certificate authority.",
		DocsAnchor: docsMail,
		Resolve:    boolOr("SMTP_TLS_INSECURE_SKIP_VERIFY", false),
	},
	{
		Key: "NOTIFICATION_EMAIL_DAILY_CAP", Group: GroupMail, RuntimeChangeable: ChangePerRequest,
		Effect:     "How many notification digest emails one user can receive in a day.",
		DocsAnchor: docsMail,
		Resolve:    envValue("NOTIFICATION_EMAIL_DAILY_CAP"),
	},
	{
		Key: "EMAIL_BRAND_NAME", Group: GroupMail, RuntimeChangeable: ChangePerRequest,
		Effect:     "The brand platform mail is attributed to. A self-hosted install should not send mail attributed to another company.",
		DocsAnchor: docsMail,
		Resolve:    envValue("EMAIL_BRAND_NAME"),
	},

	// Authentication and access.
	{
		Key: "AUTH_SECRET", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Signs every session and JWT. It must be byte-identical to the realtime service's JWT_SECRET; compare the fingerprints.",
		DocsAnchor: docsSecrets,
		Resolve:    envValue("AUTH_SECRET"),
	},
	{
		Key: "TWOFA_SECRET", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Seals stored TOTP secrets. Falls back to AUTH_SECRET; rotating it invalidates every enrolled authenticator.",
		DocsAnchor: docsSecrets, WhenUnset: SourceDerived,
		Resolve: func(*Runtime) string {
			if v := trimmed("TWOFA_SECRET"); v != "" {
				return v
			}
			return os.Getenv("AUTH_SECRET")
		},
	},
	{
		Key: "AUTH_LOGIN_CODE", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "always emails a code on every login, new_device only on an unrecognised device, off never. A transport that does not deliver demotes always to new_device.",
		DocsAnchor: docsSignIn, WhenUnset: SourceDerived,
		Resolve: func(rt *Runtime) string { return policy(rt).LoginCode },
	},
	{
		Key: "DISABLE_REGISTRATION", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "false is open signup, invite_only accepts only invited addresses, true closes signup and invitations both.",
		DocsAnchor: docsSignIn, WhenUnset: SourceDerived,
		Resolve: func(rt *Runtime) string { return policy(rt).Registration },
	},
	{
		Key: "REQUIRE_EMAIL_VERIFICATION", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "true makes registration complete an emailed code before the account exists.",
		DocsAnchor: docsSignIn, WhenUnset: SourceDerived,
		Resolve: func(rt *Runtime) string { return yesNo(policy(rt).RequireEmailVerification) },
	},
	{
		Key: "DISABLE_PASSWORD_LOGIN", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "true turns off email and password entirely, for deployments that authenticate through OIDC only.",
		DocsAnchor: docsSignIn, WhenUnset: SourceDerived,
		Resolve: func(rt *Runtime) string { return yesNo(policy(rt).DisablePasswordLogin) },
	},
	{
		Key: "SSO_AUTO_PROVISION", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "true lets a verified identity-provider assertion create an account regardless of DISABLE_REGISTRATION.",
		DocsAnchor: docsSSO, WhenUnset: SourceDerived,
		Resolve: func(rt *Runtime) string { return yesNo(policy(rt).SSOAutoProvision) },
	},
	{
		Key: "AUTH_IP_RATE_LIMIT", Group: GroupAuth, RuntimeChangeable: ChangePerRequest,
		Effect:     "Unauthenticated auth requests allowed per IP per 15 minutes.",
		DocsAnchor: docsSignIn,
		Resolve:    envOr("AUTH_IP_RATE_LIMIT", "60"),
	},
	{
		Key: "WEBAUTHN_RP_ID", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The domain passkeys are cryptographically bound to. Changing it invalidates every enrolled passkey.",
		DocsAnchor: docsSignIn, WhenUnset: SourceDerived,
		Resolve: func(rt *Runtime) string {
			if rt != nil && rt.WebAuthnRPID != "" {
				return rt.WebAuthnRPID
			}
			return os.Getenv("WEBAUTHN_RP_ID")
		},
	},
	{
		Key: "WEBAUTHN_RP_ORIGINS", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The full origins allowed to run passkey ceremonies. Derived from CORS_ALLOW_ORIGINS or APP_URL when unset.",
		DocsAnchor: docsSignIn, WhenUnset: SourceDerived,
		Resolve: func(rt *Runtime) string {
			if rt != nil && len(rt.WebAuthnRPOrigins) > 0 {
				return strings.Join(rt.WebAuthnRPOrigins, ", ")
			}
			return os.Getenv("WEBAUTHN_RP_ORIGINS")
		},
	},
	{
		Key: "OIDC_ISSUER_URL", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The OpenID Connect issuer. Setting it enables the single sign-on button, which is the only sign-in path with no dependency on outbound mail.",
		DocsAnchor: docsSSO,
		Resolve:    envValue("OIDC_ISSUER_URL"),
	},
	{
		Key: "OIDC_CLIENT_ID", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The client this backend registers as with the issuer.",
		DocsAnchor: docsSSO,
		Resolve:    envValue("OIDC_CLIENT_ID"),
	},
	{
		Key: "OIDC_CLIENT_SECRET", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The client secret for the OIDC token exchange.",
		DocsAnchor: docsSSO,
		Resolve:    envValue("OIDC_CLIENT_SECRET"),
	},
	{
		Key: "OIDC_REDIRECT_URL", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Where the provider sends the browser back. Derived from API_PUBLIC_URL when unset; empty disables the OIDC path.",
		DocsAnchor: docsSSO, WhenUnset: SourceDerived,
		Resolve: func(rt *Runtime) string {
			if rt != nil && rt.OIDCRedirectURL != "" {
				return rt.OIDCRedirectURL
			}
			if v := trimmed("OIDC_REDIRECT_URL"); v != "" {
				return v
			}
			if base := strings.TrimRight(trimmed("API_PUBLIC_URL"), "/"); base != "" {
				return base + "/v1/auth/oidc/callback"
			}
			return ""
		},
	},
	{
		Key: "OIDC_SCOPES", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Scopes requested from the issuer.",
		DocsAnchor: docsSSO,
		Resolve:    envOr("OIDC_SCOPES", "openid,profile,email"),
	},
	{
		Key: "OIDC_ALLOWED_DOMAINS", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Restricts single sign-on to these email domains. Empty allows every address the issuer asserts.",
		DocsAnchor: docsSSO,
		Resolve:    envValue("OIDC_ALLOWED_DOMAINS"),
	},
	{
		Key: "OIDC_DEFAULT_ORG", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The organization every single sign-on user joins. Without it each new user gets their own single-member organization.",
		DocsAnchor: docsSSO,
		Resolve:    envValue("OIDC_DEFAULT_ORG"),
	},
	{
		Key: "OIDC_PROVIDER_NAME", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The label on the single sign-on button.",
		DocsAnchor: docsSSO,
		Resolve:    envOr("OIDC_PROVIDER_NAME", "Single sign-on"),
	},
	{
		Key: "GOOGLE_CLIENT_ID", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The Google sign-in client, separate from the BOX_GOOGLE_* mailbox client.",
		DocsAnchor: docsSignIn,
		Resolve:    envValue("GOOGLE_CLIENT_ID"),
	},
	{
		Key: "GOOGLE_CLIENT_SECRET", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The Google sign-in client secret.",
		DocsAnchor: docsSignIn,
		Resolve:    envValue("GOOGLE_CLIENT_SECRET"),
	},
	{
		Key: "GOOGLE_REDIRECT_URI", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Where Google sends the browser back after sign-in.",
		DocsAnchor: docsSignIn,
		Resolve:    envValue("GOOGLE_REDIRECT_URI"),
	},
	{
		Key: "APPLE_APP_ID", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The Sign in with Apple service identifier.",
		DocsAnchor: docsSignIn,
		Resolve:    envValue("APPLE_APP_ID"),
	},
	{
		Key: "APPLE_TEAM_ID", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The Apple developer team the key belongs to.",
		DocsAnchor: docsSignIn,
		Resolve:    envValue("APPLE_TEAM_ID"),
	},
	{
		Key: "APPLE_KEY_ID", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The Apple private key identifier.",
		DocsAnchor: docsSignIn,
		Resolve:    envValue("APPLE_KEY_ID"),
	},
	{
		Key: "APPLE_KEY_SECRET", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The Apple private key used to sign client assertions.",
		DocsAnchor: docsSignIn,
		Resolve:    envValue("APPLE_KEY_SECRET"),
	},
	{
		Key: "WARMBLY_BOOTSTRAP_EMAIL", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Creates the first owner unattended. Read only while the users table is empty.",
		DocsAnchor: docsFirstOwner,
		Resolve:    envValue("WARMBLY_BOOTSTRAP_EMAIL"),
	},
	{
		Key: "WARMBLY_BOOTSTRAP_PASSWORD_HASH", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The argon2 PHC string for the first owner. Preferred over the plaintext variant.",
		DocsAnchor: docsFirstOwner,
		Resolve:    envValue("WARMBLY_BOOTSTRAP_PASSWORD_HASH"),
	},
	{
		Key: "WARMBLY_BOOTSTRAP_PASSWORD", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "A plaintext first-owner password. Remove it once the account exists: it is read only while the users table is empty.",
		DocsAnchor: docsFirstOwner,
		Resolve:    envValue("WARMBLY_BOOTSTRAP_PASSWORD"),
	},
	{
		Key: "WARMBLY_BOOTSTRAP_ORG", Group: GroupAuth, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The name of the organization created alongside the first owner.",
		DocsAnchor: docsFirstOwner,
		Resolve:    envValue("WARMBLY_BOOTSTRAP_ORG"),
	},

	// Captcha.
	{
		Key: "CAPTCHA_PROVIDER", Group: GroupCaptcha, RuntimeChangeable: ChangeBootOnly,
		Effect:     "turnstile requires a Cloudflare Turnstile solution on the auth endpoints. Auto-off when no secret is set.",
		DocsAnchor: docsCaptcha, WhenUnset: SourceDerived,
		Resolve: func(*Runtime) string { return config.CaptchaProvider() },
	},
	{
		Key: "TURNSTILE_SECRET", Group: GroupCaptcha, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The Turnstile server-side secret. Empty with CAPTCHA_PROVIDER=turnstile fails every verification.",
		DocsAnchor: docsCaptcha,
		Resolve:    envValue("TURNSTILE_SECRET"),
	},
	{
		Key: "TURNSTILE_BYPASS_TOKEN", Group: GroupCaptcha, RuntimeChangeable: ChangePerRequest,
		Effect:     "A token that skips captcha verification. Honoured only when APP_ENV is dev.",
		DocsAnchor: docsCaptcha,
		Resolve:    envValue("TURNSTILE_BYPASS_TOKEN"),
	},

	// Encryption.
	{
		Key: "KMS_PROVIDER", Group: GroupEncryption, RuntimeChangeable: ChangeBootOnly,
		Effect:     "local wraps organization keys with the master key below; aws wraps them with AWS KMS.",
		DocsAnchor: docsEncryption,
		Resolve:    func(*Runtime) string { return config.KMSProvider() },
	},
	{
		Key: "KMS_LOCAL_MASTER_KEY", Group: GroupEncryption, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The base64 32-byte root key every organization key is wrapped with. Back it up: losing it is unrecoverable.",
		DocsAnchor: docsEncryption,
		Resolve:    envValue("KMS_LOCAL_MASTER_KEY"),
	},
	{
		Key: "KMS_LOCAL_MASTER_KEY_FILE", Group: GroupEncryption, RuntimeChangeable: ChangeBootOnly,
		Effect:     "A file holding the root key, as an alternative to the inline value. Setting both is an error.",
		DocsAnchor: docsEncryption,
		Resolve:    envValue("KMS_LOCAL_MASTER_KEY_FILE"),
	},
	{
		Key: "KMS_AWS_KEY_ID", Group: GroupEncryption, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The AWS KMS key or alias used when KMS_PROVIDER is aws.",
		DocsAnchor: docsEncryption,
		Resolve:    envValue("KMS_AWS_KEY_ID"),
	},
	{
		Key: "CREDENTIALS_ENCRYPTION_KEY", Group: GroupEncryption, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Seals mailbox SMTP and IMAP passwords at rest. Empty stores them unsealed. Back it up: losing it makes connected mailboxes unrecoverable.",
		DocsAnchor: docsEncryption,
		Resolve:    envValue("CREDENTIALS_ENCRYPTION_KEY"),
	},
	{
		Key: "ENCRYPTED_KEYS_PROVIDER", Group: GroupEncryption, RuntimeChangeable: ChangeBootOnly,
		Effect:     "postgres for the backend and consumer, http for workers, which proxy through the backend internal API instead of opening SQL.",
		DocsAnchor: docsEncryption,
		Resolve:    envOr("ENCRYPTED_KEYS_PROVIDER", "postgres"),
	},

	// Storage.
	{
		Key: "BLOB_PROVIDER", Group: GroupStorage, RuntimeChangeable: ChangeBootOnly,
		Effect:     "filesystem stores email bodies, attachments and avatars on disk; s3 stores them in any S3-compatible bucket.",
		DocsAnchor: docsStorage,
		Resolve:    func(*Runtime) string { return config.BlobProvider() },
	},
	{
		Key: "BLOB_FS_ROOT", Group: GroupStorage, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The directory the filesystem provider writes to. The backend, the consumer and every worker on this host must share it.",
		DocsAnchor: docsStorage,
		Resolve:    envValue("BLOB_FS_ROOT"),
	},
	{
		Key: "BLOB_BUCKET", Group: GroupStorage, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The bucket the s3 provider writes to.",
		DocsAnchor: docsStorage,
		Resolve:    envValue("BLOB_BUCKET"),
	},
	{
		Key: "BLOB_PUBLIC_BASE_URL", Group: GroupStorage, RuntimeChangeable: ChangePerRequest,
		Effect:     "The public URL base avatars and logos are served from.",
		DocsAnchor: docsStorage,
		Resolve:    envValue("BLOB_PUBLIC_BASE_URL"),
	},
	{
		Key: "AWS_ENDPOINT_URL_S3", Group: GroupStorage, RuntimeChangeable: ChangeBootOnly,
		Effect:     "A non-AWS S3 endpoint (MinIO, R2, B2).",
		DocsAnchor: docsStorage,
		Resolve:    envValue("AWS_ENDPOINT_URL_S3"),
	},
	{
		Key: "AWS_REGION", Group: GroupStorage, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The AWS region for S3, KMS and SES. Only needed when one of those providers is selected.",
		DocsAnchor: docsStorage,
		Resolve:    envValue("AWS_REGION"),
	},
	{
		Key: "AWS_CONFIG_ENABLED", Group: GroupStorage, RuntimeChangeable: ChangeBootOnly,
		Effect:     "true reads unset secrets from AWS SSM and Secrets Manager instead of failing.",
		DocsAnchor: docsStorage,
		Resolve:    boolOr("AWS_CONFIG_ENABLED", false),
	},

	// Event bus.
	{
		Key: "EVENTBUS_PROVIDER", Group: GroupEventBus, RuntimeChangeable: ChangeBootOnly,
		Effect:     "nats runs one small JetStream binary; kafka needs the kafka build tag and the KAFKA_* settings.",
		DocsAnchor: docsEventBus,
		Resolve:    func(*Runtime) string { return config.EventBusProvider() },
	},
	{
		Key: "CODEC_PROVIDER", Group: GroupEventBus, RuntimeChangeable: ChangeBootOnly,
		Effect:     "json is required wherever workers run: worker command and result envelopes carry untyped bodies Avro cannot serialize.",
		DocsAnchor: docsEventBus,
		Resolve:    func(*Runtime) string { return config.CodecProvider() },
	},
	{
		Key: "NATS_URL", Group: GroupEventBus, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The NATS server campaign sends, mailbox syncs and tracking events flow through.",
		DocsAnchor: docsEventBus,
		Resolve:    envOr("NATS_URL", "nats://localhost:4222"),
	},
	{
		Key: "NATS_STREAM_NAME", Group: GroupEventBus, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The JetStream stream every subject is bound to.",
		DocsAnchor: docsEventBus,
		Resolve:    envOr("NATS_STREAM_NAME", "warmbly"),
	},
	{
		Key: "NATS_SUBJECT_PREFIX", Group: GroupEventBus, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The prefix every subject carries. It must match across the backend, the consumer and every worker.",
		DocsAnchor: docsEventBus,
		Resolve:    envOr("NATS_SUBJECT_PREFIX", "warmbly"),
	},
	{
		Key: "KAFKA_BOOTSTRAP_SERVERS", Group: GroupEventBus, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The Kafka brokers, when EVENTBUS_PROVIDER is kafka.",
		DocsAnchor: docsEventBus,
		Resolve:    envValue("KAFKA_BOOTSTRAP_SERVERS"),
	},
	{
		Key: "SCHEMA_REGISTRY_URL", Group: GroupEventBus, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The Avro schema registry. Only used with the kafka bus and the avro codec.",
		DocsAnchor: docsEventBus,
		Resolve:    envValue("SCHEMA_REGISTRY_URL"),
	},
	{
		Key: "EVENTBUS_HANDLER_TIMEOUT", Group: GroupEventBus, RuntimeChangeable: ChangeBootOnly,
		Effect:     "How long one consumer handler may run before the message is redelivered.",
		DocsAnchor: docsEventBus,
		Resolve:    envValue("EVENTBUS_HANDLER_TIMEOUT"),
	},

	// Workers.
	{
		Key: "INTERNAL_API_TOKEN", Group: GroupWorkers, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The shared token on /api/v1/internal. Workers fetch organization keys with it and the tracking service resolves click links with it.",
		DocsAnchor: docsWorkers,
		Resolve:    envValue("INTERNAL_API_TOKEN"),
	},
	{
		Key: "ENCRYPTED_KEYS_BACKEND_URL", Group: GroupWorkers, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The backend base a worker reaches organization keys through. Empty makes the worker start normally and never register.",
		DocsAnchor: docsWorkers,
		Resolve:    envValue("ENCRYPTED_KEYS_BACKEND_URL"),
	},
	{
		Key: "ENCRYPTED_KEYS_WORKER_TOKEN", Group: GroupWorkers, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The worker's copy of INTERNAL_API_TOKEN. It must match the backend's value exactly.",
		DocsAnchor: docsWorkers,
		Resolve:    envValue("ENCRYPTED_KEYS_WORKER_TOKEN"),
	},
	{
		Key: "WORKER_ID", Group: GroupWorkers, RuntimeChangeable: ChangeBootOnly,
		Effect:     "A worker's stable identity. Derived from the hostname when unset, which changes on every container recreate.",
		DocsAnchor: docsWorkers,
		Resolve:    envValue("WORKER_ID"),
	},
	{
		Key: "WORKER_TIER", Group: GroupWorkers, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Which placement tier a worker accepts. Free-trial organizations place onto free workers, paid ones onto premium.",
		DocsAnchor: docsWorkers,
		Resolve:    envValue("WORKER_TIER"),
	},
	{
		Key: "MAIL_TLS_INSECURE", Group: GroupWorkers, RuntimeChangeable: ChangeBootOnly,
		Effect:     "true disables certificate verification on customer mailbox connections.",
		DocsAnchor: docsWorkers,
		Resolve:    boolOr("MAIL_TLS_INSECURE", false),
	},
	{
		Key: "BOX_GOOGLE_CLIENT_ID", Group: GroupWorkers, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Your Google Cloud OAuth client for connecting Gmail mailboxes. Needed on the backend and on every worker.",
		DocsAnchor: docsWorkers,
		Resolve:    envValue("BOX_GOOGLE_CLIENT_ID"),
	},
	{
		Key: "BOX_GOOGLE_CLIENT_SECRET", Group: GroupWorkers, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The secret for the Gmail mailbox OAuth client.",
		DocsAnchor: docsWorkers,
		Resolve:    envValue("BOX_GOOGLE_CLIENT_SECRET"),
	},
	{
		Key: "BOX_OUTLOOK_CLIENT_ID", Group: GroupWorkers, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Your Microsoft 365 OAuth client for connecting Outlook mailboxes.",
		DocsAnchor: docsWorkers,
		Resolve:    envValue("BOX_OUTLOOK_CLIENT_ID"),
	},
	{
		Key: "BOX_OUTLOOK_CLIENT_SECRET", Group: GroupWorkers, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The secret for the Outlook mailbox OAuth client.",
		DocsAnchor: docsWorkers,
		Resolve:    envValue("BOX_OUTLOOK_CLIENT_SECRET"),
	},

	// Tracking.
	{
		Key: "TRACKING_DOMAIN", Group: GroupTracking, RuntimeChangeable: ChangePerRequest,
		Effect:     "The host open pixels and click links point at. Recipients still receive mail when it is wrong, but nothing is recorded.",
		DocsAnchor: docsAddresses,
		Resolve:    envValue("TRACKING_DOMAIN"),
	},
	{
		Key: "TRACKING_SERVICE_URL", Group: GroupTracking, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Where the backend probes the tracking service. Unset means the System status page cannot report on it.",
		DocsAnchor: docsAddresses,
		Resolve:    envValue("TRACKING_SERVICE_URL"),
	},
	{
		Key: "BACKEND_INTERNAL_URL", Group: GroupTracking, RuntimeChangeable: ChangeBootOnly,
		Effect:     "The backend base the tracking service resolves opaque click tickets through.",
		DocsAnchor: docsWorkers,
		Resolve:    envValue("BACKEND_INTERNAL_URL"),
	},
	{
		Key: "TRACKING_RATE_LIMIT_PER_MIN", Group: GroupTracking, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Tracking events counted per source address per minute. Over-budget pixels are still served, just not counted.",
		DocsAnchor: docsAddresses,
		Resolve:    envOr("TRACKING_RATE_LIMIT_PER_MIN", "300"),
	},

	// Observability.
	{
		Key: "SENTRY_DSN", Group: GroupObservability, RuntimeChangeable: ChangeBootOnly,
		Effect:     "Error reporting. Optional in every environment; unset simply logs instead.",
		DocsAnchor: docsDeployment,
		Resolve:    envValue("SENTRY_DSN"),
	},
}

func envValue(key string) func(*Runtime) string {
	return func(*Runtime) string { return os.Getenv(key) }
}

func envOr(key, def string) func(*Runtime) string {
	return func(*Runtime) string {
		if v := trimmed(key); v != "" {
			return v
		}
		return def
	}
}

func boolOr(key string, def bool) func(*Runtime) string {
	return func(*Runtime) string {
		v := strings.ToLower(trimmed(key))
		switch v {
		case "1", "true", "yes", "on":
			return "true"
		case "0", "false", "no", "off":
			return "false"
		}
		return strconv.FormatBool(def)
	}
}

func trimmed(key string) string { return strings.TrimSpace(os.Getenv(key)) }

func yesNo(v bool) string { return strconv.FormatBool(v) }

// smtpConfig reads the same derived SMTP shape the transport is built from, so
// the displayed port and security mode are the ones actually dialled.
func smtpConfig() *config.SMTPConfig {
	return (&config.Config{}).LoadSMTPConfig(context.Background())
}

// policy resolves the auth policy, preferring the one boot already built.
func policy(rt *Runtime) *config.AuthPolicy {
	if rt != nil && rt.Policy != nil {
		return rt.Policy
	}
	return config.LoadAuthPolicy(MailDelivers(rt))
}

// MailDelivers reports whether the configured transport puts mail on the wire.
func MailDelivers(rt *Runtime) bool {
	if rt != nil && rt.MailTransportKind != "" {
		return rt.MailDelivers
	}
	return config.MailTransport() != config.MailTransportLog
}
