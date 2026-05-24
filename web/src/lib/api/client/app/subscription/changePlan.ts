import Request from "../../Request";

export default async function changePlan(data: { plan_id: string }): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: `/subscription/change-plan`,
        data,
        authorization: true,
    })
}
