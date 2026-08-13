import type { DailyPlanView } from "@/lib/api/models/app/emails/SendingBehavior";
import Request from "../../Request";

export default async function getSendingPlan(id: string): Promise<DailyPlanView> {
    return await Request<DailyPlanView>({
        method: "GET",
        url: `/emails/${id}/behavior/plan`,
        authorization: true,
    })
}
