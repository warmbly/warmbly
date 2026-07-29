import { useMutation, useQueryClient } from "@tanstack/react-query";
import generateAIVariable from "@/lib/api/client/app/generation/aiVariable";
import type { AIVariableGenerateRequest } from "@/lib/api/models/app/generation/AIVariable";

// Preview generation for an AI variable. A mutation (run from the config
// popover's Preview button); every success refreshes the credits views so the
// header meter moves. A 402 rejects with an AppError carrying status 402.
export default function useGenerateAIVariable() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (body: AIVariableGenerateRequest) => generateAIVariable(body),
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
        },
    });
}
