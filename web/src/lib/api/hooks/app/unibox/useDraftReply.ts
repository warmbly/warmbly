import { useMutation } from "@tanstack/react-query";
import draftReply, {
    type DraftReplyInput,
} from "@/lib/api/client/app/unibox/draftReply";

// Drafts a context-grounded reply. The credit charge is server-side; the caller
// fills the composer with the returned draft, and the human sends.
export default function useDraftReply() {
    return useMutation({
        mutationFn: (data: DraftReplyInput) => draftReply(data),
    });
}
