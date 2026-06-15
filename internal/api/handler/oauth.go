package handler

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/oauth"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// respondOAuthError renders an *oauth.OAuthError as its RFC 6749 JSON body, or a
// generic invalid_request for anything else.
func respondOAuthError(c *gin.Context, err error) {
	var oe *oauth.OAuthError
	if errors.As(err, &oe) {
		c.JSON(oe.HTTPStatus, oe)
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "error_description": err.Error()})
}

// --- Application management (JWT + org gate) --------------------------------

func (h *Handler) ListOAuthApplications(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	apps, err := h.OAuthService.ListApplications(c.Request.Context(), *orgID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "lookup failed"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"applications": apps})
}

func (h *Handler) CreateOAuthApplication(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	userID, uerr := middleware.GetUserUUID(c)
	if uerr != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	var w models.OAuthApplicationWrite
	if err := c.ShouldBindJSON(&w); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	app, err := h.OAuthService.RegisterApplication(c.Request.Context(), *orgID, userID, w)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, app)
}

func (h *Handler) GetOAuthApplication(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	app, gerr := h.OAuthService.GetApplication(c.Request.Context(), *orgID, id)
	if gerr != nil {
		errx.JSON(c, errx.New(errx.Internal, "lookup failed"))
		return
	}
	if app == nil {
		errx.JSON(c, errx.New(errx.NotFound, "application not found"))
		return
	}
	c.JSON(http.StatusOK, app)
}

func (h *Handler) UpdateOAuthApplication(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	var w models.OAuthApplicationWrite
	if err := c.ShouldBindJSON(&w); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	app, uerr := h.OAuthService.UpdateApplication(c.Request.Context(), *orgID, id, w)
	if uerr != nil {
		errx.JSON(c, errx.New(errx.BadRequest, uerr.Error()))
		return
	}
	c.JSON(http.StatusOK, app)
}

func (h *Handler) DeleteOAuthApplication(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	if derr := h.OAuthService.DeleteApplication(c.Request.Context(), *orgID, id); derr != nil {
		errx.JSON(c, errx.New(errx.Internal, "delete failed"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

func (h *Handler) RotateOAuthApplicationSecret(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	secret, rerr := h.OAuthService.RotateSecret(c.Request.Context(), *orgID, id)
	if rerr != nil {
		errx.JSON(c, errx.New(errx.BadRequest, rerr.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"client_secret": secret})
}

// GetOAuthAppWebhookSecret reveals the app's webhook signing secret (used to
// verify every app-webhook delivery, from any org).
//
// GET /oauth/applications/:id/webhook-secret
func (h *Handler) GetOAuthAppWebhookSecret(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	secret, rerr := h.OAuthService.WebhookSecret(c.Request.Context(), *orgID, id)
	if rerr != nil {
		errx.JSON(c, errx.New(errx.NotFound, rerr.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"webhook_secret": secret})
}

// RotateOAuthAppWebhookSecret mints a new app webhook signing secret and
// re-materializes every install with it.
//
// POST /oauth/applications/:id/webhook-secret/rotate
func (h *Handler) RotateOAuthAppWebhookSecret(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	secret, rerr := h.OAuthService.RotateWebhookSecret(c.Request.Context(), *orgID, id)
	if rerr != nil {
		errx.JSON(c, errx.New(errx.BadRequest, rerr.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"webhook_secret": secret})
}

// ListOAuthAppWebhookEndpoints returns the per-org endpoints the app has
// materialized (every org that authorized it), with health.
//
// GET /oauth/applications/:id/webhook-endpoints
func (h *Handler) ListOAuthAppWebhookEndpoints(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	app, aerr := h.OAuthService.GetApplication(c.Request.Context(), *orgID, id)
	if aerr != nil || app == nil {
		errx.JSON(c, errx.New(errx.NotFound, "application not found"))
		return
	}
	eps, lerr := h.WebhookService.ListAppEndpoints(c.Request.Context(), id)
	if lerr != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to list endpoints"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"endpoints": eps})
}

// ListOAuthAppWebhookDeliveries returns the app-developer delivery log: every
// attempt across all of the app's endpoints, filterable + paginated.
//
// GET /oauth/applications/:id/webhook-deliveries
func (h *Handler) ListOAuthAppWebhookDeliveries(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	app, aerr := h.OAuthService.GetApplication(c.Request.Context(), *orgID, id)
	if aerr != nil || app == nil {
		errx.JSON(c, errx.New(errx.NotFound, "application not found"))
		return
	}
	filter := models.WebhookDeliveryFilter{
		Status:    c.Query("status"),
		EventType: c.Query("event_type"),
		Limit:     50,
	}
	if raw := c.Query("limit"); raw != "" {
		n, perr := strconv.Atoi(raw)
		if perr != nil || n <= 0 || n > 200 {
			errx.JSON(c, errx.New(errx.BadRequest, "limit must be between 1 and 200"))
			return
		}
		filter.Limit = n
	}
	offset, xerr := paging.DecodeOffsetCursor(c.Query("cursor"))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	filter.Offset = offset
	result, lerr := h.WebhookService.ListAppDeliveries(c.Request.Context(), id, filter)
	if lerr != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to list deliveries"))
		return
	}
	c.JSON(http.StatusOK, result)
}

// UploadOAuthAppLogo stores an app logo image (PNG/JPG, <=2MB) in public object
// storage and returns its URL. Used by the registration UI during the branding
// step: there is no app id yet, so the returned URL is sent in the create/update
// payload. Reuses the avatar upload validation + storage path.
func (h *Handler) UploadOAuthAppLogo(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	body, mime, ext, xerr := readAvatarUpload(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	key := fmt.Sprintf("oauth-app-logos/%s-%d%s", orgID.String(), time.Now().Unix(), ext)
	url, xerr := putPublicObject(c.Request.Context(), h.Storage, key, body, mime)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"logo_url": url})
}

// --- Authorize (JWT: the logged-in user consents) ---------------------------

func authorizeRequestFrom(get func(string) string) oauth.AuthorizeRequest {
	return oauth.AuthorizeRequest{
		ResponseType:        get("response_type"),
		ClientID:            get("client_id"),
		RedirectURI:         get("redirect_uri"),
		Scope:               get("scope"),
		State:               get("state"),
		CodeChallenge:       get("code_challenge"),
		CodeChallengeMethod: get("code_challenge_method"),
	}
}

// OAuthAuthorizeDetails returns the consent info the dashboard renders before
// asking the user to approve. Read from query params.
func (h *Handler) OAuthAuthorizeDetails(c *gin.Context) {
	req := authorizeRequestFrom(c.Query)
	info, err := h.OAuthService.AuthorizeDetails(c.Request.Context(), req)
	if err != nil {
		respondOAuthError(c, err)
		return
	}
	c.JSON(http.StatusOK, info)
}

// OAuthAuthorize is called when the user approves consent. It mints the code and
// returns the redirect URL the browser should follow back to the app.
func (h *Handler) OAuthAuthorize(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	userID, uerr := middleware.GetUserUUID(c)
	if uerr != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	var body struct {
		ResponseType        string `json:"response_type"`
		ClientID            string `json:"client_id"`
		RedirectURI         string `json:"redirect_uri"`
		Scope               string `json:"scope"`
		State               string `json:"state"`
		CodeChallenge       string `json:"code_challenge"`
		CodeChallengeMethod string `json:"code_challenge_method"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	req := oauth.AuthorizeRequest{
		ResponseType:        body.ResponseType,
		ClientID:            body.ClientID,
		RedirectURI:         body.RedirectURI,
		Scope:               body.Scope,
		State:               body.State,
		CodeChallenge:       body.CodeChallenge,
		CodeChallengeMethod: body.CodeChallengeMethod,
	}
	redirect, err := h.OAuthService.IssueAuthorizationCode(c.Request.Context(), *orgID, userID, req)
	if err != nil {
		respondOAuthError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"redirect_url": redirect})
}

// --- Token + revoke (public; client-authenticated) --------------------------

// clientCredentials pulls client_id/secret from HTTP Basic auth (preferred) or
// the form body, per RFC 6749 §2.3.1.
func clientCredentials(c *gin.Context) (string, string) {
	if id, secret, ok := c.Request.BasicAuth(); ok {
		return id, secret
	}
	return c.PostForm("client_id"), c.PostForm("client_secret")
}

// OAuthToken is the token endpoint. It dispatches on grant_type.
func (h *Handler) OAuthToken(c *gin.Context) {
	grantType := c.PostForm("grant_type")
	clientID, clientSecret := clientCredentials(c)
	switch grantType {
	case "authorization_code":
		resp, err := h.OAuthService.ExchangeCode(
			c.Request.Context(), clientID, clientSecret,
			c.PostForm("code"), c.PostForm("redirect_uri"), c.PostForm("code_verifier"),
		)
		if err != nil {
			respondOAuthError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, resp)
	case "refresh_token":
		resp, err := h.OAuthService.RefreshToken(c.Request.Context(), clientID, clientSecret, c.PostForm("refresh_token"))
		if err != nil {
			respondOAuthError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, resp)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_grant_type", "error_description": "grant_type must be authorization_code or refresh_token"})
	}
}

// OAuthRevoke is the token-revocation endpoint (RFC 7009). It succeeds even for
// an unknown token, so the response never confirms a token's existence.
func (h *Handler) OAuthRevoke(c *gin.Context) {
	clientID, clientSecret := clientCredentials(c)
	token := c.PostForm("token")
	if err := h.OAuthService.RevokeToken(c.Request.Context(), clientID, clientSecret, token); err != nil {
		respondOAuthError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": true})
}

// --- Authorized apps (JWT: the user's connected apps) -----------------------

func (h *Handler) ListAuthorizedApps(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	userID, uerr := middleware.GetUserUUID(c)
	if uerr != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	apps, err := h.OAuthService.ListAuthorizedApps(c.Request.Context(), *orgID, userID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "lookup failed"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"authorized_apps": apps})
}

func (h *Handler) RevokeAuthorizedApp(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	userID, uerr := middleware.GetUserUUID(c)
	if uerr != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	appID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	if rerr := h.OAuthService.RevokeAuthorization(c.Request.Context(), *orgID, userID, appID); rerr != nil {
		errx.JSON(c, errx.New(errx.Internal, "revoke failed"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": true})
}

// --- Discovery (RFC 8414) ---------------------------------------------------

// OAuthServerMetadata advertises the authorization server's endpoints and
// capabilities. The authorization endpoint is the dashboard consent page; token
// and revocation are this API. APP_URL/API_PUBLIC_URL drive the absolute URLs.
func (h *Handler) OAuthServerMetadata(c *gin.Context) {
	appURL := strings.TrimRight(os.Getenv("APP_URL"), "/")
	apiURL := strings.TrimRight(os.Getenv("API_PUBLIC_URL"), "/")
	if apiURL == "" {
		scheme := "https"
		if c.Request.TLS == nil && c.Request.Header.Get("X-Forwarded-Proto") == "" {
			scheme = "http"
		}
		apiURL = scheme + "://" + c.Request.Host
	}
	c.JSON(http.StatusOK, gin.H{
		"issuer":                                apiURL,
		"authorization_endpoint":                appURL + "/oauth/authorize",
		"token_endpoint":                        apiURL + "/v1/oauth/token",
		"revocation_endpoint":                   apiURL + "/v1/oauth/revoke",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"scopes_supported":                      oauth.ScopeList(models.AllAPIPermissionsMask),
	})
}
