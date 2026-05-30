import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import { createContext, useContext } from "react";

export const CampaignContext = createContext<Campaign>({} as Campaign);

export function useCampaign() {
    return useContext(CampaignContext);
}
