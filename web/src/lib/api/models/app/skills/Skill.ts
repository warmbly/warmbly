export interface AISkill {
    id: string;
    org_id: string;
    name: string;
    description: string;
    content: string;
    enabled: boolean;
    created_at: string;
    updated_at: string;
}

export interface CreateAISkill {
    name: string;
    description?: string;
    content?: string;
    enabled?: boolean;
}

export interface UpdateAISkill {
    name?: string;
    description?: string;
    content?: string;
    enabled?: boolean;
}
