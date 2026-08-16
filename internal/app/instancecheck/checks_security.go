package instancecheck

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/warmbly/warmbly/internal/app/instanceconfig"
	"github.com/warmbly/warmbly/internal/config"
)

const (
	docsSecrets    = "/development/configuration/#secrets"
	docsEncryption = "/development/configuration/#encryption"
	docsWorkers    = "/development/configuration/#workers"
	docsProxy      = "/development/configuration/#network-and-proxy"
	docsCaptcha    = "/development/configuration/#captcha"
	docsDeployment = "/development/configuration/#deployment"
	docsWebhooks   = "/guides/webhooks/"
	docsMail       = "/development/configuration/#platform-mail"
)

func securityChecks() []check {
	return []check{
		{id: "secret_published_default", run: checkSecretPublishedDefault},
		{id: "allow_insecure_defaults", run: checkAllowInsecureDefaults},
		{id: "credentials_key_unset", run: checkCredentialsKeyUnset},
		{id: "internal_token_unset", run: checkInternalTokenUnset},
		{id: "trusted_proxies_unset", run: checkTrustedProxiesUnset},
		{id: "captcha_misconfigured", run: checkCaptchaMisconfigured},
		{id: "turnstile_bypass_set", run: checkTurnstileBypassSet},
		{id: "dev_mode_public", run: checkDevModePublic},
		{id: "unsafe_webhook_urls", run: checkUnsafeWebhookURLs},
		{id: "tls_verification_off", run: checkTLSVerificationOff},
	}
}

func checkSecretPublishedDefault(ctx context.Context, d Deps, in Input) *Finding {
	keys := make([]string, 0, len(instanceconfig.PublishedDefaults))
	for key := range instanceconfig.PublishedDefaults {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var offenders []string
	for _, key := range keys {
		if os.Getenv(key) == instanceconfig.PublishedDefaults[key] {
			offenders = append(offenders, key)
		}
	}
	if len(offenders) == 0 {
		return nil
	}

	severity := SeverityError
	if devEnv() || truthy("ALLOW_INSECURE_DEFAULTS") {
		severity = SeverityWarning
	}

	verb := "hold"
	if len(offenders) == 1 {
		verb = "holds"
	}

	return result(CategorySecurity, severity, "Published default secrets in use",
		fmt.Sprintf("%s still %s the published default value from docker-compose.yml. "+
			"Those values are public in the Warmbly repository, so anyone can forge a session token "+
			"or unwrap every organization key. Generate real values with `make gen-key` and restart.",
			strings.Join(offenders, ", "), verb),
		docsSecrets)
}

func checkAllowInsecureDefaults(ctx context.Context, d Deps, in Input) *Finding {
	if !truthy("ALLOW_INSECURE_DEFAULTS") {
		return nil
	}
	return result(CategorySecurity, SeverityWarning, "Insecure defaults are allowed",
		"ALLOW_INSECURE_DEFAULTS=true is set, so this instance starts even when it is using published default secrets. "+
			"Remove it once you have generated real values.",
		docsSecrets)
}

func checkCredentialsKeyUnset(ctx context.Context, d Deps, in Input) *Finding {
	if env("CREDENTIALS_ENCRYPTION_KEY") != "" {
		return nil
	}
	return result(CategorySecurity, SeverityError, "Mailbox credentials are not sealed",
		"CREDENTIALS_ENCRYPTION_KEY is not set, so mailbox SMTP and IMAP passwords are stored without being sealed. "+
			"Set a 64 hex character key (`openssl rand -hex 32`) before connecting any mailbox. "+
			"Back it up: losing it makes connected mailboxes unrecoverable.",
		docsEncryption)
}

func checkInternalTokenUnset(ctx context.Context, d Deps, in Input) *Finding {
	if env("INTERNAL_API_TOKEN") != "" {
		return nil
	}
	return result(CategorySecurity, SeverityError, "Internal API token is not set",
		"INTERNAL_API_TOKEN is not set, so every request to /api/v1/internal/ is rejected. "+
			"Workers cannot fetch organization keys and the tracking service cannot resolve click links. "+
			"Nothing fails at boot, only at runtime.",
		docsWorkers)
}

func checkTrustedProxiesUnset(ctx context.Context, d Deps, in Input) *Finding {
	// Only a real forwarded request proves a proxy is in front; guessing would
	// warn every directly exposed backend, where empty is correct.
	if !in.Forwarded || env("TRUSTED_PROXIES") != "" {
		return nil
	}
	return result(CategorySecurity, SeverityWarning, "Proxy headers are not trusted",
		"This request arrived with an X-Forwarded-For header but TRUSTED_PROXIES is empty, "+
			"so Warmbly is recording your proxy's address as the client address. "+
			"The per-IP login limiter, session records, audit rows and API key IP allowlists are all reading the wrong address. "+
			"Set TRUSTED_PROXIES to your proxy's CIDR.",
		docsProxy)
}

func checkCaptchaMisconfigured(ctx context.Context, d Deps, in Input) *Finding {
	if config.CaptchaProvider() != "turnstile" || env("TURNSTILE_SECRET") != "" {
		return nil
	}
	return result(CategorySecurity, SeverityError, "Captcha cannot verify",
		"CAPTCHA_PROVIDER is turnstile but TURNSTILE_SECRET is empty, so every captcha verification fails "+
			"and nobody can sign in. Set the secret or set CAPTCHA_PROVIDER=none.",
		docsCaptcha)
}

func checkTurnstileBypassSet(ctx context.Context, d Deps, in Input) *Finding {
	if env("TURNSTILE_BYPASS_TOKEN") == "" {
		return nil
	}
	severity := SeverityWarning
	if devEnv() {
		severity = SeverityInfo
	}
	return result(CategorySecurity, severity, "Turnstile bypass token is set",
		"TURNSTILE_BYPASS_TOKEN is set. It is only honoured when APP_ENV=dev, so on this deployment it does nothing. "+
			"Remove it to avoid confusion.",
		docsCaptcha)
}

func checkDevModePublic(ctx context.Context, d Deps, in Input) *Finding {
	if !devEnv() || isLoopbackURL(appURL()) {
		return nil
	}
	value := env("APP_ENV")
	if value == "" {
		value = "unset"
	}
	return result(CategorySecurity, SeverityWarning, "Dev mode on a public address",
		fmt.Sprintf("This instance is running with APP_ENV=%s on a public address. "+
			"Dev mode allows the published default secrets and enables Gin debug logging. Set APP_ENV=prod.", value),
		docsDeployment)
}

func checkUnsafeWebhookURLs(ctx context.Context, d Deps, in Input) *Finding {
	if !truthy("WARMBLY_ALLOW_UNSAFE_WEBHOOK_URLS") {
		return nil
	}
	return result(CategorySecurity, SeverityWarning, "Unsafe webhook URLs are allowed",
		"Customer webhooks may point at http:// and private addresses. This is intended for development only, "+
			"because it lets any workspace member make the backend reach into your internal network.",
		docsWebhooks)
}

func checkTLSVerificationOff(ctx context.Context, d Deps, in Input) *Finding {
	platform := truthy("SMTP_TLS_INSECURE_SKIP_VERIFY")
	mailbox := truthy("MAIL_TLS_INSECURE")
	if !platform && !mailbox {
		return nil
	}

	subject := "mailbox connections"
	switch {
	case platform && mailbox:
		subject = "platform mail and mailbox connections"
	case platform:
		subject = "platform mail"
	}

	return result(CategorySecurity, SeverityWarning, "TLS verification is disabled",
		fmt.Sprintf("TLS certificate verification is disabled for %s. "+
			"Only do this for a relay using a private certificate authority.", subject),
		docsMail)
}
