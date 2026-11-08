// EmbedSnippets — turns a connect link or a report link into code for the
// agency's own website: the auto-resizing script embed and a plain iframe.
// The link's token is the whole authorisation, so whoever can see the page it
// is embedded on can open it; the copy says so.

import React from "react";
import { CopyIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { Label, TextInput } from "@/components/ui/field";
import { parseLink, snippets } from "./embedCode";

function Snippet({ label, code }: { label: string; code: string }) {
    async function copy() {
        await navigator.clipboard.writeText(code);
        toast.success(`${label} copied`);
    }
    return (
        <div>
            <div className="flex items-center justify-between mb-1">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{label}</span>
                <button
                    type="button"
                    onClick={() => void copy()}
                    className="h-6 px-2 rounded-md border border-slate-200 text-[11px] text-slate-600 hover:bg-slate-50 inline-flex items-center gap-1"
                >
                    <CopyIcon className="w-3 h-3" />
                    Copy
                </button>
            </div>
            <pre className="rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-[11.5px] font-mono text-slate-700 whitespace-pre-wrap break-all">
                {code}
            </pre>
        </div>
    );
}

export default function EmbedSnippets({ baseUrl }: { baseUrl: string }) {
    const [link, setLink] = React.useState("");
    const parsed = parseLink(link);
    const base = baseUrl.replace(/\/+$/, "");
    const out = parsed && base ? snippets(base, parsed.kind, parsed.token) : null;

    return (
        <div className="space-y-3">
            <div className="max-w-[520px]">
                <Label>Connect link or report link</Label>
                <TextInput
                    value={link}
                    onChange={setLink}
                    placeholder={`${base || "https://portal.example.com"}/connect/…`}
                    invalid={!!link.trim() && !parsed}
                    className="w-full font-mono"
                />
                <p className="mt-1 text-[11px] text-slate-500">
                    Paste a link you created from a mailbox's "Invite client to connect" or a campaign's "Share report". Links are
                    shown once, so keep the one you want to embed.
                </p>
            </div>
            {!base && (
                <p className="text-[11.5px] text-slate-500">
                    This install has no portal address configured, so there is nowhere to serve embeds from. An operator needs to set
                    PORTAL_DOMAIN.
                </p>
            )}
            {out && parsed && (
                <div className="space-y-3">
                    <Snippet label="Script embed (resizes to fit)" code={out.script} />
                    <Snippet label="Plain iframe" code={out.iframe} />
                    <p className="text-[11.5px] text-amber-700">
                        {parsed.kind === "report"
                            ? "Anyone who can see the page you embed this on can read the report. Revoke the link to take it down."
                            : "Anyone who can see the page you embed this on can connect a mailbox into this workspace while the link is live."}
                    </p>
                </div>
            )}
        </div>
    );
}
