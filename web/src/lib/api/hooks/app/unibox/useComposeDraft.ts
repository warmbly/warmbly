import { useMutation, useQueryClient } from "@tanstack/react-query";
import composeDraft, {
    type ComposeDraftInput,
} from "@/lib/api/client/app/unibox/composeDraft";

// Drafts a grounded compose email (contact + history + voice profile). May
// return a clarifying question instead of a draft; either way the credit
// charge is server-side, so refresh the credits views on success.
export default function useComposeDraft() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (data: ComposeDraftInput) => composeDraft(data),
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
        },
    });
}
