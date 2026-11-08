import Request from "../../Request";
import type LeadTimezones from "@/lib/api/models/app/campaigns/LeadTimezones";

export default async function getCampaignLeadTimezones(id: string): Promise<LeadTimezones> {
    return Request<LeadTimezones>({
        method: "GET",
        url: `/campaigns/${id}/lead-timezones`,
        authorization: true,
    });
}
