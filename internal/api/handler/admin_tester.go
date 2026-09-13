package handler

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"net/mail"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
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
}

type adminCreateTesterResponse struct {
	UserID uuid.UUID `json:"user_id"`
	Email  string    `json:"email"`
	OrgID  uuid.UUID `json:"organization_id"`
	// Password is returned once and never stored in a readable form. Losing it
	// means making another tester, which is cheap.
	Password string `json:"password"`
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
	created, cerr := h.UserRepo.CreateExemptUser(c.Request.Context(), parsed, hash, reason, adminID)
	if cerr != nil {
		errx.JSON(c, errx.New(errx.Internal, "could not create the account"))
		return
	}

	orgName := strings.TrimSpace(req.OrgName)
	if orgName == "" {
		orgName = "Tester workspace"
	}
	org, oerr := h.OrganizationService.Create(c.Request.Context(), created.ID, orgName)
	if oerr != nil {
		// The account and its exemption committed together, so it is already
		// in the tester list and can be revoked from there. Say so, rather
		// than leaving the operator to guess what survived.
		errx.JSON(c, errx.New(errx.Internal,
			"the account was created but its workspace was not. It is listed under Testers; revoke it there and try again."))
		return
	}
	if h.TrialService != nil {
		// Best effort: without it the workspace has no subscription row and
		// reads as unpaid, which is recoverable from the admin panel.
		_ = h.TrialService.StartFreeTrialWithOrg(c.Request.Context(), created.ID, org.ID)
	}

	h.logTesterAction(c, *adminID, created.ID, "create_tester", map[string]any{
		"email": created.Email, "reason": reason, "organization_id": org.ID.String(),
	})

	c.JSON(http.StatusOK, adminCreateTesterResponse{
		UserID:   created.ID,
		Email:    created.Email,
		OrgID:    org.ID,
		Password: password,
	})
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

// AdminRevokeTester drops the exemption. The account stays, so anything it
// created is still attributable; it simply stops bypassing the login code.
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
	if err := h.UserRepo.SetLoginCodeExempt(c.Request.Context(), userID, false, "", nil); err != nil {
		errx.JSON(c, errx.New(errx.Internal, "could not revoke the exemption"))
		return
	}
	h.logTesterAction(c, *adminID, userID, "revoke_tester", nil)
	c.JSON(http.StatusOK, gin.H{"revoked": true})
}

func (h *Handler) logTesterAction(c *gin.Context, adminID, userID uuid.UUID, action string, details map[string]any) {
	if h.AdminService == nil {
		return
	}
	h.AdminService.LogAdminAction(c.Request.Context(), adminID, action, "user", &userID,
		details, c.ClientIP(), c.Request.UserAgent())
}
