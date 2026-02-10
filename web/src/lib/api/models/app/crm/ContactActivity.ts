export default interface ContactActivity {
    id: string
    contact_id: string
    type: string
    description: string
    metadata?: Record<string, unknown>
    created_at: Date
}
