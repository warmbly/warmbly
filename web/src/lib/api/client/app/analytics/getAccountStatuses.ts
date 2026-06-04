import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";
import Request from "../../Request";

export default async function getAccountStatuses(): Promise<AccountStatus[]> {
    // The backend wraps list responses in a `{ data: [...] }` envelope, and the
    // shared Request helper returns the raw body (it does not unwrap). Mirror the
    // convention used by the other list clients (getMyInvitations, listTemplates)
    // so callers always receive a real array — otherwise `for…of` over the
    // envelope object throws "{} is not iterable" on the accounts page.
    const res = await Request<{ data: AccountStatus[] | null } | AccountStatus[]>({
        method: "GET",
        url: `/analytics/accounts`,
        authorization: true,
    })
    if (Array.isArray(res)) return res
    return res?.data ?? []
}
