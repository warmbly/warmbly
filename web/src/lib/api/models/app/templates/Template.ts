export default interface Template {
    id: string
    name: string
    subject: string
    body: string
    variables?: string[]
    created_at: Date
    updated_at: Date
}
