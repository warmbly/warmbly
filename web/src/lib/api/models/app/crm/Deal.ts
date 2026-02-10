export default interface Deal {
    id: string
    title: string
    value?: number
    currency?: string
    pipeline_id: string
    stage_id: string
    contact_id?: string
    contact_email?: string
    status: 'open' | 'won' | 'lost'
    expected_close_date?: Date
    notes?: string
    created_at: Date
    updated_at: Date
}
