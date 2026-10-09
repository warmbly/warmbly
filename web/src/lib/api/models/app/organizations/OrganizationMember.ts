// Mirror of internal/models/organization.go's OrganizationMember.
// `role` and `permissions` come from the server; `permissions` is a
// uint16 bitmask matching internal/models/organization_permission.go.

import type MemberAccess from "./MemberAccess";
import type { AccessScope } from "./MemberAccess";

export interface MemberRole {
    id: string;
    name: string;
    color: string;
}

export default interface OrganizationMember {
    id: string;
    user_id: string;
    // Flattened from the joined user by the API (GET /organization/members).
    // Optional + possibly-empty: always guard before calling string methods.
    email?: string;
    name?: string;
    // "owner" (membership status) or a workspace role name.
    role: string;
    // Set when the member is assigned a custom role (id into /organization/roles).
    role_id?: string;
    // Full assigned role set (a member can hold several).
    roles?: MemberRole[];
    permissions?: number;
    joined_at?: Date;
    // Which resources the roles apply to; "restricted" reaches only `access`'s grants.
    access_scope?: AccessScope;
    access?: MemberAccess;
}
