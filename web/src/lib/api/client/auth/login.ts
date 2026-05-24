import type Login from "../../models/auth/Login";
import type Session from "@/lib/api/models/auth/Session";
import Request from "../Request";

export default async function login(data: Login): Promise<Session> {
    return await Request<Session>({
        method: "POST",
        url: "/auth/login",
        data,
    });
}
