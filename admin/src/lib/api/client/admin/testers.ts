// /admin/testers/* — accounts handed to somebody outside the team.
//
// A tester is an ordinary account with its own workspace, marked exempt from
// the emailed login code because the holder cannot read this instance's mail.
// The exemption is what makes it findable, so listing testers and listing
// exemptions are the same query.

import { Request } from "@/lib/api/client";
import type { LoginCodeExemption, CreatedTester, AdminOrgRole, ReissuedTesterPassword } from "@/lib/api/models/admin";

export function listTesters(): Promise<{ data: LoginCodeExemption[] }> {
    return Request({ method: "GET", url: "/admin/testers", authorization: true });
}

export function createTester(body: {
    email: string;
    reason: string;
    /** Names a new workspace. Ignored when organization_id is set. */
    org_name?: string;
    /** Joins an existing workspace instead of minting one. Requires role_id. */
    organization_id?: string;
    role_id?: string;
    /** Hours the password works, 1 to 48. Defaults to 48. */
    password_hours?: number;
}): Promise<CreatedTester> {
    return Request({ method: "POST", url: "/admin/testers", authorization: true, data: body });
}

/** The roles of one workspace, for the join-existing path. */
export function listOrganizationRoles(orgID: string): Promise<{ data: AdminOrgRole[] }> {
    return Request({ method: "GET", url: `/admin/organizations/${orgID}/roles`, authorization: true });
}

export function revokeTester(id: string): Promise<{ revoked: boolean; password_cleared: boolean }> {
    return Request({ method: "DELETE", url: `/admin/testers/${id}`, authorization: true });
}

export function seedTesterWorkspace(orgID: string): Promise<{ organization_id: string; created: boolean; seeded_at: string }> {
    return Request({ method: "POST", url: `/admin/organizations/${orgID}/sample-data`, authorization: true });
}

/** Issues a tester a fresh 48-hour initial password and signs out its sessions. Needs a recent confirmation. */
export function reissueTesterPassword(id: string): Promise<ReissuedTesterPassword> {
    return Request({ method: "POST", url: `/admin/testers/${id}/password`, authorization: true });
}
