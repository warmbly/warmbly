export interface Stage {
    id: string;
    pipeline_id: string;
    name: string;
    color: string;
    position: number;
    deal_count?: number;
    created_at: string;
    updated_at: string;
}

export default interface Pipeline {
    id: string;
    organization_id: string;
    name: string;
    position: number;
    stages: Stage[];
    created_at: string;
    updated_at: string;
}
