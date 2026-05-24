import Request from "../../Request";

export interface OAuthStartResponse {
    url: string;
    state: string;
}

export default async function onboardOAuthStart(provider: "gmail" | "outlook"): Promise<OAuthStartResponse> {
    return await Request<OAuthStartResponse>({
        method: "POST",
        url: `/emails/onboarding/oauth/start`,
        data: { provider },
        authorization: true,
    });
}
