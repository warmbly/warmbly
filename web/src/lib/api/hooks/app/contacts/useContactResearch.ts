import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
    listContactResearch,
    researchContact,
    batchResearch,
} from "@/lib/api/client/app/contacts/research";

// A contact's research history. Refreshed live by the AI_RESEARCH_PROGRESS
// realtime event (see useRealtimeEvents).
export function useContactResearch(contactId: string | undefined) {
    return useQuery({
        queryKey: ["contacts", contactId, "research"],
        queryFn: () => listContactResearch(contactId as string),
        enabled: !!contactId,
        staleTime: 30_000,
    });
}

export function useRunContactResearch(contactId: string | undefined) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (objective: string) => researchContact(contactId as string, objective),
        onSuccess: () => {
            if (contactId)
                qc.invalidateQueries({ queryKey: ["contacts", contactId, "research"] });
        },
    });
}

export function useBatchResearch() {
    return useMutation({
        mutationFn: (input: { contactIds: string[]; objective: string }) =>
            batchResearch(input.contactIds, input.objective),
    });
}
