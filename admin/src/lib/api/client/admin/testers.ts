// /admin/testers/* — accounts handed to somebody outside the team.
//
// A tester is an ordinary account with its own workspace, marked exempt from
// the emailed login code because the holder cannot read this instance's mail.
// The exemption is what makes it findable, so listing testers and listing
// exemptions are the same query.

import { Request } from "@/lib/api/client";
import type { LoginCodeExemption, CreatedTester } from "@/lib/api/models/admin";

export function listTesters(): Promise<{ data: LoginCodeExemption[] }> {
    return Request({ method: "GET", url: "/admin/testers", authorization: true });
}

export function createTester(body: {
    email: string;
    org_name?: string;
    reason: string;
}): Promise<CreatedTester> {
    return Request({ method: "POST", url: "/admin/testers", authorization: true, data: body });
}

export function revokeTester(id: string): Promise<{ revoked: boolean }> {
    return Request({ method: "DELETE", url: `/admin/testers/${id}`, authorization: true });
}
