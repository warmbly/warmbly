import type TrialStatus from "@/lib/api/models/app/subscription/TrialStatus";
import Request from "../../Request";

export default async function getTrialStatus(): Promise<TrialStatus> {
    return await Request<TrialStatus>({
        method: "GET",
        url: `/subscription/trial`,
        authorization: true,
    })
}
