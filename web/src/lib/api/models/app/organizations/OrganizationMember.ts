// Mirror of internal/models/organization.go's OrganizationMember.
// `role` and `permissions` come from the server; `permissions` is a
// uint16 bitmask matching internal/models/organization_permission.go.

export type OrganizationRole = "owner" | "admin" | "manager" | "member" | "viewer";

export default interface OrganizationMember {
    id: string;
    user_id: string;
    // Flattened from the joined user by the API (GET /organization/members).
    // Optional + possibly-empty: always guard before calling string methods.
    email?: string;
    name?: string;
    role: OrganizationRole;
    permissions?: number;
    joined_at?: Date;
}
