// Custom workspace role: an org-scoped named permission set. Editing one
// propagates to every member assigned to it (server-side write-through).
export default interface OrganizationRole {
    id: string;
    organization_id: string;
    name: string;
    description: string;
    color: string;
    permissions: number;
    member_count: number;
    created_at: string;
    updated_at: string;
}
