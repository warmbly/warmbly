// Platform admin permission bits, mirroring internal/models/admin_permission.go.
// /auth/me serializes the bitmask as a single uint32, so the frontend has to
// know the bit positions to gate nav and pages the way the backend gates routes.

export const AdminPerm = {
    ViewUsers: 1 << 0,
    BanUsers: 1 << 1,
    EditUsers: 1 << 2,
    ImpersonateUsers: 1 << 3,
    ViewWorkers: 1 << 4,
    ManageWorkers: 1 << 5,
    ViewWarmupPool: 1 << 6,
    ManageWarmupBans: 1 << 7,
    ReviewAppeals: 1 << 8,
    ViewCampaigns: 1 << 9,
    StopCampaigns: 1 << 10,
    ViewAnalytics: 1 << 11,
    ViewAuditLogs: 1 << 12,
    ManageRateLimits: 1 << 13,
    ManageSettings: 1 << 14,
    ViewEnterpriseInquiries: 1 << 15,
    ManageEnterpriseInquiries: 1 << 16,
    ManagePlans: 1 << 17,
    ManageBilling: 1 << 18,
    GrantAdminAccess: 1 << 19,
    ViewOrganizations: 1 << 20,
    ManageOrganizations: 1 << 21,
} as const;

export type AdminPermBit = (typeof AdminPerm)[keyof typeof AdminPerm];

// A payload without the bitmask is an older backend, not a denial: callers
// treat "unknown" as visible and let the API answer.
export function hasAdminPerm(mask: number | undefined, perm: number): boolean {
    if (typeof mask !== "number") return true;
    return (mask & perm) === perm;
}
