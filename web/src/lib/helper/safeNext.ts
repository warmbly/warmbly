// A post-sign-in ?next= is followed only when it is a path on this origin: one
// leading "/" not followed by another, and no backslash or control character.
export default function safeNext(next: string | null | undefined, fallback: string): string {
    if (!next || next[0] !== "/" || next[1] === "/") return fallback;
    // eslint-disable-next-line no-control-regex
    if (/[\u0000-\u001f\u007f\\]/.test(next)) return fallback;
    return next;
}

export function postAuthNext(next: string | null | undefined, invitationConsumed = false): string {
    const target = safeNext(next, "/app/emails");
    const url = new URL(target, "https://warmbly.invalid");
    const path = url.pathname.replace(/\/+$/, "");
    if (invitationConsumed && path === "/invite") return "/app/emails";
    // Onboarding forwards to its own next, so go there; boot returns an unfinished account to onboarding with it.
    if (path === "/onboarding") return postAuthNext(url.searchParams.get("next"), invitationConsumed);
    return target;
}

// A provider sign-in leaves the page, so ?next= waits in this tab until the callback takes it.
const SSO_NEXT_KEY = "wb.sso.next";

export function rememberSSONext(next: string | null | undefined): void {
    try {
        if (next) sessionStorage.setItem(SSO_NEXT_KEY, next);
        else sessionStorage.removeItem(SSO_NEXT_KEY);
    } catch {
        /* storage blocked: the sign-in still lands on the default page */
    }
}

export function takeSSONext(): string | undefined {
    try {
        const next = sessionStorage.getItem(SSO_NEXT_KEY) ?? undefined;
        sessionStorage.removeItem(SSO_NEXT_KEY);
        return next;
    } catch {
        return undefined;
    }
}
