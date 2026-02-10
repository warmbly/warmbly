export default interface UniboxEmail {
    id: string
    from: string
    to: string
    subject: string
    body: string
    date: Date
    is_seen: boolean
    thread_id?: string
    account_id: string
}
