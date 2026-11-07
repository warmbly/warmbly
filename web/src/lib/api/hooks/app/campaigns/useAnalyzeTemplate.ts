import { useMutation, useQueryClient } from "@tanstack/react-query";
import analyzeTemplate from "@/lib/api/client/app/campaigns/analyzeTemplate";
import type { ScoreTemplateRequest } from "@/lib/api/models/app/campaigns/TemplateScore";

// On-demand AI spam analysis. A mutation, not a query: it is charged to the
// balance and only ever runs when the writer asks for it (Analyze, then
// Re-check after an edit). Every success refreshes the balance views so the
// header meter moves immediately.
export default function useAnalyzeTemplate() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (body: ScoreTemplateRequest) => analyzeTemplate(body),
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
        },
    });
}
