// Package mcp is a minimal client for the MCP streamable-HTTP transport:
// initialize, tools/list, and tools/call over JSON-RPC 2.0 to a single POST
// endpoint. No third-party dependency; the response may be a single JSON
// message or an SSE stream, and both are handled. The HTTP client blocks
// private/loopback/metadata IPs at dial time (safehttp), and callers validate
// the URL up front (webhook.ValidateOutboundURL).
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/safehttp"
)

const (
	protocolVersion = "2025-03-26"
	sessionHeader   = "Mcp-Session-Id"
	maxResponse     = 4 << 20
)

// Client speaks MCP streamable-HTTP.
type Client struct {
	http *http.Client
}

// NewClient returns a client with an SSRF-safe dialer and the given timeout.
func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &Client{http: safehttp.Client(timeout)}
}

// Tool is a tool discovered from a server.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// ListTools initializes a session and returns the server's tools.
func (c *Client) ListTools(ctx context.Context, url, bearer string) ([]Tool, error) {
	sess, err := c.initialize(ctx, url, bearer)
	if err != nil {
		return nil, err
	}
	raw, err := c.call(ctx, url, bearer, sess, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("mcp: decode tools/list: %w", err)
	}
	return out.Tools, nil
}

// CallTool initializes a session and invokes a tool, returning its text content.
func (c *Client) CallTool(ctx context.Context, url, bearer, name string, args json.RawMessage) (string, error) {
	sess, err := c.initialize(ctx, url, bearer)
	if err != nil {
		return "", err
	}
	params := map[string]any{"name": name}
	if len(args) > 0 {
		params["arguments"] = json.RawMessage(args)
	} else {
		params["arguments"] = map[string]any{}
	}
	raw, err := c.call(ctx, url, bearer, sess, "tools/call", params)
	if err != nil {
		return "", err
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return string(raw), nil // pass through non-standard shapes
	}
	var b strings.Builder
	for _, blk := range out.Content {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
		}
	}
	res := b.String()
	if res == "" {
		res = string(raw)
	}
	if out.IsError {
		return "", fmt.Errorf("mcp tool error: %s", res)
	}
	return res, nil
}

// initialize performs the MCP handshake and returns the session id (may be "").
func (c *Client) initialize(ctx context.Context, url, bearer string) (string, error) {
	id := 1
	req := rpcRequest{
		JSONRPC: "2.0", ID: &id, Method: "initialize",
		Params: map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "warmbly", "version": "1.0"},
		},
	}
	_, sess, err := c.post(ctx, url, bearer, "", req)
	if err != nil {
		return "", err
	}
	// Best-effort initialized notification (no response expected).
	_, _, _ = c.post(ctx, url, bearer, sess, rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"})
	return sess, nil
}

// call sends a JSON-RPC request expecting a result.
func (c *Client) call(ctx context.Context, url, bearer, sess, method string, params any) (json.RawMessage, error) {
	id := 2
	resp, _, err := c.post(ctx, url, bearer, sess, rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("mcp: empty response")
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp: %s (%d)", resp.Error.Message, resp.Error.Code)
	}
	return resp.Result, nil
}

// post performs one HTTP POST of a JSON-RPC message and returns the parsed
// response (nil for notifications) plus any session id from the header.
func (c *Client) post(ctx context.Context, url, bearer, sess string, msg rpcRequest) (*rpcResponse, string, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		httpReq.Header.Set("Authorization", "Bearer "+bearer)
	}
	if sess != "" {
		httpReq.Header.Set(sessionHeader, sess)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	newSess := resp.Header.Get(sessionHeader)
	if newSess == "" {
		newSess = sess
	}
	if resp.StatusCode == http.StatusAccepted || msg.ID == nil {
		// Notification accepted; no body to parse.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponse))
		return nil, newSess, nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return nil, newSess, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newSess, fmt.Errorf("mcp: status %d", resp.StatusCode)
	}

	parsed, err := parseRPC(resp.Header.Get("Content-Type"), raw)
	if err != nil {
		return nil, newSess, err
	}
	return parsed, newSess, nil
}

// parseRPC decodes a JSON-RPC response from a plain JSON body or an SSE stream.
func parseRPC(contentType string, raw []byte) (*rpcResponse, error) {
	if strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		// Find the last data frame that parses as a JSON-RPC response with a
		// result or error (skipping server notifications).
		for _, frame := range strings.Split(string(raw), "\n\n") {
			for _, line := range strings.Split(frame, "\n") {
				line = strings.TrimSpace(line)
				if !strings.HasPrefix(line, "data:") {
					continue
				}
				payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				var r rpcResponse
				if err := json.Unmarshal([]byte(payload), &r); err == nil && r.ID != nil {
					return &r, nil
				}
			}
		}
		return nil, errors.New("mcp: no response in event stream")
	}
	var r rpcResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("mcp: decode response: %w", err)
	}
	return &r, nil
}
