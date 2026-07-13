import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
    listSkills,
    createSkill,
    updateSkill,
    deleteSkill,
} from "@/lib/api/client/app/skills/skills";
import type {
    CreateAISkill,
    UpdateAISkill,
} from "@/lib/api/models/app/skills/Skill";

// Org AI skills. Refreshed by the ai_skill spine entry on any mutation.
export function useSkills() {
    return useQuery({
        queryKey: ["ai", "skills"],
        queryFn: () => listSkills(),
        staleTime: 30_000,
    });
}

export function useCreateSkill() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (data: CreateAISkill) => createSkill(data),
        onSuccess: () => qc.invalidateQueries({ queryKey: ["ai", "skills"] }),
    });
}

export function useUpdateSkill() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (input: { id: string; data: UpdateAISkill }) =>
            updateSkill(input.id, input.data),
        onSuccess: () => qc.invalidateQueries({ queryKey: ["ai", "skills"] }),
    });
}

export function useDeleteSkill() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => deleteSkill(id),
        onSuccess: () => qc.invalidateQueries({ queryKey: ["ai", "skills"] }),
    });
}
