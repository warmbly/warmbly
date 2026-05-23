// API keys — dev-focused preview.
//
// Instead of an empty page, show a code snippet using the API + a
// permissions matrix so the user understands what an API key gives
// them. Different shape from every other tab — code monospace
// dominates the visual.

import { CopyIcon, KeyIcon, PlusIcon, ShieldCheckIcon } from "lucide-react";
import toast from "react-hot-toast";
import {
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    Stat,
    StatStrip,
    TopbarAction,
} from "@/components/layout/Page";
import { comingSoon } from "@/lib/helper/comingSoon";

const SAMPLE_PERMISSIONS = [
    { scope: "campaigns:read", description: "List + read campaign data", granted: true },
    { scope: "campaigns:write", description: "Create / update / start / stop campaigns", granted: true },
    { scope: "contacts:read", description: "Search and read contact records", granted: true },
    { scope: "contacts:write", description: "Add, edit, delete contacts and tags", granted: false },
    { scope: "emails:send", description: "Trigger sends from a connected mailbox", granted: false },
    { scope: "analytics:read", description: "Aggregated stats and deliverability data", granted: true },
];

const SAMPLE_KEYS = [
    { label: "production · server", prefix: "wmbly_pk_live_••••••••••••", lastUsed: "3m ago" },
    { label: "staging · ci", prefix: "wmbly_pk_test_••••••••••••", lastUsed: "yesterday" },
];

const CODE_SNIPPET = `curl https://api.warmbly.com/v1/campaigns \\
  -H "Authorization: Bearer wmbly_pk_live_••••••••" \\
  -H "Content-Type: application/json"`;

export default function APIKeysPage() {
    function copyCode() {
        navigator.clipboard.writeText(CODE_SNIPPET).catch(() => undefined);
        toast.success("Snippet copied");
    }

    return (
        <Page>
            <PageTopbar
                eyebrow="API keys"
                subtitle="Programmatic access tokens · billed against your workspace"
            >
                <TopbarAction
                    icon={<PlusIcon className="w-3 h-3" />}
                    onClick={() => comingSoon("API keys")}
                >
                    Create key
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat label="Active" value={0} sub="keys" />
                <Stat label="Requests (24h)" value="—" sub="across all keys" />
                <Stat label="Rate limit" value="60" sub="req/min, default" />
                <Stat label="Last call" value="—" sub="across all keys" last />
            </StatStrip>

            <SectionBar label="Quick start" />
            <div className="px-5 py-4 border-b border-slate-200/60">
                <div className="rounded-md border border-slate-200 bg-slate-950 overflow-hidden">
                    <div className="h-8 px-3 flex items-center gap-2 border-b border-slate-800/60">
                        <div className="size-1.5 rounded-full bg-red-400/70" />
                        <div className="size-1.5 rounded-full bg-amber-400/70" />
                        <div className="size-1.5 rounded-full bg-emerald-400/70" />
                        <span className="ml-2 text-[11px] text-slate-400 font-mono">curl</span>
                        <button
                            type="button"
                            onClick={copyCode}
                            className="ml-auto inline-flex items-center gap-1 h-6 px-2 rounded text-slate-400 hover:text-white hover:bg-slate-800 transition-colors text-[11px]"
                        >
                            <CopyIcon className="w-3 h-3" />
                            Copy
                        </button>
                    </div>
                    <pre className="px-4 py-3 text-[12px] leading-relaxed text-slate-200 font-mono whitespace-pre overflow-x-auto">
{CODE_SNIPPET}
                    </pre>
                </div>
                <p className="text-[11.5px] text-slate-500 mt-2 leading-relaxed">
                    Send the key as a Bearer header. Every request is logged with
                    request id, latency and origin — visible from this page once
                    keys are live.
                </p>
            </div>

            <SectionBar label="Keys" count={0} />
            <div
                aria-hidden
                className="border-b border-slate-200/60 opacity-70 pointer-events-none select-none divide-y divide-slate-200/60"
            >
                {SAMPLE_KEYS.map((k, i) => (
                    <div key={i} className="h-11 px-5 flex items-center gap-3">
                        <KeyIcon className="w-3.5 h-3.5 text-slate-400" />
                        <span className="text-[12.5px] text-slate-900 font-medium">
                            {k.label}
                        </span>
                        <span className="font-mono text-[11px] text-slate-400 ml-3 truncate">
                            {k.prefix}
                        </span>
                        <span className="ml-auto font-mono text-[10.5px] text-slate-400 tabular-nums">
                            last used {k.lastUsed}
                        </span>
                    </div>
                ))}
            </div>

            <SectionBar label="Default scopes" />
            <PageBody>
                <div className="px-5 py-3 divide-y divide-slate-200/60">
                    {SAMPLE_PERMISSIONS.map((p) => (
                        <div key={p.scope} className="h-9 flex items-center gap-3">
                            <ShieldCheckIcon
                                className={`w-3.5 h-3.5 shrink-0 ${
                                    p.granted ? "text-emerald-500" : "text-slate-300"
                                }`}
                            />
                            <span className="font-mono text-[11.5px] text-slate-900">
                                {p.scope}
                            </span>
                            <span className="text-[11.5px] text-slate-500 truncate">
                                {p.description}
                            </span>
                            <span
                                className={`ml-auto text-[10.5px] uppercase tracking-[0.08em] font-medium ${
                                    p.granted ? "text-emerald-600" : "text-slate-400"
                                }`}
                            >
                                {p.granted ? "default" : "opt in"}
                            </span>
                        </div>
                    ))}
                </div>
            </PageBody>
        </Page>
    );
}
