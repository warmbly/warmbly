import Request from "../../Request";
import type GetSocket from "@/lib/api/models/app/socket/GetSocket";
import { API_BASE_URL } from "@/lib/information";

export function socketURL(ticket: GetSocket, base = API_BASE_URL): string {
    if (!ticket.proxy_path) return ticket.url;
    const api = new URL(base.replace(/\/$/, "") + "/", window.location.origin);
    const url = new URL(ticket.proxy_path.replace(/^\//, ""), api);
    if (url.origin !== api.origin || !["http:", "https:"].includes(url.protocol)) {
        throw new Error("Invalid realtime proxy URL");
    }
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    return url.toString();
}

export default async function getSocket(useProxy = true): Promise<GetSocket> {
    const ticket = await Request<GetSocket>({
        method: "POST",
        url: `/getaway`,
        authorization: true,
    });
    return { ...ticket, url: useProxy ? socketURL(ticket) : ticket.url };
}
