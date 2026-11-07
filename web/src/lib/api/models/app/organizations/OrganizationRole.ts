// Custom workspace role: an org-scoped named permission set. Editing one
// propagates to every member assigned to it (server-side write-through).
export default interface OrganizationRole {
    id: string;
    organization_id: string;
    name: string;
    description: string;
    color: string;
    permissions: number;
    // Lets a restricted member holding the role act inside their grants, not only read.
    acts_in_scope?: boolean;
    member_count: number;
    created_at: Date;
    updated_at: Date;
}
