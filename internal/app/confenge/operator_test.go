package confenge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type operatorUsersStub struct {
	user  *models.User
	scope uint32
	err   error
}

func (s operatorUsersStub) GetUser(context.Context, uuid.UUID) (*models.User, error) {
	return s.user, s.err
}

func (s operatorUsersStub) GetBanState(context.Context, uuid.UUID) (uint32, error) {
	return s.scope, s.err
}

type operatorMembershipStub struct {
	member *models.OrganizationMember
	err    *errx.Error
}

func (s operatorMembershipStub) GetMembership(context.Context, uuid.UUID, uuid.UUID) (*models.OrganizationMember, *errx.Error) {
	return s.member, s.err
}

func TestOperatorModeConfigFailsClosed(t *testing.T) {
	t.Setenv(EnvEnabled, "true")
	t.Setenv(EnvOperatorMode, "true")
	t.Setenv("APP_URL", "http://127.0.0.1:5173")
	t.Setenv(EnvOperatorUserID, "invalid")
	t.Setenv(EnvOperatorOrgID, uuid.NewString())

	if err := LoadConfig().ValidateStartup("production"); err == nil {
		t.Fatal("expected invalid operator user ID to fail startup validation")
	}
}

func TestOperatorModeRequiresLoopbackAppURL(t *testing.T) {
	t.Setenv(EnvEnabled, "true")
	t.Setenv(EnvOperatorMode, "true")
	t.Setenv(EnvOperatorUserID, uuid.NewString())
	t.Setenv(EnvOperatorOrgID, uuid.NewString())
	t.Setenv("APP_URL", "https://confenge.example.com")

	if err := LoadConfig().ValidateStartup("production"); err == nil {
		t.Fatal("expected public APP_URL to fail operator-mode startup validation")
	}
}

func TestValidateOperatorIdentity(t *testing.T) {
	userID := uuid.New()
	orgID := uuid.New()
	accepted := time.Now()
	cfg := Config{Enabled: true, OperatorMode: true, OperatorUserID: userID, OperatorOrgID: orgID}
	user := &models.User{ID: userID, Email: "operador@confenge.local"}
	member := &models.OrganizationMember{
		UserID:         userID,
		OrganizationID: orgID,
		AcceptedAt:     &accepted,
		Permissions:    models.PermViewContacts | models.PermManageContacts,
	}

	got, err := ValidateOperatorIdentity(context.Background(), cfg, operatorUsersStub{user: user}, operatorMembershipStub{member: member})
	if err != nil {
		t.Fatalf("ValidateOperatorIdentity: %v", err)
	}
	if got.ID != userID {
		t.Fatalf("user ID = %s, want %s", got.ID, userID)
	}

	_, err = ValidateOperatorIdentity(
		context.Background(),
		cfg,
		operatorUsersStub{user: user, scope: uint32(models.BanScopeLogin)},
		operatorMembershipStub{member: member},
	)
	if err == nil {
		t.Fatal("expected login-banned operator to be rejected")
	}

	member.Permissions = models.PermViewContacts
	_, err = ValidateOperatorIdentity(context.Background(), cfg, operatorUsersStub{user: user}, operatorMembershipStub{member: member})
	if err == nil {
		t.Fatal("expected operator without manage contacts to be rejected")
	}

	_, err = ValidateOperatorIdentity(context.Background(), cfg, operatorUsersStub{err: errors.New("missing")}, operatorMembershipStub{})
	if err == nil {
		t.Fatal("expected missing operator to be rejected")
	}
}
