// Mirror of internal/models.UniboxScheduledItem — one queued
// outbound message the user can review or cancel.

export default interface UniboxScheduledItem {
    task_id: string
    scheduled_at: string
    created_at: string

    account_id: string
    account_email: string
    account_name: string

    to: string[]
    cc?: string[]
    bcc?: string[]
    subject: string
    snippet: string

    thread_id?: string
}

export interface UniboxScheduledResult {
    data: UniboxScheduledItem[]
}
