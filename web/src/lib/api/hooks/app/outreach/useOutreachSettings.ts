import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getOutreachSettings, updateOutreachSettings } from "@/lib/api/client/app/outreach/outreach";
import type { OutreachSettings } from "@/lib/api/models/app/outreach/OutreachSettings";

export const OUTREACH_SETTINGS_KEY = ["app", "outreach", "settings"];

export function useOutreachSettings() {
    return useQuery({
        queryKey: OUTREACH_SETTINGS_KEY,
        queryFn: getOutreachSettings,
    });
}

export function useUpdateOutreachSettings() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (s: OutreachSettings) => updateOutreachSettings(s),
        onSuccess: (_, s) => {
            // The endpoint returns no body; seed the cache with what we sent so
            // the page does not flash back to the pre-edit value before refetch.
            qc.setQueryData(OUTREACH_SETTINGS_KEY, s);
        },
    });
}
