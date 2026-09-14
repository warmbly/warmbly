import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
    pauseCampaignLead,
    resumeCampaignLead,
    type LeadHoldResult,
} from "@/lib/api/client/app/campaigns/leadHold";

// Pausing or resuming one lead changes the Leads list (its status and the
// scope-chip counts) and the contact drawer's campaign panel. One invalidation
// covers both: react-query matches by key PREFIX, so ["contacts"] already
// includes ["contacts", id]. The server also audits the change, so teammates
// get it over the audit spine; this is the local echo.
function invalidate(queryClient: ReturnType<typeof useQueryClient>) {
    void queryClient.invalidateQueries({ queryKey: ["contacts"] });
}

export function usePauseLead() {
    const queryClient = useQueryClient();
    return useMutation<LeadHoldResult, unknown, { campaignId: string; contactId: string; until?: string | null; reason?: string }>({
        mutationFn: ({ campaignId, contactId, until, reason }) =>
            pauseCampaignLead(campaignId, contactId, { until, reason }),
        onSuccess: () => invalidate(queryClient),
    });
}

export function useResumeLead() {
    const queryClient = useQueryClient();
    return useMutation<LeadHoldResult, unknown, { campaignId: string; contactId: string }>({
        mutationFn: ({ campaignId, contactId }) => resumeCampaignLead(campaignId, contactId),
        onSuccess: () => invalidate(queryClient),
    });
}
