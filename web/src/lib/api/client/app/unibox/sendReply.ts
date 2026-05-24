import Request from "../../Request";

export interface SendUniboxReplyRequest {
    email_account_id: string
    to: string[]
    cc?: string[]
    bcc?: string[]
    subject: string
    body_html?: string
    body_plain?: string
    in_reply_to?: string[]
    thread_id?: string
    send_mode?: "instant" | "smart"
}

export interface SendUniboxReplyResponse {
    task_id: string
    scheduled_at: Date
    send_mode: string
}

export default async function sendReply(data: SendUniboxReplyRequest): Promise<SendUniboxReplyResponse> {
    return await Request<SendUniboxReplyResponse>({
        method: "POST",
        url: "/unibox/reply",
        data,
        authorization: true,
    })
}
