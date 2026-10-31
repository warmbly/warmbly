package handler

import (
	"crypto/rand"
	"errors"
	"math/big"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/repository"
)

// A tester account is one an operator hands to somebody outside the team: a
// vendor's reviewer during an OAuth verification, an auditor, a support
// engineer. It is an ordinary account with its own workspace, marked exempt
// from the emailed login code because the holder cannot read this instance's
// mail. That exemption is what makes it findable later, so the list below is
// the same set the instance check warns about.

type adminCreateTesterRequest struct {
	Email   string `json:"email"`
	OrgName string `json:"org_name"`
	Reason  string `json:"reason"`
	// OrgID joins the tester to a workspace that already exists instead of
	// minting an empty one. That is what a vendor's reviewer needs: an OAuth
	// verification is judged on the app doing real work, and a workspace with
	// no mailbox in it shows none of that. RoleID is then required, because
	// this is the one path that grants workspace access without anybody in
	// that workspace asking for it, and a default would be a permission
	// nobody chose.
	OrgID  *uuid.UUID `json:"organization_id"`
	RoleID *uuid.UUID `json:"role_id"`
	// PasswordHours is how long the handed-out password works; 0 means the default.
	PasswordHours int `json:"password_hours"`
	// PasswordDays is the older whole-day form, read when PasswordHours is unset.
	PasswordDays int `json:"password_days"`
}

// An initial credential works for at most 48 hours.
const (
	testerPasswordDefaultHours = 48
	testerPasswordMaxHours     = 48
)

// testerPasswordLifetime resolves the requested lifetime, refusing one over the cap.
func testerPasswordLifetime(req adminCreateTesterRequest) (time.Duration, *errx.Error) {
	hours := req.PasswordHours
	if hours == 0 && req.PasswordDays != 0 {
		hours = req.PasswordDays * 24
	}
	if hours == 0 {
		hours = testerPasswordDefaultHours
	}
	if hours < 1 || hours > testerPasswordMaxHours {
		return 0, errx.New(errx.BadRequest, "The initial password can be valid for 1 to 48 hours. Set password_hours to a whole number from 1 to 48.")
	}
	return time.Duration(hours) * time.Hour, nil
}

type adminCreateTesterResponse struct {
	UserID uuid.UUID `json:"user_id"`
	Email  string    `json:"email"`
	OrgID  uuid.UUID `json:"organization_id"`
	// Joined reports whether the tester landed in an existing workspace rather
	// than one made for it, so the panel can say which and the operator is not
	// left guessing what they just handed out.
	Joined bool `json:"joined_existing"`
	// Password is returned once and never stored in a readable form. Losing it
	// means making another tester, which is cheap.
	Password          string    `json:"password"`
	PasswordExpiresAt time.Time `json:"password_expires_at"`
	// PasswordChangeRequired is always true: the holder replaces the password at first sign-in.
	PasswordChangeRequired bool `json:"password_change_required"`
}

// testerPasswordAlphabet leaves out the characters that are misread when a
// password is copied by hand off a screen or out of a form.
const testerPasswordAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func testerPassword() (string, error) {
	const length = 20
	b := make([]byte, length)
	max := big.NewInt(int64(len(testerPasswordAlphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = testerPasswordAlphabet[n.Int64()]
	}
	return "Tester-" + string(b), nil
}

// AdminCreateTester creates the account, its workspace and the exemption in
// one call, so an operator never has to reach for the CLI to let a reviewer in.
func (h *Handler) AdminCreateTester(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	var req adminCreateTesterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "an email and a reason are required"))
		return
	}
	parsed, perr := mail.ParseAddress(strings.TrimSpace(req.Email))
	if perr != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "that is not a valid email address"))
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "a reason is required, so the account is answerable later"))
		return
	}
	if h.UserRepo == nil || h.OrganizationService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "account creation is not available on this instance"))
		return
	}

	if _, nerr := displayname.Validate("Workspace name", req.OrgName, displayname.Workspace, true); nerr != nil {
		errx.JSON(c, nerr)
		return
	}

	lifetime, lerr := testerPasswordLifetime(req)
	if lerr != nil {
		errx.JSON(c, lerr)
		return
	}
	passwordExpiresAt := time.Now().Add(lifetime)

	if req.OrgID != nil && req.RoleID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "joining an existing workspace needs a role, so the access granted is one somebody chose"))
		return
	}

	if existing, lerr := h.UserRepo.GetUserByEmail(c.Request.Context(), parsed.Address); lerr == nil && existing != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "an account with that address already exists"))
		return
	}

	password, gerr := testerPassword()
	if gerr != nil {
		errx.JSON(c, errx.New(errx.Internal, "could not generate a password"))
		return
	}
	hash, herr := argon2.Hash(password)
	if herr != nil {
		errx.JSON(c, errx.New(errx.Internal, "could not hash the password"))
		return
	}

	// One transaction, so a failure cannot leave an account that holds no
	// exemption: that account would be invisible to the tester list,
	// un-retryable because the address was taken, and reachable by whoever
	// held the password.
	created, cerr := h.UserRepo.CreateExemptUser(c.Request.Context(), parsed, hash, reason, adminID, passwordExpiresAt)
	if cerr != nil {
		errx.JSON(c, errx.New(errx.Internal, "could not create the account"))
		return
	}
	// The workspace grant must end exactly when the stored password does.
	if created.PasswordExpiresAt != nil {
		passwordExpiresAt = *created.PasswordExpiresAt
	}

	// A tester joined to an existing workspace gets no workspace of its own on
	// purpose. Owning one would leave the reviewer a member of two, and the
	// dashboard only skips its workspace picker when there is exactly one, so
	// the first thing they would meet is a chooser naming an empty workspace.
	var orgID uuid.UUID
	joined := req.OrgID != nil
	if joined {
		member, merr := h.OrganizationService.AttachTester(c.Request.Context(), *req.OrgID, created.ID, *adminID, *req.RoleID)
		if merr != nil {
			h.undoHalfMadeTester(c, created.ID, merr)
			return
		}
		orgID = member.OrganizationID
	} else {
		orgName := strings.TrimSpace(req.OrgName)
		if orgName == "" {
			orgName = "Tester workspace"
		}
		org, oerr := h.OrganizationService.CreateTesterWorkspace(c.Request.Context(), created.ID, *adminID, orgName, reason, passwordExpiresAt)
		if oerr != nil {
			h.undoHalfMadeTester(c, created.ID, oerr)
			return
		}
		orgID = org.ID
	}

	entry := map[string]any{
		"email": created.Email, "reason": reason, "organization_id": orgID.String(),
		"joined_existing": joined, "password_expires_at": passwordExpiresAt.UTC().Format(time.RFC3339),
	}
	if joined {
		// The role is the whole of what this tester can reach, so it belongs in
		// the audit row rather than only in the members table it can be
		// changed out of later.
		entry["role_id"] = req.RoleID.String()
	}
	h.logTesterAction(c, *adminID, created.ID, "create_tester", entry)

	c.JSON(http.StatusOK, adminCreateTesterResponse{
		UserID:                 created.ID,
		Email:                  created.Email,
		OrgID:                  orgID,
		Joined:                 joined,
		Password:               password,
		PasswordExpiresAt:      passwordExpiresAt,
		PasswordChangeRequired: true,
	})
}

// undoHalfMadeTester removes an account whose workspace step failed. Leaving it
// would take the address without giving anybody anything: revoking the
// exemption does not free the address, and a retry fails the existing-email
// check, so the operator would have nowhere to go. The delete is guarded on the
// account holding no membership, so it cannot remove a tester that did join.
//
// The original error is what the operator sees, because a seat limit and a role
// that does not exist are both things they can fix and neither is a 500. Only a
// cleanup that itself fails changes the answer, since that is the one case
// where something was left behind.
func (h *Handler) undoHalfMadeTester(c *gin.Context, userID uuid.UUID, cause *errx.Error) {
	if derr := h.UserRepo.DeleteOrphanExemptUser(c.Request.Context(), userID); derr != nil {
		errx.JSON(c, errx.New(errx.Internal,
			cause.Message+"; the half-made account could not be removed either, and it is listed under Testers"))
		return
	}
	errx.JSON(c, cause)
}

// AdminListTesters returns every account holding a login-code exemption, which
// is the set an operator needs to review and prune.
func (h *Handler) AdminListTesters(c *gin.Context) {
	if h.UserRepo == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "accounts are not available on this instance"))
		return
	}
	list, err := h.UserRepo.ListLoginCodeExempt(c.Request.Context())
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "could not list tester accounts"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *Handler) AdminSeedTesterWorkspace(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil || orgID == uuid.Nil {
		errx.JSON(c, errx.New(errx.BadRequest, "that is not a workspace id"))
		return
	}
	if h.OrganizationService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "sample data is not available on this instance"))
		return
	}
	result, xerr := h.OrganizationService.SeedTesterWorkspace(c.Request.Context(), orgID, *adminID, c.ClientIP(), c.Request.UserAgent())
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, result)
}

// AdminRevokeTester drops the exemption, the handed-out password and every
// session the account holds. The account stays, so anything it created is
// still attributable.
func (h *Handler) AdminRevokeTester(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "that is not a user id"))
		return
	}
	if h.UserRepo == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "accounts are not available on this instance"))
		return
	}
	cleared, ok := h.revokeExemption(c, userID)
	if !ok {
		return
	}
	h.logTesterAction(c, *adminID, userID, "revoke_tester", map[string]any{"password_cleared": cleared})
	c.JSON(http.StatusOK, gin.H{"revoked": true, "password_cleared": cleared})
}

// revokeExemption ends an exemption and every session the account holds,
// answering the caller itself when it fails.
func (h *Handler) revokeExemption(c *gin.Context, userID uuid.UUID) (passwordCleared, ok bool) {
	ctx := c.Request.Context()
	// Sessions end first, so a failure leaves the tester listed for a retry.
	if h.TokenService != nil {
		if xerr := h.TokenService.RevokeOtherSessions(ctx, userID, uuid.Nil); xerr != nil {
			errs.CaptureException(xerr)
			errx.JSON(c, errx.New(errx.Internal, "could not end the account's sessions"))
			return false, false
		}
	}
	cleared, err := h.UserRepo.RevokeTester(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			errx.JSON(c, errx.New(errx.NotFound, "no such account"))
			return false, false
		}
		errx.JSON(c, errx.New(errx.Internal, "could not revoke the exemption"))
		return false, false
	}
	// A sign-in that landed between the two writes is ended too.
	if h.TokenService != nil {
		if xerr := h.TokenService.RevokeOtherSessions(ctx, userID, uuid.Nil); xerr != nil {
			errs.CaptureException(xerr)
		}
	}
	return cleared, true
}

type adminReissueTesterPasswordResponse struct {
	UserID uuid.UUID `json:"user_id"`
	// Password is returned once and never stored in a readable form.
	Password               string    `json:"password"`
	PasswordExpiresAt      time.Time `json:"password_expires_at"`
	PasswordChangeRequired bool      `json:"password_change_required"`
}

// AdminReissueTesterPassword gives an account created as a tester a fresh
// initial password, valid 48 hours, and signs out everything it held.
func (h *Handler) AdminReissueTesterPassword(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil || userID == uuid.Nil {
		errx.JSON(c, errx.New(errx.BadRequest, "that is not a user id"))
		return
	}
	if h.UserRepo == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "accounts are not available on this instance"))
		return
	}
	ctx := c.Request.Context()

	// Checked before anything is revoked, so a refused call changes nothing.
	tester, terr := h.UserRepo.IsCreatedTester(ctx, userID)
	if errors.Is(terr, repository.ErrUserNotFound) || (terr == nil && !tester) {
		errx.JSON(c, errx.New(errx.NotFound, "no account created as a tester has that id"))
		return
	}
	if terr != nil {
		errs.CaptureException(terr)
		errx.JSON(c, errx.New(errx.Internal, "could not read the account"))
		return
	}

	password, gerr := testerPassword()
	if gerr != nil {
		errx.JSON(c, errx.New(errx.Internal, "could not generate a password"))
		return
	}
	hash, herr := argon2.Hash(password)
	if herr != nil {
		errx.JSON(c, errx.New(errx.Internal, "could not hash the password"))
		return
	}

	// Sessions and remembered devices end first, so a failure leaves the old password in place.
	if h.TokenService != nil {
		if xerr := h.TokenService.RevokeOtherSessions(ctx, userID, uuid.Nil); xerr != nil {
			errs.CaptureException(xerr)
			errx.JSON(c, errx.New(errx.Internal, "could not end the account's sessions"))
			return
		}
	}
	if h.AuthService != nil {
		if xerr := h.AuthService.ForgetTrustedDevices(ctx, userID); xerr != nil {
			errx.JSON(c, errx.New(errx.Internal, "could not forget the account's remembered devices"))
			return
		}
	}
	until, rerr := h.UserRepo.ReissueTesterPassword(ctx, userID, hash)
	if rerr != nil {
		if errors.Is(rerr, repository.ErrNotTester) {
			errx.JSON(c, errx.New(errx.NotFound, "no account created as a tester has that id"))
			return
		}
		errs.CaptureException(rerr)
		errx.JSON(c, errx.New(errx.Internal, "could not store the new password"))
		return
	}
	// A sign-in that landed between the two writes is ended too.
	if h.TokenService != nil {
		if xerr := h.TokenService.RevokeOtherSessions(ctx, userID, uuid.Nil); xerr != nil {
			errs.CaptureException(xerr)
		}
	}

	h.logTesterAction(c, *adminID, userID, "reissue_tester_password", map[string]any{
		"password_expires_at": until.UTC().Format(time.RFC3339),
	})
	c.JSON(http.StatusOK, adminReissueTesterPasswordResponse{
		UserID:                 userID,
		Password:               password,
		PasswordExpiresAt:      until,
		PasswordChangeRequired: true,
	})
}

type adminSetLoginCodeExemptRequest struct {
	Exempt *bool  `json:"exempt"`
	Reason string `json:"reason"`
}

// loginCodeExemptReasonMax bounds the free-text reason kept on the user row.
const loginCodeExemptReasonMax = 500

// AdminSetLoginCodeExempt grants or clears the emailed-login-code exemption on
// an existing account. Clearing goes through the same revoke as a tester.
func (h *Handler) AdminSetLoginCodeExempt(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil || userID == uuid.Nil {
		errx.JSON(c, errx.New(errx.BadRequest, "that is not a user id"))
		return
	}
	var req adminSetLoginCodeExemptRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Exempt == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "exempt is required"))
		return
	}
	if h.UserRepo == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "accounts are not available on this instance"))
		return
	}

	if !*req.Exempt {
		cleared, ok := h.revokeExemption(c, userID)
		if !ok {
			return
		}
		h.logTesterAction(c, *adminID, userID, "revoke_login_code_exempt", map[string]any{"password_cleared": cleared})
		c.JSON(http.StatusOK, gin.H{"login_code_exempt": false, "password_cleared": cleared})
		return
	}

	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "a reason is required"))
		return
	}
	if utf8.RuneCountInString(reason) > loginCodeExemptReasonMax {
		errx.JSON(c, errx.New(errx.BadRequest, "the reason is too long"))
		return
	}
	if err := h.UserRepo.SetLoginCodeExempt(c.Request.Context(), userID, true, reason, adminID); err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			errx.JSON(c, errx.New(errx.NotFound, "no such account"))
			return
		}
		errs.CaptureException(err)
		errx.JSON(c, errx.New(errx.Internal, "could not grant the exemption"))
		return
	}
	h.logTesterAction(c, *adminID, userID, "grant_login_code_exempt", map[string]any{"reason": reason})
	c.JSON(http.StatusOK, gin.H{"login_code_exempt": true, "reason": reason})
}

func (h *Handler) logTesterAction(c *gin.Context, adminID, userID uuid.UUID, action string, details map[string]any) {
	if h.AdminService == nil {
		return
	}
	h.AdminService.LogAdminAction(c.Request.Context(), adminID, action, "user", &userID,
		details, c.ClientIP(), c.Request.UserAgent())
}
