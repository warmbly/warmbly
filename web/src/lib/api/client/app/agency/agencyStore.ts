import Request from "../../Request";
import type { AgencyStoreOverview } from "@/lib/api/models/app/agency/AgencyStore";

export const getAgencyStore = (agencyId: string, signal?: AbortSignal) =>
    Request<AgencyStoreOverview>({ method: "GET", url: `/agency/${agencyId}/store`, authorization: true, signal });
