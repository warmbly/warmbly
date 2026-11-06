import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import getCreditSettings from "@/lib/api/client/app/subscription/getCreditSettings";
import updateCreditSettings, { type UpdateCreditSettingsBody } from "@/lib/api/client/app/subscription/updateCreditSettings";

export const creditSettingsQuery = queryOptions({
    queryKey: ["subscription", "credits", "settings"],
    queryFn: getCreditSettings,
    staleTime: 60_000,
});

export function useCreditSettings() {
    return useQuery(creditSettingsQuery);
}

export function useUpdateCreditSettings() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (body: UpdateCreditSettingsBody) => updateCreditSettings(body),
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: ["subscription", "credits"] });
        },
    });
}
