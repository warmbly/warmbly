import type Plan from "@/lib/api/models/app/subscription/Plan";
import Request from "../../Request";

export default async function listPlans(): Promise<Plan[]> {
    return await Request<Plan[]>({
        method: "GET",
        url: `/plans`,
        authorization: true,
    })
}
