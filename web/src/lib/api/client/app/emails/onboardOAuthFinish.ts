import Request from "../../Request";
import type Inbox from "@/lib/api/models/app/emails/Inbox";

export default async function onboardOAuthFinish(code: string, state: string): Promise<Inbox> {
    return await Request<Inbox>({
        method: "POST",
        url: `/emails/onboarding/oauth/finish`,
        data: { code, state },
        authorization: true,
    });
}
