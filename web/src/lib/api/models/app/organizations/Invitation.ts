import type MemberAccess from "./MemberAccess";

export default interface Invitation {
    id: string
    organization_id: string
    organization_name: string
    email: string
    role: string;
    // Workspace role row this invitation lands in.
    role_id?: string
    roles?: { id: string; name: string; color: string }[]
    invited_by: string
    created_at: Date
    expires_at: Date
    access?: MemberAccess
}
