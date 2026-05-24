export default interface Invitation {
    id: string
    organization_id: string
    organization_name: string
    email: string
    role: 'admin' | 'member'
    invited_by: string
    created_at: Date
    expires_at: Date
}
