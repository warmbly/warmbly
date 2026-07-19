import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
    deleteComposeDraft,
    listComposeDrafts,
    saveComposeDraft,
    type ComposeDraftSaveInput,
} from "@/lib/api/client/app/unibox/composeDrafts";

const DRAFTS_KEY = ["unibox", "compose", "drafts"] as const;

export default function useComposeDrafts(enabled = true) {
    return useQuery({
        queryKey: DRAFTS_KEY,
        queryFn: listComposeDrafts,
        staleTime: 15_000,
        enabled,
    });
}

// Debounced autosave target. The id is client-generated, so retries and
// repeated saves of the same draft are idempotent.
export function useSaveComposeDraft() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, data }: { id: string; data: ComposeDraftSaveInput }) =>
            saveComposeDraft(id, data),
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: DRAFTS_KEY });
        },
    });
}

export function useDeleteComposeDraft() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => deleteComposeDraft(id),
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: DRAFTS_KEY });
        },
    });
}
