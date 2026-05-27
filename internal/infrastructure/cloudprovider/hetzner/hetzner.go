// Package hetzner implements cloudprovider.Provider over the Hetzner Cloud
// REST API (https://docs.hetzner.cloud/).
//
// Auth is a single bearer token (Project API token). One token is one
// project; multi-project operators register multiple cloud_credentials rows.
package hetzner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/warmbly/warmbly/internal/infrastructure/cloudprovider"
)

const (
	defaultBaseURL = "https://api.hetzner.cloud/v1"
	defaultTimeout = 30 * time.Second
)

// Client is the Hetzner Cloud API client implementing cloudprovider.Provider.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// Option customizes the Client. WithHTTPClient and WithBaseURL are useful
// for tests against httptest.Server.
type Option func(*Client)

func WithBaseURL(u string) Option          { return func(c *Client) { c.baseURL = u } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// New returns a Client authenticated with the given Hetzner project token.
func New(token string, opts ...Option) (*Client, error) {
	if token == "" {
		return nil, errors.New("hetzner: token is required")
	}
	c := &Client{
		baseURL: defaultBaseURL,
		token:   token,
		http:    &http.Client{Timeout: defaultTimeout},
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

func (c *Client) Name() string { return "hetzner" }

// ---------------------------------------------------------------------------
// Plumbing
// ---------------------------------------------------------------------------

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("hetzner: marshal body: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("hetzner: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("hetzner: read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var e struct {
			Error struct {
				Code, Message string
			} `json:"error"`
		}
		_ = json.Unmarshal(respBody, &e)
		if e.Error.Message != "" {
			return fmt.Errorf("hetzner: %s %s: %d %s (%s)",
				method, path, resp.StatusCode, e.Error.Message, e.Error.Code)
		}
		return fmt.Errorf("hetzner: %s %s: %d %s",
			method, path, resp.StatusCode, string(respBody))
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("hetzner: decode response: %w", err)
	}
	return nil
}

// Verify hits a cheap authenticated endpoint to confirm the token is valid.
func (c *Client) Verify(ctx context.Context) error {
	var out struct {
		Datacenters []map[string]any `json:"datacenters"`
	}
	return c.do(ctx, http.MethodGet, "/datacenters", nil, &out)
}

// ---------------------------------------------------------------------------
// Catalog: locations, server_types, images
// ---------------------------------------------------------------------------

type apiLocation struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Country     string `json:"country"`
	City        string `json:"city"`
	NetworkZone string `json:"network_zone"`
}

func (c *Client) Locations(ctx context.Context) ([]cloudprovider.Location, error) {
	var out struct {
		Locations []apiLocation `json:"locations"`
	}
	if err := c.do(ctx, http.MethodGet, "/locations", nil, &out); err != nil {
		return nil, err
	}
	locs := make([]cloudprovider.Location, 0, len(out.Locations))
	for _, l := range out.Locations {
		locs = append(locs, cloudprovider.Location{
			Name:        l.Name,
			Description: l.Description,
			City:        l.City,
			Country:     l.Country,
			Network:     l.NetworkZone,
		})
	}
	return locs, nil
}

type apiPrice struct {
	Location     string `json:"location"`
	PriceMonthly struct {
		Gross string `json:"gross"`
		Net   string `json:"net"`
	} `json:"price_monthly"`
}

type apiServerType struct {
	ID           int        `json:"id"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	Cores        int        `json:"cores"`
	Memory       float64    `json:"memory"`
	Disk         int        `json:"disk"`
	StorageType  string     `json:"storage_type"`
	CPUType      string     `json:"cpu_type"`
	Architecture string     `json:"architecture"`
	Prices       []apiPrice `json:"prices"`
}

func (c *Client) ServerTypes(ctx context.Context) ([]cloudprovider.ServerType, error) {
	var out struct {
		ServerTypes []apiServerType `json:"server_types"`
	}
	if err := c.do(ctx, http.MethodGet, "/server_types?per_page=100", nil, &out); err != nil {
		return nil, err
	}
	types := make([]cloudprovider.ServerType, 0, len(out.ServerTypes))
	for _, t := range out.ServerTypes {
		st := cloudprovider.ServerType{
			Name:         t.Name,
			Description:  t.Description,
			Cores:        t.Cores,
			Memory:       t.Memory,
			Disk:         t.Disk,
			StorageType:  t.StorageType,
			CPUType:      t.CPUType,
			Architecture: t.Architecture,
		}
		// Use the cheapest available location price for the headline.
		for _, p := range t.Prices {
			gross, _ := strconv.ParseFloat(p.PriceMonthly.Gross, 64)
			if st.PriceMonthlyEUR == 0 || gross < st.PriceMonthlyEUR {
				st.PriceMonthlyEUR = gross
			}
		}
		types = append(types, st)
	}
	return types, nil
}

func (c *Client) Images(ctx context.Context) ([]cloudprovider.Image, error) {
	var out struct {
		Images []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			OSFlavor    string `json:"os_flavor"`
			OSVersion   string `json:"os_version"`
			Type        string `json:"type"`
		} `json:"images"`
	}
	if err := c.do(ctx, http.MethodGet, "/images?type=system&per_page=100", nil, &out); err != nil {
		return nil, err
	}
	imgs := make([]cloudprovider.Image, 0, len(out.Images))
	for _, i := range out.Images {
		if i.Type != "system" {
			continue
		}
		imgs = append(imgs, cloudprovider.Image{
			Name:        i.Name,
			Description: i.Description,
			OSFlavor:    i.OSFlavor,
			OSVersion:   i.OSVersion,
		})
	}
	return imgs, nil
}

// ---------------------------------------------------------------------------
// Server lifecycle
// ---------------------------------------------------------------------------

type createServerReq struct {
	Name             string            `json:"name"`
	ServerType       string            `json:"server_type"`
	Image            string            `json:"image"`
	Location         string            `json:"location,omitempty"`
	Datacenter       string            `json:"datacenter,omitempty"`
	SSHKeys          []string          `json:"ssh_keys,omitempty"`
	UserData         string            `json:"user_data,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	PlacementGroup   *string           `json:"placement_group,omitempty"`
	Networks         []string          `json:"networks,omitempty"`
	Firewalls        []firewallRef     `json:"firewalls,omitempty"`
	StartAfterCreate bool              `json:"start_after_create"`
}
type firewallRef struct {
	Firewall string `json:"firewall"`
}

type apiServer struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	PublicNet struct {
		IPv4 struct {
			IP string `json:"ip"`
		} `json:"ipv4"`
		IPv6 struct {
			IP string `json:"ip"`
		} `json:"ipv6"`
	} `json:"public_net"`
}

func (c *Client) CreateServer(ctx context.Context, req cloudprovider.CreateServerRequest) (*cloudprovider.Server, error) {
	body := createServerReq{
		Name:             req.Name,
		ServerType:       req.ServerType,
		Image:            req.Image,
		Location:         req.Location,
		Datacenter:       req.Datacenter,
		SSHKeys:          req.SSHKeyIDs,
		UserData:         req.UserData,
		Labels:           req.Labels,
		StartAfterCreate: req.StartAfterCreate,
	}
	if req.PlacementGroup != "" {
		body.PlacementGroup = &req.PlacementGroup
	}
	if req.PrivateNetwork != "" {
		body.Networks = []string{req.PrivateNetwork}
	}
	if req.Firewall != "" {
		body.Firewalls = []firewallRef{{Firewall: req.Firewall}}
	}

	var out struct {
		Server apiServer `json:"server"`
	}
	if err := c.do(ctx, http.MethodPost, "/servers", body, &out); err != nil {
		return nil, err
	}
	return &cloudprovider.Server{
		ID:         strconv.Itoa(out.Server.ID),
		Name:       out.Server.Name,
		Status:     out.Server.Status,
		PublicIPv4: out.Server.PublicNet.IPv4.IP,
		PublicIPv6: out.Server.PublicNet.IPv6.IP,
	}, nil
}

func (c *Client) DeleteServer(ctx context.Context, serverID string) error {
	return c.do(ctx, http.MethodDelete, "/servers/"+serverID, nil, nil)
}

// ---------------------------------------------------------------------------
// Primary IPs
// ---------------------------------------------------------------------------

type createPrimaryIPReq struct {
	Type         string            `json:"type"`
	Name         string            `json:"name"`
	Datacenter   string            `json:"datacenter,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	AssigneeType string            `json:"assignee_type"`
}

type apiPrimaryIP struct {
	ID           int    `json:"id"`
	Type         string `json:"type"`
	IP           string `json:"ip"`
	AssigneeID   *int   `json:"assignee_id"`
	AssigneeType string `json:"assignee_type"`
	AutoDelete   bool   `json:"auto_delete"`
}

func (c *Client) CreatePrimaryIP(ctx context.Context, req cloudprovider.CreatePrimaryIPRequest) (*cloudprovider.PrimaryIP, error) {
	body := createPrimaryIPReq{
		Type:         req.Type,
		Name:         req.Name,
		Datacenter:   req.Datacenter,
		Labels:       req.Labels,
		AssigneeType: "server",
	}
	var out struct {
		PrimaryIP apiPrimaryIP `json:"primary_ip"`
	}
	if err := c.do(ctx, http.MethodPost, "/primary_ips", body, &out); err != nil {
		return nil, err
	}
	return &cloudprovider.PrimaryIP{
		ID:   strconv.Itoa(out.PrimaryIP.ID),
		Type: out.PrimaryIP.Type,
		IP:   out.PrimaryIP.IP,
	}, nil
}

func (c *Client) AssignPrimaryIP(ctx context.Context, ipID, serverID string) error {
	sid, err := strconv.Atoi(serverID)
	if err != nil {
		return fmt.Errorf("hetzner: assign: invalid server id %q", serverID)
	}
	body := struct {
		AssigneeType string `json:"assignee_type"`
		AssigneeID   int    `json:"assignee_id"`
	}{AssigneeType: "server", AssigneeID: sid}
	return c.do(ctx, http.MethodPost, "/primary_ips/"+ipID+"/actions/assign", body, nil)
}

func (c *Client) UnassignPrimaryIP(ctx context.Context, ipID string) error {
	return c.do(ctx, http.MethodPost, "/primary_ips/"+ipID+"/actions/unassign", nil, nil)
}

func (c *Client) DeletePrimaryIP(ctx context.Context, ipID string) error {
	return c.do(ctx, http.MethodDelete, "/primary_ips/"+ipID, nil, nil)
}

func (c *Client) SetReverseDNS(ctx context.Context, ipID, hostname string) error {
	body := struct {
		IP     string `json:"ip"`
		DNSPtr string `json:"dns_ptr"`
	}{DNSPtr: hostname}
	return c.do(ctx, http.MethodPost, "/primary_ips/"+ipID+"/actions/change_dns_ptr", body, nil)
}

// Compile-time check.
var _ cloudprovider.Provider = (*Client)(nil)
