import { useMutation } from "@tanstack/react-query";
import edit from "@/lib/api/client/app/generation/edit";
import type { EditRequest } from "@/lib/api/models/app/generation/Write";

// On-demand AI selection edit. A mutation run from the floating edit toolbar;
// a 402 (out of credits) rejects with an AppError carrying status 402.
export default function useGenerateEdit() {
    return useMutation({
        mutationFn: (body: EditRequest) => edit(body),
    });
}
