import { useMutation, useQueryClient } from "@tanstack/react-query";
import draftReply, {
    type DraftReplyInput,
} from "@/lib/api/client/app/unibox/draftReply";

// Drafts a context-grounded reply. The credit charge is server-side; the caller
// fills the composer with the returned draft, and the human sends. Every
// success refreshes the credits views so the header meter moves immediately.
export default function useDraftReply() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (data: DraftReplyInput) => draftReply(data),
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
        },
    });
}
