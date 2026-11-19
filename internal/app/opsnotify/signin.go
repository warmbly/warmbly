package opsnotify

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// AdminBits reads a user's admin permission mask with one indexed lookup.
type AdminBits interface {
	GetUserAdminPermissions(ctx context.Context, userID uuid.UUID) (uint32, error)
}

// AdminDirectory reads the account a sign-in belongs to.
type AdminDirectory interface {
	GetUserDetail(ctx context.Context, userID uuid.UUID) (*models.AdminUserDetail, error)
}

// AdminSignIns raises EventAdminSignIn for every sign-in by an account that
// holds platform admin access. Plain customer sign-ins are not reported.
type AdminSignIns struct {
	notifier Notifier
	bits     AdminBits
	users    AdminDirectory
}

// NewAdminSignIns builds the token service's session observer.
func NewAdminSignIns(notifier Notifier, bits AdminBits, users AdminDirectory) *AdminSignIns {
	return &AdminSignIns{notifier: notifier, bits: bits, users: users}
}

var _ token.SessionObserver = (*AdminSignIns)(nil)

// SessionStarted runs off the request path, so a slow lookup costs no sign-in.
func (a *AdminSignIns) SessionStarted(ctx context.Context, start token.SessionStart) {
	if a == nil || a.notifier == nil || a.bits == nil || a.users == nil {
		return
	}
	// Almost every sign-in is a customer's; stop at the cheap read.
	perms, err := a.bits.GetUserAdminPermissions(ctx, start.UserID)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	if perms == 0 {
		return
	}
	u, err := a.users.GetUserDetail(ctx, start.UserID)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	if u == nil || u.AdminPermissions == 0 {
		return
	}

	device := strings.TrimSpace(strings.Join(nonBlank(start.Browser, start.OS), " on "))
	if start.NewDevice {
		device = strings.TrimSpace(device + " (new device)")
	}
	second := "No"
	if start.MFAVerified {
		second = "Yes"
	}

	a.notifier.NotifyOperator(
		EventAdminSignIn,
		"Admin signed in",
		u.Email+" started a new session.",
		map[string]string{
			"Account":       u.Email,
			"Method":        signInMethod(start.AuthProvider),
			"Second factor": second,
			"Device":        device,
			"Location":      strings.Join(nonBlank(start.City, start.Country), ", "),
			"IP address":    start.IP,
		},
	)
}

func signInMethod(provider string) string {
	switch provider {
	case token.AuthProviderEmail:
		return "Email and password"
	case token.AuthProviderGoogle:
		return "Google"
	case token.AuthProviderApple:
		return "Apple"
	case token.AuthProviderWebAuthn:
		return "Passkey"
	case token.AuthProviderOIDC:
		return "Single sign-on"
	case token.AuthProviderPartner:
		return "Reseller sign-in link"
	default:
		return provider
	}
}

func nonBlank(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
