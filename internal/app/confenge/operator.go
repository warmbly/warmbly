package confenge

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// OperatorUserStore is the user data needed to validate the dedicated operator.
type OperatorUserStore interface {
	GetUser(context.Context, uuid.UUID) (*models.User, error)
	GetBanState(context.Context, uuid.UUID) (uint32, error)
}

// OperatorMembershipStore is the organization data needed by operator mode.
type OperatorMembershipStore interface {
	GetMembership(context.Context, uuid.UUID, uuid.UUID) (*models.OrganizationMember, *errx.Error)
}

// ValidateOperatorIdentity verifies the dedicated technical operator.
func ValidateOperatorIdentity(
	ctx context.Context,
	cfg Config,
	users OperatorUserStore,
	organizations OperatorMembershipStore,
) (*models.User, error) {
	if !cfg.OperatorMode {
		return nil, fmt.Errorf("%s is disabled", EnvOperatorMode)
	}
	if users == nil || organizations == nil {
		return nil, fmt.Errorf("CONFENGE operator dependencies are unavailable")
	}

	user, err := users.GetUser(ctx, cfg.OperatorUserID)
	if err != nil || user == nil {
		return nil, fmt.Errorf("%s does not identify an active user", EnvOperatorUserID)
	}
	if user.IsPendingDeletion() {
		return nil, fmt.Errorf("CONFENGE operator is pending deletion")
	}
	scope, err := users.GetBanState(ctx, cfg.OperatorUserID)
	if err != nil {
		return nil, fmt.Errorf("read CONFENGE operator ban state: %w", err)
	}
	if models.BanScope(scope).Has(models.BanScopeLogin) {
		return nil, fmt.Errorf("CONFENGE operator is blocked from signing in")
	}

	member, xerr := organizations.GetMembership(ctx, cfg.OperatorOrgID, cfg.OperatorUserID)
	if xerr != nil || member == nil || member.AcceptedAt == nil {
		return nil, fmt.Errorf("CONFENGE operator must be an accepted organization member")
	}
	if !member.HasPermission(models.PermViewContacts) || !member.HasPermission(models.PermManageContacts) {
		return nil, fmt.Errorf("CONFENGE operator requires view_contacts and manage_contacts")
	}
	return user, nil
}
