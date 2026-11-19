import type Token from "../../models/auth/Token";
import Request from "../Request";

export interface PartnerLogin extends Token {
    /** A path inside the app, or a URL on one of the partner's own hosts; empty for the dashboard. */
    return_to: string;
}

/** Spends a reseller's one-time login link for a session in the workspace it names. */
export default async function exchangePartnerLogin(code: string): Promise<PartnerLogin> {
    return await Request<PartnerLogin>({ method: "POST", url: "/auth/partner/exchange", data: { code } });
}
