// Mirror of internal/models/organization.go's OrganizationMember.
// `role` and `permissions` come from the server; `permissions` is a
// uint16 bitmask matching internal/models/organization_permission.go.

export type OrganizationRole = "owner" | "admin" | "manager" | "member" | "viewer";

export default interface OrganizationMember {
    id: string;
    user_id: string;
    email: string;
    role: OrganizationRole;
    permissions?: number;
    joined_at: Date;
}
