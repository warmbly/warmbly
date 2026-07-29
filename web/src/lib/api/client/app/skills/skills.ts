import type {
    AISkill,
    CreateAISkill,
    UpdateAISkill,
} from "@/lib/api/models/app/skills/Skill";
import Request from "../../Request";

export async function listSkills(): Promise<{ data: AISkill[] }> {
    return await Request<{ data: AISkill[] }>({
        method: "GET",
        url: `/ai/skills`,
        authorization: true,
    });
}

export async function createSkill(data: CreateAISkill): Promise<AISkill> {
    return await Request<AISkill>({
        method: "POST",
        url: `/ai/skills`,
        data,
        authorization: true,
    });
}

export async function updateSkill(id: string, data: UpdateAISkill): Promise<AISkill> {
    return await Request<AISkill>({
        method: "PATCH",
        url: `/ai/skills/${id}`,
        data,
        authorization: true,
    });
}

export async function deleteSkill(id: string): Promise<{ deleted: boolean }> {
    return await Request<{ deleted: boolean }>({
        method: "DELETE",
        url: `/ai/skills/${id}`,
        authorization: true,
    });
}
