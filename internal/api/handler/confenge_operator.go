package handler

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/app/confenge"
	"github.com/warmbly/warmbly/internal/app/token"
)

const (
	confengeOperatorWindow = time.Minute
	confengeOperatorLimit  = 10
)

var confengeOperatorAttempts sync.Map

type confengeOperatorRateState struct {
	mu      sync.Mutex
	started time.Time
	count   int
}

// ConfengeOperatorSession mints the loopback-only operator session.
func (h *Handler) ConfengeOperatorSession(c *gin.Context) {
	if !h.ConfengeConfig.OperatorMode {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	peerIP, local := confengeOperatorPeerIP(c.Request)
	if !local {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if !allowConfengeOperatorSession(peerIP, time.Now()) {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":      "Too Many Requests",
			"message":    "Muitas tentativas de abrir a sessão. Aguarde um minuto.",
			"code":       "rate_limit_exceeded",
			"request_id": c.GetString("request_id"),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), authRequestTimeout)
	defer cancel()

	user, err := confenge.ValidateOperatorIdentity(
		ctx,
		h.ConfengeConfig,
		h.UserRepo,
		h.OrganizationService,
	)
	if err != nil {
		confengeOperatorUnavailable(c)
		return
	}

	session, xerr := h.TokenService.GenerateSessionWithOrg(
		ctx,
		user.ID,
		user.Email,
		peerIP,
		c.Request.UserAgent(),
		token.AuthProviderConfenge,
		&h.ConfengeConfig.OperatorOrgID,
	)
	if xerr != nil {
		confengeOperatorUnavailable(c)
		return
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, session)
}

func confengeOperatorRequestIsLocal(request *http.Request) bool {
	_, ok := confengeOperatorPeerIP(request)
	return ok
}

func confengeOperatorPeerIP(request *http.Request) (string, bool) {
	if request == nil {
		return "", false
	}
	peer, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		peer = request.RemoteAddr
	}
	peerIP := net.ParseIP(peer)
	if peerIP == nil || (!peerIP.IsLoopback() && !peerIP.IsPrivate()) {
		return "", false
	}
	host := request.Host
	if parsedHost, _, splitErr := net.SplitHostPort(host); splitErr == nil {
		host = parsedHost
	}
	hostIP := net.ParseIP(host)
	if host != "localhost" && (hostIP == nil || !hostIP.IsLoopback()) {
		return "", false
	}
	if origin := strings.TrimSpace(request.Header.Get("Origin")); origin != "" {
		parsed, parseErr := url.Parse(origin)
		if parseErr != nil {
			return "", false
		}
		originIP := net.ParseIP(parsed.Hostname())
		if parsed.Hostname() != "localhost" && (originIP == nil || !originIP.IsLoopback()) {
			return "", false
		}
	}
	return peerIP.String(), true
}

func allowConfengeOperatorSession(ip string, now time.Time) bool {
	value, _ := confengeOperatorAttempts.LoadOrStore(ip, &confengeOperatorRateState{started: now})
	state := value.(*confengeOperatorRateState)
	state.mu.Lock()
	defer state.mu.Unlock()
	if now.Sub(state.started) >= confengeOperatorWindow {
		state.started = now
		state.count = 0
	}
	if state.count >= confengeOperatorLimit {
		return false
	}
	state.count++
	return true
}

func confengeOperatorUnavailable(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error":      "Service Unavailable",
		"message":    "A sessão do operador CONFENGE não está disponível.",
		"code":       "CONFENGE_OPERATOR_SESSION_UNAVAILABLE",
		"request_id": c.GetString("request_id"),
	})
}
