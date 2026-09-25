import React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { usePermission } from "@/hooks/usePermission";
import useContactByEmail from "@/lib/api/hooks/app/contacts/useContactByEmail";
import useContactCampaignStates from "@/lib/api/hooks/app/contacts/useContactCampaignStates";
import { pauseCampaignLead } from "@/lib/api/client/app/campaigns/leadHold";
import listContactCampaignStates from "@/lib/api/client/app/contacts/listContactCampaignStates";
import type ContactCampaignState from "@/lib/api/models/app/contacts/ContactCampaignState";
import { leadCanBePaused } from "@/lib/leadHold";

export interface FollowUpTargets {
    contactId: string;
    campaigns: ContactCampaignState[];
}

// A reply recipient's campaigns with a step still to send, and a pause for them; empty without MANAGE_CAMPAIGNS.
export default function usePauseFollowUps(email: string | undefined) {
    const allowed = usePermission("MANAGE_CAMPAIGNS");
    const queryClient = useQueryClient();
    const lookup = useContactByEmail(email, allowed);
    const contactId = lookup.data?.id ?? "";
    const statesQ = useContactCampaignStates(contactId, allowed);

    const targets = React.useMemo<FollowUpTargets>(
        () => ({
            contactId,
            campaigns: allowed && contactId ? (statesQ.data?.data ?? []).filter(leadCanBePaused) : [],
        }),
        [allowed, contactId, statesQ.data],
    );

    const pauseAll = React.useCallback(
        async (t: FollowUpTargets, until: string | null) => {
            // A manual pause replaces any hold, so re-read (the composer may be gone) and skip the held.
            const fresh = await queryClient
                .fetchQuery({
                    queryKey: ["contacts", t.contactId, "campaign-state"],
                    queryFn: () => listContactCampaignStates(t.contactId),
                    staleTime: 0,
                })
                .then((r) => r.data)
                .catch(() => undefined);
            if (!fresh) return { paused: 0, failed: t.campaigns.length, held: 0 };
            const now = new Map(fresh.map((c) => [c.campaign_id, c]));
            const campaigns = t.campaigns.filter((c) => {
                const cur = now.get(c.campaign_id);
                return !!cur && leadCanBePaused(cur);
            });
            const held = t.campaigns.filter((c) => !!now.get(c.campaign_id)?.hold).length;
            const results = await Promise.allSettled(
                campaigns.map((c) => pauseCampaignLead(c.campaign_id, t.contactId, { until })),
            );
            if (campaigns.length > 0) void queryClient.invalidateQueries({ queryKey: ["contacts"] });
            const paused = results.filter((r) => r.status === "fulfilled").length;
            // Anything neither paused nor already held is reported as not paused.
            return { paused, failed: t.campaigns.length - paused - held, held };
        },
        [queryClient],
    );

    return { targets, pauseAll };
}
