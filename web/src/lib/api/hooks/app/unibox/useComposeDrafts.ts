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
//
// Invalidation lives inside mutationFn, not onSuccess: closing the composer
// fires the save and unmounts in the same tick, and observer callbacks
// (useMutation's onSuccess) are skipped once the owning component is gone,
// leaving the rail's Drafts list stale until a reload.
export function useSaveComposeDraft() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async ({ id, data }: { id: string; data: ComposeDraftSaveInput }) => {
            const res = await saveComposeDraft(id, data);
            void qc.invalidateQueries({ queryKey: DRAFTS_KEY });
            return res;
        },
    });
}

export function useDeleteComposeDraft() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: async (id: string) => {
            const res = await deleteComposeDraft(id);
            void qc.invalidateQueries({ queryKey: DRAFTS_KEY });
            return res;
        },
    });
}
