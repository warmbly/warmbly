// Package cloudprovider abstracts over cloud-VPS APIs so the provisioning
// state machine can target multiple providers without baking Hetzner-specific
// types into the orchestration layer.
//
// One implementation today (Hetzner). The interface is intentionally small;
// adding OVH or Vultr later means implementing six methods.
package cloudprovider

import "context"

// Provider is the surface the provisioning state machine talks to.
type Provider interface {
	Name() string

	// Catalog — what's available to provision against. Used by the admin
	// dropdowns when an operator is composing a template.
	Locations(ctx context.Context) ([]Location, error)
	ServerTypes(ctx context.Context) ([]ServerType, error)
	Images(ctx context.Context) ([]Image, error)

	// Auth check, called from the admin "Test connection" button.
	Verify(ctx context.Context) error

	// Provisioning. Each returns the provider-native ID + IPv4 so the state
	// machine can record it for later cleanup.
	CreateServer(ctx context.Context, req CreateServerRequest) (*Server, error)
	DeleteServer(ctx context.Context, serverID string) error

	// Primary IP lifecycle. ipv4_per_server=1 in a template means "use the
	// IP that came with the server" — these calls are only made for extras.
	CreatePrimaryIP(ctx context.Context, req CreatePrimaryIPRequest) (*PrimaryIP, error)
	AssignPrimaryIP(ctx context.Context, ipID, serverID string) error
	UnassignPrimaryIP(ctx context.Context, ipID string) error
	DeletePrimaryIP(ctx context.Context, ipID string) error
	SetReverseDNS(ctx context.Context, ipID, hostname string) error
}

// Location is a region / datacenter where servers can be created.
type Location struct {
	Name        string // "fsn1", "hil", etc.
	Description string // "Falkenstein DC Park 1"
	City        string
	Country     string // ISO-3166 alpha-2
	Network     string // continent or "EU"/"US" grouping for UI
}

// ServerType is one purchasable VPS shape.
type ServerType struct {
	Name              string // "cx22", "cpx11"
	Description       string
	Cores             int
	Memory            float64 // GiB
	Disk              int     // GiB
	StorageType       string  // "local" / "network"
	CPUType           string  // "shared" / "dedicated"
	Architecture      string  // "x86" / "arm"
	PriceMonthlyEUR   float64
	PriceMonthlyUSD   float64
	IncludedTrafficTB float64
}

// Image is an OS image available for new servers.
type Image struct {
	Name        string // "ubuntu-22.04"
	Description string
	OSFlavor    string
	OSVersion   string
}

// CreateServerRequest is what the state machine passes to CreateServer.
type CreateServerRequest struct {
	Name             string
	ServerType       string
	Image            string
	Location         string
	Datacenter       string // overrides Location when set
	SSHKeyIDs        []string
	UserData         string // cloud-init
	Labels           map[string]string
	PlacementGroup   string
	PrivateNetwork   string
	Firewall         string
	StartAfterCreate bool
}

// Server is what CreateServer returns.
type Server struct {
	ID         string
	Name       string
	Status     string
	PublicIPv4 string
	PublicIPv6 string
}

// CreatePrimaryIPRequest configures one extra Primary IP. The IP that
// comes free with a server is created by CreateServer, not here.
type CreatePrimaryIPRequest struct {
	Type       string // "ipv4" / "ipv6"
	Name       string
	Datacenter string // must match the server's datacenter
	Labels     map[string]string
}

// PrimaryIP is what CreatePrimaryIP returns.
type PrimaryIP struct {
	ID                 string
	Type               string
	IP                 string
	AssignedToServerID string // empty when unassigned
}
