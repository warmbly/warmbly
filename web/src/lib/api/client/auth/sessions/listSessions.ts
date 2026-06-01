import Request from "../../Request";
import type ActiveSession from "@/lib/api/models/auth/ActiveSession";

export default async function listSessions(): Promise<ActiveSession[]> {
    return await Request<ActiveSession[]>({
        method: "GET",
        url: "/auth/sessions",
        authorization: true,
    });
}
