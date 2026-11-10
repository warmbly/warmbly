// embedCode builds the website embed for a connect link or a report link.

export type Kind = "connect" | "report";

// parseLink accepts a full link or a bare token, and reads the kind from the path.
export function parseLink(raw: string): { kind: Kind; token: string } | null {
    const s = raw.trim();
    if (!s) return null;
    const fromPath = (path: string) => {
        const m = path.match(/\/(connect|r)\/([A-Za-z0-9_-]{16,128})\/?$/);
        return m ? { kind: (m[1] === "r" ? "report" : "connect") as Kind, token: m[2] } : null;
    };
    try {
        return fromPath(new URL(s).pathname);
    } catch {
        return fromPath(s.startsWith("/") ? s : `/${s}`);
    }
}

export function snippets(base: string, kind: Kind, token: string) {
    const path = kind === "report" ? "/r/" : "/connect/";
    const attr = kind === "report" ? "data-warmbly-report" : "data-warmbly-connect";
    const title = kind === "report" ? "Report" : "Connect your mailbox";
    return {
        script: `<script src="${base}/embed.js" ${attr}="${token}" async></script>`,
        iframe: `<iframe src="${base}${path}${token}?embed=1" title="${title}" style="width:100%;min-height:${kind === "report" ? 720 : 560}px;border:0" loading="lazy"></iframe>`,
    };
}
