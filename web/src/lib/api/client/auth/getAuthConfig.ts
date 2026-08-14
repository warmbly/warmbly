import type AuthConfig from "../../models/auth/AuthConfig";
import Request from "../Request";

export default async function getAuthConfig(): Promise<AuthConfig> {
    return await Request<AuthConfig>({
        method: "GET",
        url: "/auth/config",
    });
}
