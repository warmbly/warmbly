// EspCoveragePanel — the "provider matching" visual under the ESP matching
// control. For each recipient provider it shows which of this campaign's
// mailboxes serve it, ranked so the difference between a true same-provider
// match and an SMTP wildcard is obvious. No connector lines, no new API.
//
// Grounded in the scheduler (internal/scheduler/campaign_scheduler.go):
//   • recipient provider is derived from the domain → "gmail" | "outlook" |
//     unknown (everything else).
//   • a mailbox matches a recipient when: mailbox is smtp_imap (WILDCARD —
//     serves any provider, even in strict), OR the recipient is unknown
//     (wildcard), OR provider equality (gmail↔gmail, outlook↔outlook).
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
type Status = "ok" | "warn" | "blocked" | "any";

const LABEL: Record<string, string> = {
    gmail: "Google",
    outlook: "Outlook",
    smtp_imap: "Other / SMTP",
    other: "Other domains",
};

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

    const poolChips = (
        [
            { key: "gmail", count: counts.gmail },
            { key: "outlook", count: counts.outlook },
            { key: "smtp_imap", count: counts.smtp },
        ] as const
    ).filter((p) => p.count > 0);

    // Pool is all wildcard mailboxes (SMTP only) → even strict can't narrow.
    const onlyWildcard = counts.smtp > 0 && counts.gmail === 0 && counts.outlook === 0;

    return (
        <div className="rounded-lg border border-slate-200 bg-slate-50/50 p-4 space-y-3.5">
            {/* Pool summary + active mode */}
            <div className="flex items-center justify-between gap-2">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400">Your mailboxes</span>
                <ModePill mode={mode} />
            </div>
            <div className="flex items-center gap-2 flex-wrap -mt-1">
                {poolChips.map((p) => (
                    <span
                        key={p.key}
                        className="inline-flex items-center gap-1.5 h-7 pl-1 pr-2 rounded-md border border-slate-200 bg-white"
                    >
                        <ProviderLogo provider={p.key} className="size-5" />
                        <span className="text-[11.5px] text-slate-700">
                            {LABEL[p.key]} <span className="font-semibold tabular-nums">{p.count}</span>
                        </span>
                    </span>
                ))}
            </div>

            {/* Recipient → serving mailboxes, ranked (match vs wildcard vs catch-all) */}
            <div className="space-y-1.5">
                {(["gmail", "outlook"] as const).map((r) => {
                    const sameCount = r === "gmail" ? counts.gmail : counts.outlook;
                    const hasSame = sameCount > 0;
                    // SMTP/IMAP is a wildcard for Gmail/Outlook recipients only OUTSIDE
                    // strict — under strict, "same provider" excludes unknown-ESP SMTP.
                    const wildServes = counts.smtp > 0 && mode !== "strict";
                    // prefer falls back cross-provider when no same + no wildcard.
                    const crossCount = (r === "gmail" ? counts.outlook : counts.gmail);

                    let status: Status;
                    if (mode === "off") status = "any";
                    else if (hasSame || wildServes) status = "ok";
                    else if (mode === "prefer" && crossCount > 0) status = "warn";
                    else status = "blocked";

                    const tint =
                        status === "blocked"
                            ? "border-rose-200 bg-rose-50/40"
                            : status === "warn"
                              ? "border-amber-200 bg-amber-50/40"
                              : "border-slate-200 bg-white";

                    return (
                        <div key={r} className={`flex items-center gap-3 rounded-lg border px-3 py-2.5 ${tint}`}>
                            <ProviderLogo provider={r} className="size-8" />
                            <div className="min-w-0 flex-1">
                                <div className="text-[12.5px] font-medium text-slate-800 leading-tight">
                                    {LABEL[r]} recipients
                                </div>
                                <div className="mt-1.5 flex items-center gap-1.5 flex-wrap">
                                    <span className="text-[10px] uppercase tracking-[0.1em] text-slate-400">via</span>
                                    {mode === "off" ? (
                                        <AnyMailbox />
                                    ) : (
                                        <>
                                            {hasSame && (
                                                <Chip provider={r} count={sameCount} variant="match" />
                                            )}
                                            {wildServes && (
                                                <Chip provider="smtp_imap" count={counts.smtp} variant="wildcard" />
                                            )}
                                            {!hasSame && !wildServes && mode === "prefer" && crossCount > 0 && (
                                                <Chip
                                                    provider={r === "gmail" ? "outlook" : "gmail"}
                                                    count={crossCount}
                                                    variant="fallback"
                                                />
                                            )}
                                            {status === "blocked" && (
                                                <span className="text-[11px] text-rose-500 font-medium">
                                                    No matching mailbox
                                                </span>
                                            )}
                                        </>
                                    )}
                                </div>
                            </div>
                            <StatusBadge status={status} />
                        </div>
                    );
                })}

                {/* Unknown-domain recipients always use any mailbox (wildcard), every mode. */}
                <div className="flex items-center gap-3 rounded-lg border border-slate-200 bg-white px-3 py-2.5">
                    <ProviderLogo provider="other" className="size-8" />
                    <div className="min-w-0 flex-1">
                        <div className="text-[12.5px] font-medium text-slate-800 leading-tight">Other domains</div>
                        <div className="mt-1.5 flex items-center gap-1.5 flex-wrap">
                            <span className="text-[10px] uppercase tracking-[0.1em] text-slate-400">via</span>
                            <AnyMailbox />
                        </div>
                    </div>
                    <StatusBadge status="any" />
                </div>
            </div>

            {mode === "strict" && onlyWildcard && (
                <p className="rounded-md bg-amber-50 border border-amber-200 px-2.5 py-2 text-[11px] text-amber-700 leading-relaxed">
                    All your mailboxes are SMTP/IMAP, which can send to any provider — so Strict can't narrow by
                    provider here and behaves like Off. Connect a Google or Outlook mailbox to truly restrict
                    same-provider sending.
                </p>
            )}

            <p className="text-[11px] text-slate-400 leading-relaxed">
                {mode === "off"
                    ? "Provider matching is off — any recipient can be sent from any mailbox in the pool."
                    : mode === "strict"
                      ? "Strict sends Google and Outlook recipients only from a same-provider mailbox (the sky match) — a recipient with no same-provider mailbox is held (deferred) until one frees up, never sent cross-provider. SMTP/IMAP mailboxes only carry non-Google/Outlook (“other”) domains under strict."
                      : "Prefer uses a same-provider mailbox when one has capacity (the sky match), otherwise it falls back to another provider (the amber chip) — it never holds a recipient. SMTP/IMAP mailboxes can carry any provider."}
            </p>
        </div>
    );
}

function AnyMailbox() {
    return <span className="text-[11px] text-slate-400">Any mailbox in the pool</span>;
}

function Chip({
    provider,
    count,
    variant,
}: {
    provider: string;
    count: number;
    variant: "match" | "wildcard" | "fallback";
}) {
    const cls =
        variant === "match"
            ? "border-sky-200 bg-sky-50/60"
            : variant === "fallback"
              ? "border-amber-200 bg-amber-50/60 border-dashed"
              : "border-slate-200 bg-slate-50";
    return (
        <span className={`inline-flex items-center gap-1 h-6 pl-1 pr-1.5 rounded-md border ${cls}`}>
            <ProviderLogo provider={provider} className="size-4" muted={variant !== "match"} />
            <span className={`text-[11px] tabular-nums ${variant === "match" ? "text-slate-700" : "text-slate-500"}`}>
                ×{count}
            </span>
            {variant === "wildcard" && (
                <span className="text-[9px] uppercase tracking-[0.1em] text-slate-400 ml-0.5">any</span>
            )}
            {variant === "fallback" && (
                <span className="text-[9px] uppercase tracking-[0.1em] text-amber-500 ml-0.5">fallback</span>
            )}
        </span>
    );
}

function StatusBadge({ status }: { status: Status }) {
    const map = {
        ok: { text: "Covered", cls: "bg-emerald-50 text-emerald-700", dot: "bg-emerald-500" },
        warn: { text: "Fallback", cls: "bg-amber-50 text-amber-700", dot: "bg-amber-500" },
        blocked: { text: "Deferred", cls: "bg-rose-50 text-rose-700", dot: "bg-rose-500" },
        any: { text: "Any", cls: "bg-slate-100 text-slate-500", dot: "bg-slate-400" },
    }[status];
    return (
        <span
            className={`shrink-0 inline-flex items-center gap-1.5 h-5 px-2 rounded text-[10px] uppercase tracking-[0.08em] font-medium ${map.cls}`}
        >
            <span className={`size-1.5 rounded-full ${map.dot}`} />
            {map.text}
        </span>
    );
}

function ModePill({ mode }: { mode: Mode }) {
    const m = {
        off: { t: "Matching off", c: "bg-slate-100 text-slate-500" },
        prefer: { t: "Prefer same", c: "bg-sky-50 text-sky-700" },
        strict: { t: "Strict same", c: "bg-sky-50 text-sky-700" },
    }[mode];
    return (
        <span
            className={`inline-flex items-center h-5 px-2 rounded text-[10px] uppercase tracking-[0.08em] font-medium ${m.c}`}
        >
            {m.t}
        </span>
    );
}
