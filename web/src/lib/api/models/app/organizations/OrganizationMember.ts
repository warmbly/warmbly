export default interface OrganizationMember {
    id: string
    user_id: string
    email: string
    role: 'owner' | 'admin' | 'member'
    joined_at: Date
}
