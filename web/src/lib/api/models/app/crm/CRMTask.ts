export default interface CRMTask {
    id: string
    title: string
    description?: string
    due_date?: Date
    completed: boolean
    deal_id?: string
    contact_id?: string
    assigned_to?: string
    priority: 'low' | 'medium' | 'high'
    created_at: Date
    updated_at: Date
}
