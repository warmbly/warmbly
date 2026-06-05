// EspCoveragePanel — the visual "provider matching" map under the ESP matching
// control. A bipartite diagram: recipient providers on the left, this
// campaign's mailboxes on the right, with connector lines showing which mailbox
// actually serves which recipient — mode-aware, no new API.
//
// Grounded in the scheduler (internal/scheduler/campaign_scheduler.go):
//   • recipient provider is derived from the domain → "gmail" | "outlook" |
//     unknown (everything else).
//   • a mailbox matches a recipient when: mailbox is smtp_imap (wildcard), OR
//     the recipient is unknown (wildcard), OR provider equality.
//   • strict + no matching mailbox → the recipient is DEFERRED (held to the next
//     slot), never sent cross-provider and never skipped.
//   • prefer + no matching mailbox → falls back to a cross-provider mailbox.
//
// Pool mirrors the backend resolution: explicit senders ∪ tag mailboxes, else
// all active mailboxes. Only healthy (active) mailboxes count toward coverage.

import React from "react";
import useEmails from "@/lib/api/hooks/app/emails/useEmails";
import ProviderLogo from "./ProviderLogo";

type Mode = "off" | "prefer" | "strict";

const LABEL: Record<string, string> = {
    gmail: "Google",
    outlook: "Outlook",
    smtp_imap: "Other / SMTP",
    other: "Other domains",
};

// Fixed node geometry so connector coordinates are deterministic (no measuring).
const NODE_H = 52;
const GAP = 12;
const CENTERS = [NODE_H / 2, NODE_H + GAP + NODE_H / 2, 2 * (NODE_H + GAP) + NODE_H / 2];
const TOTAL_H = 3 * NODE_H + 2 * GAP;

const STROKE = { match: "#10b981", fallback: "#f59e0b", neutral: "#cbd5e1" } as const;

export default function EspCoveragePanel({
    mode,
    emailTags,
    explicitAccounts,
}: {
    mode: Mode;
    emailTags: string[];
    explicitAccounts: string[];
}) {
    const { emails, isLoading } = useEmails({ query: "", tag: "", limit: 200 });

    const pool = React.useMemo(() => {
        const explicit = new Set(explicitAccounts);
        const tags = new Set(emailTags);
        const picked = emails.filter((e) => {
            if (explicit.size && explicit.has(e.id)) return true;
            if (tags.size && (e.tags ?? []).some((t) => tags.has(t))) return true;
            return false;
        });
        const resolved = explicit.size || tags.size ? picked : emails;
        return resolved.filter((e) => e.status === "active");
    }, [emails, emailTags, explicitAccounts]);

    const counts = React.useMemo(() => {
        let gmail = 0;
        let outlook = 0;
        let smtp = 0;
        for (const e of pool) {
            if (e.provider === "gmail") gmail++;
            else if (e.provider === "outlook") outlook++;
            else smtp++;
        }
        return { gmail, outlook, smtp, total: pool.length };
    }, [pool]);

    if (isLoading) {
        return (
            <div className="rounded-lg border border-slate-200 bg-slate-50/50 p-4">
                <div className="h-4 w-40 bg-slate-200/70 rounded animate-pulse" />
            </div>
        );
    }

    if (counts.total === 0) {
        return (
            <div className="rounded-lg border border-amber-200 bg-amber-50/60 p-4">
                <p className="text-[11.5px] text-amber-700 leading-relaxed">
                    No active mailboxes resolve for this campaign yet, so there's nothing to match against. Add
                    mailboxes or tags under <span className="font-medium">Sending accounts</span>.
                </p>
            </div>
        );
    }

    const mailboxes = [
        { key: "gmail", count: counts.gmail },
        { key: "outlook", count: counts.outlook },
        { key: "smtp_imap", count: counts.smtp },
    ];
    const recipients = ["gmail", "outlook", "other"];

    const isPrimary = (r: string, m: string) => r === "other" || m === "smtp_imap" || r === m;

    const edges: { ri: number; mi: number; tone: keyof typeof STROKE; dashed: boolean }[] = [];
    recipients.forEach((r, ri) => {
        mailboxes.forEach((m, mi) => {
            if (m.count === 0) return;
            const primary = isPrimary(r, m.key);
            if (mode === "off") edges.push({ ri, mi, tone: "neutral", dashed: false });
            else if (mode === "strict") {
                if (primary) edges.push({ ri, mi, tone: "match", dashed: false });
            } else {
                edges.push({ ri, mi, tone: primary ? "match" : "fallback", dashed: !primary });
            }
        });
    });

    const recStatus = (r: string): "ok" | "warn" | "blocked" => {
        if (r === "other" || mode === "off") return "ok";
        const samePrimary = (r === "gmail" ? counts.gmail : counts.outlook) > 0 || counts.smtp > 0;
        if (samePrimary) return "ok";
        if (mode === "strict") return "blocked";
        return counts.total > 0 ? "warn" : "blocked";
    };
    const statusLabel = { ok: "Covered", warn: "Fallback", blocked: "Deferred" } as const;
    const statusText = { ok: "text-emerald-600", warn: "text-amber-600", blocked: "text-rose-600" } as const;
    const nodeBorder = {
        ok: "border-slate-200",
        warn: "border-amber-200 bg-amber-50/40",
        blocked: "border-rose-200 bg-rose-50/40",
    } as const;

    return (
        <div className="rounded-lg border border-slate-200 bg-slate-50/50 p-4 space-y-3.5">
            <div className="flex items-center justify-between text-[10px] uppercase tracking-[0.14em] text-slate-400">
                <span>Recipients</span>
                <span>Your mailboxes</span>
            </div>

            <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-stretch">
                {/* Recipients (left) */}
                <div className="min-w-0 flex flex-col" style={{ gap: GAP }}>
                    {recipients.map((r) => {
                        const st = recStatus(r);
                        return (
                            <div
                                key={r}
                                className={`rounded-lg border bg-white px-2.5 flex items-center gap-2.5 ${nodeBorder[st]}`}
                                style={{ height: NODE_H }}
                            >
                                <ProviderLogo provider={r} className="size-7" />
                                <div className="min-w-0">
                                    <div className="text-[12px] font-medium text-slate-800 truncate leading-tight">
                                        {LABEL[r]}
                                    </div>
                                    <div className={`text-[10.5px] leading-tight ${statusText[st]}`}>
                                        {statusLabel[st]}
                                    </div>
                                </div>
                            </div>
                        );
                    })}
                </div>

                {/* Connectors */}
                <svg
                    className="w-14 sm:w-20"
                    height={TOTAL_H}
                    viewBox={`0 0 100 ${TOTAL_H}`}
                    preserveAspectRatio="none"
                    aria-hidden="true"
                >
                    {edges.map((e, i) => (
                        <path
                            key={i}
                            d={`M0,${CENTERS[e.ri]} C50,${CENTERS[e.ri]} 50,${CENTERS[e.mi]} 100,${CENTERS[e.mi]}`}
                            fill="none"
                            stroke={STROKE[e.tone]}
                            strokeWidth={2}
                            strokeLinecap="round"
                            strokeOpacity={e.tone === "neutral" ? 0.6 : 0.9}
                            strokeDasharray={e.dashed ? "4 4" : undefined}
                            vectorEffect="non-scaling-stroke"
                        />
                    ))}
                </svg>

                {/* Mailboxes (right) */}
                <div className="min-w-0 flex flex-col" style={{ gap: GAP }}>
                    {mailboxes.map((m) => {
                        const empty = m.count === 0;
                        return (
                            <div
                                key={m.key}
                                className={`rounded-lg border bg-white px-2.5 flex items-center gap-2.5 ${
                                    empty ? "border-slate-200/70" : "border-slate-200"
                                }`}
                                style={{ height: NODE_H }}
                            >
                                <ProviderLogo provider={m.key} className="size-7" muted={empty} />
                                <div className="min-w-0 flex-1">
                                    <div
                                        className={`text-[12px] font-medium truncate leading-tight ${
                                            empty ? "text-slate-400" : "text-slate-800"
                                        }`}
                                    >
                                        {LABEL[m.key]}
                                    </div>
                                    <div className="text-[10.5px] text-slate-400 leading-tight">
                                        {m.count} {m.count === 1 ? "mailbox" : "mailboxes"}
                                    </div>
                                </div>
                            </div>
                        );
                    })}
                </div>
            </div>

            {/* Legend */}
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 pt-0.5 text-[10px] text-slate-400">
                <span className="inline-flex items-center gap-1.5">
                    <span className="w-4 border-t-2 border-emerald-500" /> Match
                </span>
                {mode === "prefer" && (
                    <span className="inline-flex items-center gap-1.5">
                        <span className="w-4 border-t-2 border-dashed border-amber-500" /> Fallback
                    </span>
                )}
                {mode === "off" && (
                    <span className="inline-flex items-center gap-1.5">
                        <span className="w-4 border-t-2 border-slate-300" /> Any mailbox
                    </span>
                )}
            </div>

            <p className="text-[11px] text-slate-400 leading-relaxed">
                {mode === "off"
                    ? "Provider matching is off — any recipient can be sent from any mailbox in the pool."
                    : mode === "strict"
                      ? "Strict only ever sends from a same-provider mailbox; a recipient with no match is held (deferred) until one frees up — never sent cross-provider. SMTP/IMAP mailboxes are a universal wildcard, and non-Google/Outlook domains send from any mailbox."
                      : "Prefer uses a same-provider mailbox when one has capacity, otherwise it falls back to another provider — it never holds a recipient. SMTP/IMAP mailboxes can carry any provider."}
            </p>
        </div>
    );
}
