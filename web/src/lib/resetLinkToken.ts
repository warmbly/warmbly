// The emailed reset link carries its token in the fragment, which never reaches a server; nothing else is read.
export function resetLinkToken(hash: string): string {
    return new URLSearchParams(hash.replace(/^#/, "")).get("session") ?? "";
}
