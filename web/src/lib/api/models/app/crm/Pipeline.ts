export default interface Pipeline {
    id: string
    name: string
    description?: string
    stages: Stage[]
    created_at: Date
    updated_at: Date
}

export interface Stage {
    id: string
    name: string
    position: number
    color?: string
}
