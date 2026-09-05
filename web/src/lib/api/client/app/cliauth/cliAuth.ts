// /auth/cli/* — a signed-in member reviews the code a CLI is showing and
// authorizes it into one of their workspaces, which mints the API key.

import Request from "@/lib/api/client/Request";
import type { CLIAuthCode } from "@/lib/api/models/app/cliauth/CLIAuth";

export async function describeCLIAuthCode(code: string): Promise<CLIAuthCode> {
    return await Request<CLIAuthCode>({ method: "GET", url: `/auth/cli/codes/${encodeURIComponent(code)}`, authorization: true });
}

export async function approveCLIAuthCode(code: string, organizationId: string): Promise<CLIAuthCode> {
    return await Request<CLIAuthCode>({
        method: "POST",
        url: `/auth/cli/codes/${encodeURIComponent(code)}/approve`,
        data: { organization_id: organizationId },
        authorization: true,
    });
}

export async function denyCLIAuthCode(code: string): Promise<void> {
    await Request<void>({ method: "POST", url: `/auth/cli/codes/${encodeURIComponent(code)}/deny`, authorization: true });
}
