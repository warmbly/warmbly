import { useQuery } from "@tanstack/react-query";
import getCampaignLeadTimezones from "@/lib/api/client/app/campaigns/getCampaignLeadTimezones";

// Keyed under the campaign so the leads and contacts invalidations that
// already refresh the campaign reach it too.
export default function useCampaignLeadTimezones(id: string) {
    return useQuery({
        queryKey: ["campaigns", id, "lead-timezones"],
        queryFn: () => getCampaignLeadTimezones(id),
        enabled: !!id,
    });
}
