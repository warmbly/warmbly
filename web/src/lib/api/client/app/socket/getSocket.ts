import Request from "../../Request";
import type GetSocket from "@/lib/api/models/app/socket/GetSocket";

export default async function getSocket(): Promise<GetSocket> {
    return await Request<GetSocket>({
        method: "POST",
        url: `/getaway`,
        authorization: true,
    })
}
