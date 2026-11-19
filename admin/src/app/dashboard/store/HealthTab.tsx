// Store health: which GraphKit environment the store talks to, the partner
// balance that pays for every order, whether webhooks and the two loops are
// keeping up, and the counts an operator acts on, each linking to its list.

import { Link } from "react-router-dom";
import { AlertTriangle, Bell, Wallet } from "lucide-react";
import { Callout, EmptyState, Panel, Property, PropertyList, Section, StatusBadge } from "@/components/ui/kit";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorState } from "@/components/ErrorState";
import type { StoreHealth, StoreLoopStatus } from "@/lib/api/client/admin/store";
import { absolute, humanSeconds, relative } from "@/app/dashboard/jobs/format";
import { TONE_TEXT, type Tone } from "@/lib/tones";
import { cn } from "@/lib/utils";
import { MODE_TONE, storeLink, usd } from "./format";
import { useStoreHealth } from "./useStoreHealth";

// Webhooks only arrive on change, so a quiet day is normal; past this the reconciler is the only source.
const WEBHOOK_STALE_MS = 24 * 60 * 60 * 1000;
// A loop this far behind has stopped keeping the store current.
const LOOP_LAG_WARN_SECONDS = 60 * 60;

export function HealthTab() {
    const q = useStoreHealth();
    if (q.isLoading) return <Skeleton className="h-64 w-full rounded-xl" />;
    if (q.error || !q.data) return <ErrorState error={q.error} title="Failed to load store health" onRetry={() => q.refetch()} />;
    const h = q.data;
    if (!h.enabled || h.mode === "off") {
        return (
            <EmptyState
                icon={Wallet}
                title="The store is off"
                hint="It turns on when GRAPHKIT_API_KEY is set on the backend. Nothing is sold and no loop runs until then."
            />
        );
    }
    return (
        <div>
            <div className="mb-5 flex flex-wrap items-center gap-2">
                <StatusBadge tone={MODE_TONE[h.mode] ?? "neutral"} dot>
                    {h.mode}
                </StatusBadge>
                <StatusBadge tone={h.metered ? "neutral" : "warning"}>{h.metered ? "charges workspaces" : "unmetered: charges nothing"}</StatusBadge>
            </div>

            <div className="grid gap-3 md:grid-cols-2">
                <BalancePanel h={h} />
                <WebhookPanel h={h} />
                <LoopPanel title="Reconciler" hint="Re-reads open orders and domains from GraphKit" loop={h.reconciler} />
                <LoopPanel title="Billing loop" hint="Charges mailbox months and domain renewals" loop={h.billing} />
            </div>

            <Section title="Needs attention" className="mt-8">
                <CountGrid h={h} />
            </Section>

            <Section
                title="Collected ahead of GraphKit"
                description="Mailboxes whose GraphKit period started before Warmbly collected it. This should stay at 0."
            >
                {(h.uncollected ?? []).length === 0 ? (
                    <div className="rounded-xl border border-border px-4 py-6 text-center text-[13px] text-muted-foreground">
                        Every mailbox period was paid before it started
                    </div>
                ) : (
                    <ul className="divide-y divide-border overflow-hidden rounded-xl border border-border">
                        {(h.uncollected ?? []).map((m) => (
                            <li key={m.id} className="flex items-center gap-3 px-4 py-2.5 text-[13px]">
                                <AlertTriangle className={cn("size-3.5 shrink-0", TONE_TEXT.danger)} />
                                <span className="min-w-0 flex-1 truncate">{m.email}</span>
                                <span className="shrink-0 tabular-nums text-muted-foreground" title={absolute(m.period_end)}>
                                    GraphKit to {absolute(m.period_end)}, paid to {absolute(m.paid_through)}
                                </span>
                            </li>
                        ))}
                    </ul>
                )}
            </Section>

            <Section title="Recent store alerts" description="store.unavailable operator alerts, newest first.">
                {h.alerts.length === 0 ? (
                    <div className="rounded-xl border border-border px-4 py-6 text-center text-[13px] text-muted-foreground">No recent alerts</div>
                ) : (
                    <ul className="divide-y divide-border overflow-hidden rounded-xl border border-border">
                        {h.alerts.map((a, i) => (
                            <li key={`${a.at}-${i}`} className="flex items-center gap-3 px-4 py-2.5 text-[13px]">
                                <Bell className="size-3.5 shrink-0 text-subtle-foreground" />
                                <span className="min-w-0 flex-1 truncate">{a.title}</span>
                                <span className="hidden font-mono text-[11.5px] text-subtle-foreground sm:inline">{a.code}</span>
                                <span className="shrink-0 tabular-nums text-muted-foreground" title={absolute(a.at)}>
                                    {relative(a.at)}
                                </span>
                            </li>
                        ))}
                    </ul>
                )}
            </Section>
        </div>
    );
}

function BalancePanel({ h }: { h: StoreHealth }) {
    const b = h.partner_balance;
    const low = !!b && b.balance_micros <= b.low_threshold_micros;
    return (
        <Panel title="Partner balance" description="What GraphKit draws on for every order and renewal">
            {!b ? (
                <Callout tone="warning" icon={AlertTriangle} title="Balance unavailable">
                    {h.partner_balance_error === "store_unavailable" ? "GraphKit did not answer the balance read." : "No balance reported."}
                </Callout>
            ) : (
                <>
                    <div className={cn("text-2xl font-semibold tracking-[-0.03em] tabular-nums", low ? TONE_TEXT.danger : "text-foreground")}>
                        {usd(b.balance_micros)}
                        <span className="ml-1.5 text-xs font-normal text-muted-foreground">{b.currency.toUpperCase()}</span>
                    </div>
                    <div className="mt-1 text-xs text-muted-foreground">
                        Low below {usd(b.low_threshold_micros)}
                        {!b.livemode && ", sandbox funds"}
                    </div>
                    {low && (
                        <Callout tone="danger" icon={AlertTriangle} title="Balance is at or below the low threshold" className="mt-3">
                            Top up the partner account before orders and renewals start failing.
                        </Callout>
                    )}
                </>
            )}
        </Panel>
    );
}

function WebhookPanel({ h }: { h: StoreHealth }) {
    const stale = !h.last_webhook_at || Date.now() - new Date(h.last_webhook_at).getTime() > WEBHOOK_STALE_MS;
    return (
        <Panel title="Webhooks" description="GraphKit pushes order, domain and mailbox changes">
            <PropertyList>
                <Property label="Last received">
                    <span className={cn("inline-flex items-center gap-1", stale && TONE_TEXT.warning)} title={absolute(h.last_webhook_at)}>
                        {stale && <AlertTriangle className="size-3.5" />}
                        {relative(h.last_webhook_at)}
                    </span>
                </Property>
                <Property label="Event cursor">{h.events_cursor_set ? "Set" : <span className={TONE_TEXT.warning}>Not set</span>}</Property>
            </PropertyList>
            {stale && <p className="mt-2 text-xs text-muted-foreground">Nothing in the last day. The reconciler still catches changes, more slowly.</p>}
        </Panel>
    );
}

function loopTone(loop: StoreLoopStatus): Tone {
    const s = loop.last_status.toLowerCase();
    if (s === "error" || s === "failed") return "danger";
    if (loop.lag_seconds > LOOP_LAG_WARN_SECONDS) return "warning";
    return "success";
}

function LoopPanel({ title, hint, loop }: { title: string; hint: string; loop: StoreLoopStatus | null }) {
    if (!loop) {
        return (
            <Panel title={title} description={hint}>
                <p className="text-[13px] text-muted-foreground">Has not run yet on this instance.</p>
            </Panel>
        );
    }
    const tone = loopTone(loop);
    return (
        <Panel
            title={title}
            description={hint}
            actions={
                <StatusBadge tone={tone} dot>
                    {loop.last_status || "unknown"}
                </StatusBadge>
            }
        >
            <PropertyList>
                <Property label="Lag">
                    <span className={cn("tabular-nums", tone !== "success" && TONE_TEXT[tone])}>{humanSeconds(loop.lag_seconds)}</span>
                </Property>
                <Property label="Last finished">
                    <span title={absolute(loop.last_finished_at)}>{relative(loop.last_finished_at)}</span>
                </Property>
                <Property label="Last started">
                    <span title={absolute(loop.last_started_at)}>{relative(loop.last_started_at)}</span>
                </Property>
                {loop.last_error && (
                    <Property label="Last error">
                        <span className="font-mono text-[12px] text-[var(--admin-danger)]">{loop.last_error}</span>
                    </Property>
                )}
            </PropertyList>
        </Panel>
    );
}

function CountGrid({ h }: { h: StoreHealth }) {
    const c = h.counts;
    const items: { label: string; value: number; to: string; tone: Tone }[] = [
        { label: "Open orders", value: c.orders_open, to: storeLink("orders", { status: "open" }), tone: "info" },
        { label: "Stuck orders", value: c.orders_stuck, to: storeLink("orders", { stuck: "true" }), tone: "danger" },
        { label: "Failed orders, 7 days", value: c.orders_failed_7d, to: storeLink("orders", { status: "failed" }), tone: "danger" },
        { label: "Failed connects", value: c.connects_failed, to: storeLink("mailboxes", { connect_state: "failed" }), tone: "danger" },
        { label: "Billing failures", value: c.mailboxes_billing_failed, to: storeLink("mailboxes", { billing_failed: "true" }), tone: "danger" },
        { label: "Renewal failures", value: c.domains_renewal_failed, to: storeLink("domains", { renewal_failed: "true" }), tone: "danger" },
        { label: "Expiring in 30 days", value: c.domains_expiring_30d, to: storeLink("domains", { expiring_days: "30" }), tone: "warning" },
        { label: "Live mailboxes", value: c.mailboxes_live, to: storeLink("mailboxes", { status: "active" }), tone: "neutral" },
        { label: "Live domains", value: c.domains_live, to: storeLink("domains", { status: "active" }), tone: "neutral" },
    ];
    return (
        <div className="surface-lit grid grid-cols-2 gap-px overflow-hidden rounded-xl border border-border bg-border md:grid-cols-3">
            {items.map((it) => (
                <Link key={it.label} to={it.to} className="bg-card px-4 py-3.5 transition-colors hover:bg-accent/40">
                    <div className="text-xs text-muted-foreground">{it.label}</div>
                    <div className={cn("mt-1.5 text-2xl leading-8 font-semibold tracking-[-0.03em] tabular-nums", it.value > 0 && it.tone !== "neutral" ? TONE_TEXT[it.tone] : "text-foreground")}>
                        {it.value.toLocaleString()}
                    </div>
                </Link>
            ))}
        </div>
    );
}
