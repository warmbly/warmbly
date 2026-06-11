// Mirror of internal/models/organization_permission.go.
//
// Permission bits are a uint16 — keep this file in lock-step with the
// backend constants. Changing a value here without changing it there
// silently corrupts the role matrix.
//
// The user-facing label + description live here too so the Roles
// settings page has a single source of truth.

export const PERMISSION_BITS = {
    MANAGE_TEAM:        1 << 0,
    MANAGE_BILLING:     1 << 1,
    MANAGE_CAMPAIGNS:   1 << 2,
    MANAGE_CONTACTS:    1 << 3,
    MANAGE_EMAILS:      1 << 4,
    VIEW_ANALYTICS:     1 << 5,
    SEND_CAMPAIGNS:     1 << 6,
    ACCESS_UNIBOX:      1 << 7,
    MANAGE_SEQUENCES:   1 << 8,
    MANAGE_SETTINGS:    1 << 9,
    VIEW_CAMPAIGNS:     1 << 10,
    VIEW_CONTACTS:      1 << 11,
    TRANSFER_OWNERSHIP: 1 << 12,
    MANAGE_API_KEYS:    1 << 13,
    USE_INTEGRATIONS:   1 << 14,
} as const;

export const ALL_PERMISSIONS = 0xffff;

export type PermissionKey = keyof typeof PERMISSION_BITS;

export interface PermissionDef {
    key: PermissionKey;
    bit: number;
    label: string;
    description: string;
    category: "data" | "people" | "send" | "admin";
}

export const PERMISSION_CATALOG: PermissionDef[] = [
    // Data
    { key: "VIEW_CAMPAIGNS",     bit: PERMISSION_BITS.VIEW_CAMPAIGNS,     label: "View campaigns",    description: "Read campaign settings, sequences, and analytics.",      category: "data" },
    { key: "MANAGE_CAMPAIGNS",   bit: PERMISSION_BITS.MANAGE_CAMPAIGNS,   label: "Manage campaigns",  description: "Create, edit and archive campaigns.",                     category: "data" },
    { key: "VIEW_CONTACTS",      bit: PERMISSION_BITS.VIEW_CONTACTS,      label: "View contacts",     description: "Read contacts, segments and tags.",                       category: "data" },
    { key: "MANAGE_CONTACTS",    bit: PERMISSION_BITS.MANAGE_CONTACTS,    label: "Manage contacts",   description: "Create, edit and delete contacts.",                       category: "data" },
    { key: "MANAGE_SEQUENCES",   bit: PERMISSION_BITS.MANAGE_SEQUENCES,   label: "Manage sequences",  description: "Edit step content + spacing inside a campaign.",          category: "data" },
    { key: "VIEW_ANALYTICS",     bit: PERMISSION_BITS.VIEW_ANALYTICS,     label: "View analytics",    description: "See deliverability + engagement reports.",                category: "data" },
    { key: "USE_INTEGRATIONS",   bit: PERMISSION_BITS.USE_INTEGRATIONS,   label: "Use integrations",  description: "Push contacts and deals to connected CRMs and tools.",    category: "data" },
    // People
    { key: "MANAGE_TEAM",        bit: PERMISSION_BITS.MANAGE_TEAM,        label: "Manage team",       description: "Invite, remove and re-role members.",                     category: "people" },
    { key: "TRANSFER_OWNERSHIP", bit: PERMISSION_BITS.TRANSFER_OWNERSHIP, label: "Transfer ownership", description: "Hand workspace ownership to another member.",            category: "people" },
    // Send
    { key: "MANAGE_EMAILS",      bit: PERMISSION_BITS.MANAGE_EMAILS,      label: "Manage mailboxes",  description: "Connect, disconnect and configure sending mailboxes.",    category: "send" },
    { key: "SEND_CAMPAIGNS",     bit: PERMISSION_BITS.SEND_CAMPAIGNS,     label: "Send campaigns",    description: "Start, pause and resume campaigns.",                      category: "send" },
    { key: "ACCESS_UNIBOX",      bit: PERMISSION_BITS.ACCESS_UNIBOX,      label: "Use unified inbox", description: "Read and reply from the shared inbox.",                   category: "send" },
    // Admin
    { key: "MANAGE_SETTINGS",    bit: PERMISSION_BITS.MANAGE_SETTINGS,    label: "Manage settings",   description: "Edit workspace-wide settings.",                           category: "admin" },
    { key: "MANAGE_BILLING",     bit: PERMISSION_BITS.MANAGE_BILLING,     label: "Manage billing",    description: "View invoices and change the subscription plan.",         category: "admin" },
    { key: "MANAGE_API_KEYS",    bit: PERMISSION_BITS.MANAGE_API_KEYS,    label: "Manage API keys",   description: "Create and revoke workspace API keys.",                   category: "admin" },
];

export const CATEGORY_LABEL = {
    data:   { label: "Data",         description: "Campaigns, contacts, reports." },
    people: { label: "People",       description: "Members and ownership." },
    send:   { label: "Sending",      description: "Mailboxes and campaign delivery." },
    admin:  { label: "Workspace",    description: "Settings, billing, API." },
} as const;

// Roles are workspace data (see /organization/roles). The only hardcoded
// concept left is the OWNER membership status and the permission templates
// the role editor can start from.
export const OWNER_DEF = {
    label: "Owner",
    description: "Full control of the workspace. There is exactly one owner; transfer it from workspace settings.",
    color: "#0ea5e9",
    permissions: ALL_PERMISSIONS,
} as const;

const ALL_DEFINED = PERMISSION_CATALOG.reduce((m, p) => m | p.bit, 0);

export const ROLE_TEMPLATES = [
    { id: "admin", label: "Admin", color: "#8b5cf6", permissions: ALL_DEFINED & ~PERMISSION_BITS.TRANSFER_OWNERSHIP },
    {
        id: "manager",
        label: "Manager",
        color: "#10b981",
        permissions:
            PERMISSION_BITS.MANAGE_CAMPAIGNS | PERMISSION_BITS.MANAGE_CONTACTS | PERMISSION_BITS.MANAGE_EMAILS |
            PERMISSION_BITS.SEND_CAMPAIGNS | PERMISSION_BITS.MANAGE_SEQUENCES | PERMISSION_BITS.VIEW_ANALYTICS |
            PERMISSION_BITS.VIEW_CAMPAIGNS | PERMISSION_BITS.VIEW_CONTACTS | PERMISSION_BITS.ACCESS_UNIBOX |
            PERMISSION_BITS.USE_INTEGRATIONS,
    },
    {
        id: "viewer",
        label: "Viewer",
        color: "#f59e0b",
        permissions: PERMISSION_BITS.VIEW_CAMPAIGNS | PERMISSION_BITS.VIEW_CONTACTS | PERMISSION_BITS.VIEW_ANALYTICS,
    },
] as const;

export function hasPermission(mask: number | undefined, bit: number): boolean {
    if (mask === undefined) return false;
    return (mask & bit) === bit;
}




