export default interface APIKey {
    id: string
    name: string
    key_prefix: string
    permissions: string[]
    last_used_at?: Date
    expires_at?: Date
    created_at: Date
}
