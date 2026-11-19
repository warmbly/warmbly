import Request from "../../Request";
import type PartnerMailbox from "@/lib/api/models/app/emails/PartnerMailbox";

export default async function getPartnerMailboxes(): Promise<{ data: PartnerMailbox[] }> {
    return await Request<{ data: PartnerMailbox[] }>({
        method: "GET",
        url: `/emails/partner`,
        authorization: true,
    });
}
