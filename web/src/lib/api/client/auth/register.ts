import type Register from "@/lib/api/models/auth/Register";
import type Session from "@/lib/api/models/auth/Session";
import Request from "../Request";

export default async function register(data: Register): Promise<Session> {
    return await Request<Session>({
        method: "POST",
        url: "/auth/register",
        data,
    })
}
