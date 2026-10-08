package handler

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/socket"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
)

func (h *Handler) GenerateWebsocket(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	token, xerr := h.SocketService.GenerateWebsocketToken(c.Request.Context(), uid)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	wsURL := token
	endpoint := h.websocketEndpoint()
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil {
			errx.Handle(c, errx.InternalError())
			return
		}

		q := u.Query()
		q.Set("token", token)
		u.RawQuery = q.Encode()
		wsURL = u.String()
	}

	response := gin.H{
		"url":        wsURL,
		"expires_in": socket.SocketTTL.Seconds(),
	}
	if endpoint != "" {
		response["proxy_path"] = "/realtime/socket/websocket?token=" + url.QueryEscape(token)
	}
	c.JSON(http.StatusOK, response)
}

func (h *Handler) websocketEndpoint() string {
	if h.WebsocketURI != "" {
		return config.NormalizeWebsocketURL(h.WebsocketURI)
	}
	return config.WebsocketURL()
}

// Browser connections use the API origin already permitted by the dashboard CSP.
func (h *Handler) ProxyWebsocket(c *gin.Context) {
	endpoint := h.websocketEndpoint()
	if endpoint == "" {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if !strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		c.Status(http.StatusBadRequest)
		return
	}
	claims, xerr := h.TokenService.VerifyTokenFor(token.PurposeWebSocket, c.Query("token"))
	if xerr != nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	proof, err := h.TokenService.GenerateWebsocketProxyProof(claims, c.ClientIP())
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	target, err := url.Parse(endpoint)
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	target.Scheme = strings.Replace(target.Scheme, "ws", "http", 1)
	proxy := &httputil.ReverseProxy{
		Transport: websocketProxyTransport(target),
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.Out.URL.Path = target.Path
			request.Out.URL.RawPath = target.RawPath
			query := target.Query()
			query.Set("token", c.Query("token"))
			query.Set("vsn", c.DefaultQuery("vsn", "1.0.0"))
			request.Out.URL.RawQuery = query.Encode()
			request.Out.Header.Del("Authorization")
			request.Out.Header.Del("Cookie")
			request.Out.Header.Del("X-Warmbly-Token")
			request.Out.Header.Set("X-Warmbly-Proxy-Proof", proof)
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}

func websocketProxyTransport(target *url.URL) http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 10 * time.Second
	// Old compose files advertise localhost to browsers, not to the API container.
	host := target.Hostname()
	ip := net.ParseIP(host)
	if host == "localhost" || (ip != nil && ip.IsLoopback()) {
		if _, err := os.Stat("/.dockerenv"); err == nil {
			dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
			transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				connection, err := dialer.DialContext(ctx, network, address)
				if err == nil {
					return connection, nil
				}
				port := target.Port()
				if port == "" {
					port = "80"
					if target.Scheme == "https" {
						port = "443"
					}
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort("realtime", port))
			}
		}
	}
	return transport
}
