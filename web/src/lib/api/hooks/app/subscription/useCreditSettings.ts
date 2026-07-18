import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import getCreditSettings from "@/lib/api/client/app/subscription/getCreditSettings";
import updateCreditSettings, { type UpdateCreditSettingsBody } from "@/lib/api/client/app/subscription/updateCreditSettings";

export function useCreditSettings() {
    return useQuery({
        queryKey: ["subscription", "credits", "settings"],
        queryFn: getCreditSettings,
        staleTime: 60_000,
    });
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
