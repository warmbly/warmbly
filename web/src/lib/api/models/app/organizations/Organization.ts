export default interface Organization {
    id: string
    name: string
    avatar?: string
    plan?: string
    role: 'owner' | 'admin' | 'member'
    created_at: Date
}
