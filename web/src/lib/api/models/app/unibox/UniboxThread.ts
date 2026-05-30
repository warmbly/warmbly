// Thread response from GET /unibox/thread. The server returns
// MailSearchResult: a preview array (id, email_id, thread_id,
// from_addr, to_addr, subject, snippet, internal_date, seen) plus
// cursor pagination. No body column exists yet — snippet is the
// renderable content per message.

export interface UniboxThreadMessage {
    id: string
    email_id: string
    thread_id: string
    from_addr: string[]
    to_addr: string[]
    subject: string
    snippet: string
    internal_date: string
    seen: boolean
}

export default interface UniboxThread {
    data: UniboxThreadMessage[]
    pagination: {
        has_more: boolean
        next_cursor: string | null
    }
}
