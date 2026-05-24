import type Order from "@/lib/api/models/app/Order";
import Request from "../../Request";

export default async function moveFolder(id: string, position: number): Promise<Order[]> {
    return await Request<Order[]>({
        method: "PATCH",
        url: `/folders/${id}/move`,
        data: {
            position,
        },
        authorization: true,
    })
}
