import type UniboxEmail from './UniboxEmail'

export default interface UniboxThread {
    thread_id: string
    subject: string
    participants: string[]
    last_message_at: Date
    unseen_count: number
    messages: UniboxEmail[]
}
