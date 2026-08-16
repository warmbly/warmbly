package instancecheck

import (
	"context"
	"fmt"
)

const (
	docsRegistration = "/development/accounts-and-access/#registration-modes"
	docsSignIn       = "/development/accounts-and-access/#sign-in-methods"
	docsAdmins       = "/development/accounts-and-access/#platform-admins"
	docsFirstOwner   = "/development/accounts-and-access/#first-owner"
	docsInvitations  = "/development/accounts-and-access/#invitations"
)

// setupTokenKey mirrors the unexported key in internal/app/bootstrap. The two
// must stay identical or the outstanding-setup-link check goes silent.
const setupTokenKey = "bootstrap:setup_token"

func accessChecks() []check {
	return []check{
		{id: "registration_mode", run: checkRegistrationMode},
		{id: "no_sign_in_method", run: checkNoSignInMethod},
		{id: "single_platform_admin", run: checkSinglePlatformAdmin},
		{id: "bootstrap_password_still_set", run: checkBootstrapPasswordStillSet},
		{id: "setup_link_outstanding", run: checkSetupLinkOutstanding},
		{id: "expired_invitations", run: checkExpiredInvitations},
	}
}

func checkRegistrationMode(ctx context.Context, d Deps, in Input) *Finding {
	return result(CategoryAccess, SeverityInfo, "Registration mode",
		fmt.Sprintf("Registration is %s. `invite_only` means nobody can create an account from the sign-up form; "+
			"people join through an invitation from Settings > Members in the dashboard. "+
			"`true` means signups are fully closed and invitations do not work either.", policy(d).Registration),
		docsRegistration)
}

func checkNoSignInMethod(ctx context.Context, d Deps, in Input) *Finding {
	if !policy(d).DisablePasswordLogin {
		return nil
	}
	if env("OIDC_ISSUER_URL") != "" || runtimeOf(d).OIDCConfigured {
		return nil
	}
	if env("GOOGLE_CLIENT_ID") != "" || env("APPLE_APP_ID") != "" {
		return nil
	}
	return result(CategoryAccess, SeverityError, "No way to sign in",
		"Password login is disabled and no single sign-on provider is configured, so there is no way to sign in to this instance. "+
			"Set DISABLE_PASSWORD_LOGIN=false or configure OIDC_ISSUER_URL.",
		docsSignIn)
}

func checkSinglePlatformAdmin(ctx context.Context, d Deps, in Input) *Finding {
	if d.DB == nil {
		return nil
	}
	var count int
	var email string
	err := d.DB.QueryRow(ctx,
		`SELECT count(*), COALESCE(min(email), '') FROM users WHERE admin_permissions > 0`).Scan(&count, &email)
	if err != nil || count != 1 {
		return nil
	}
	return result(CategoryAccess, SeverityInfo, "Only one platform admin",
		fmt.Sprintf("This instance has one platform admin (%s). If you lose access to that account there is no way "+
			"to grant admin from inside the product. Add a second admin from Instance > Admins.", email),
		docsAdmins)
}

func checkBootstrapPasswordStillSet(ctx context.Context, d Deps, in Input) *Finding {
	if env("WARMBLY_BOOTSTRAP_PASSWORD") == "" || d.DB == nil {
		return nil
	}
	count, err := userCount(ctx, d)
	if err != nil || count == 0 {
		return nil
	}
	return result(CategoryAccess, SeverityWarning, "Bootstrap password is still set",
		"WARMBLY_BOOTSTRAP_PASSWORD is still set in this deployment's environment. "+
			"It is only read while the users table is empty, so it now does nothing except leave a plaintext password "+
			"in your process environment. Remove it.",
		docsFirstOwner)
}

func checkSetupLinkOutstanding(ctx context.Context, d Deps, in Input) *Finding {
	if d.DB == nil || d.Cache == nil {
		return nil
	}
	count, err := userCount(ctx, d)
	if err != nil || count != 0 {
		return nil
	}
	ttl, terr := d.Cache.TTL(ctx, setupTokenKey).Result()
	if terr != nil || ttl <= 0 {
		return nil
	}
	return result(CategoryAccess, SeverityInfo, "A setup link is outstanding",
		fmt.Sprintf("This instance has no accounts yet. A single-use setup link is live for %s; "+
			"find it with `make claim` or in the backend log.", humanizeDuration(ttl)),
		docsFirstOwner)
}

func checkExpiredInvitations(ctx context.Context, d Deps, in Input) *Finding {
	if d.DB == nil {
		return nil
	}
	var count int
	if err := d.DB.QueryRow(ctx,
		`SELECT count(*) FROM organization_invitations WHERE expires_at < NOW()`).Scan(&count); err != nil || count == 0 {
		return nil
	}
	return result(CategoryAccess, SeverityInfo, "Expired invitations",
		fmt.Sprintf("%d invitations have expired and are no longer visible in the dashboard, "+
			"but still hold their email address. Re-inviting the same address now replaces the expired row.", count),
		docsInvitations)
}

func userCount(ctx context.Context, d Deps) (int, error) {
	var count int
	err := d.DB.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count)
	return count, err
}
