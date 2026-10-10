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
    if (invitationConsumed) {
        const path = new URL(target, "https://warmbly.invalid").pathname.replace(/\/+$/, "");
        if (path === "/invite") return "/app/emails";
    }
    return target;
}
