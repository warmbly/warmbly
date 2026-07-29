import { useMutation, useQueryClient } from "@tanstack/react-query";
import edit from "@/lib/api/client/app/generation/edit";
import type { EditRequest } from "@/lib/api/models/app/generation/Write";

// On-demand AI selection edit. A mutation run from the floating edit toolbar;
// a 402 (out of credits) rejects with an AppError carrying status 402. Every
// success refreshes the credits views so the header meter moves immediately.
export default function useGenerateEdit() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (body: EditRequest) => edit(body),
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
        },
    });
}
