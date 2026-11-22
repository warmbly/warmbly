// Detail drawers for one store order, domain or mailbox, with the operator
// actions on each. Actions show only to admins holding manage-organizations;
// the server enforces the same bit.

import { useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { AlertTriangle, CalendarClock, HandCoins, Pause, Play, PlugZap, RefreshCw, Send } from "lucide-react";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Callout, Property, PropertyList, StatusBadge } from "@/components/ui/kit";
import { ErrorState } from "@/components/ErrorState";
import {
    getStoreOrder,
    placeStoreOrder,
    refreshStoreOrder,
    setStoreDomainAutoRenew,
    storeDomainAction,
    storeMailboxAction,
    type AdminStoreDomain,
    type AdminStoreMailbox,
    type AdminStoreOrder,
} from "@/lib/api/client/admin/store";
import { absolute, relative } from "@/app/dashboard/jobs/format";
import { label, providerLabel, refundableMicros, statusTone, usd } from "./format";
import { RefundDialog } from "./RefundDialog";
import { useStoreAction } from "./useStoreAction";

const linkCls = "underline decoration-border-strong underline-offset-2 hover:text-[var(--admin-accent-strong)]";
const mono = "font-mono text-[12px] break-all";

function Drawer({ open, onClose, children }: { open: boolean; onClose: () => void; children: ReactNode }) {
    return (
        <Sheet open={open} onOpenChange={(o) => !o && onClose()}>
            <SheetContent side="right" className="w-full gap-0 sm:max-w-xl">
                {children}
            </SheetContent>
        </Sheet>
    );
}

function Header({ title, children }: { title: ReactNode; children?: ReactNode }) {
    return (
        <SheetHeader className="gap-1 border-b border-border px-5 py-4 pr-12">
            <SheetTitle className="truncate">{title}</SheetTitle>
            <SheetDescription className="text-[12.5px]">{children}</SheetDescription>
        </SheetHeader>
    );
}

function Actions({ canManage, children }: { canManage: boolean; children: ReactNode }) {
    if (!canManage) {
        return <p className="text-xs text-muted-foreground">Actions need the manage organizations admin permission.</p>;
    }
    return <div className="flex flex-wrap gap-1.5">{children}</div>;
}

function When({ ts, empty = "—" }: { ts: string | null | undefined; empty?: string }) {
    if (!ts) return <span className="text-subtle-foreground">{empty}</span>;
    return <span title={absolute(ts)}>{relative(ts)}</span>;
}

function Workspace({ id, name }: { id: string; name: string }) {
    return (
        <Link to={`/store/workspaces/${id}`} className={linkCls}>
            {name || id}
        </Link>
    );
}

// ---- Order -------------------------------------------------------------------

export function OrderDrawer({ order, onClose }: { order: AdminStoreOrder | null; onClose: () => void }) {
    return (
        <Drawer open={order !== null} onClose={onClose}>
            {order && <OrderDetail key={order.id} seed={order} />}
        </Drawer>
    );
}

function OrderDetail({ seed }: { seed: AdminStoreOrder }) {
    const qc = useQueryClient();
    const key = ["admin", "store", "order", seed.id];
    const q = useQuery({ queryKey: key, queryFn: () => getStoreOrder(seed.id), initialData: seed, initialDataUpdatedAt: 0 });
    const o = q.data;
    const { act, busy, canManage } = useStoreAction();
    const [refunding, setRefunding] = useState(false);
    const put = (next: AdminStoreOrder | undefined) => next && qc.setQueryData(key, next);
    const refundable = refundableMicros(o);
    const refundedLines = new Set(o.refunded_lines ?? []);

    return (
        <>
            <Header title={`Order ${o.id.slice(0, 8)}`}>
                <Workspace id={o.organization_id} name={o.organization_name} />, placed <When ts={o.created_at} />
            </Header>
            <div className="flex-1 space-y-6 overflow-y-auto px-5 py-4">
                {q.error && <ErrorState error={q.error} title="Failed to refresh this order" onRetry={() => q.refetch()} />}
                <div className="flex flex-wrap items-center gap-1.5">
                    <StatusBadge tone={statusTone(o.status)} dot>
                        {label(o.status)}
                    </StatusBadge>
                    {o.stuck && <StatusBadge tone="warning">stuck</StatusBadge>}
                    <StatusBadge tone={o.placed ? "neutral" : "warning"}>{o.placed ? "placed at GraphKit" : "not placed"}</StatusBadge>
                </div>

                {o.failure_code && (
                    <Callout tone="danger" icon={AlertTriangle} title="Failure">
                        <span className="font-mono">{o.failure_code}</span>
                    </Callout>
                )}
                {o.stuck && (
                    <Callout tone="warning" icon={AlertTriangle} title="This order has not moved">
                        It is open past the point the reconciler expects it to settle. Refresh re-reads it from GraphKit
                        {o.charged_micros > 0 && !o.placed ? "; it was charged but never placed, so retry placing it." : "."}
                    </Callout>
                )}

                <Actions canManage={canManage}>
                    <Button
                        size="sm"
                        variant="outline"
                        disabled={busy !== null}
                        onClick={async () =>
                            put(
                                await act({
                                    id: "refresh",
                                    confirm: { title: "Refresh this order?", description: "Re-reads the order from GraphKit and applies what it says.", confirmLabel: "Refresh" },
                                    success: "Order refreshed",
                                    run: () => refreshStoreOrder(o.id),
                                }),
                            )
                        }
                    >
                        <RefreshCw className="size-3.5" />
                        {busy === "refresh" ? "Refreshing…" : "Refresh"}
                    </Button>
                    {o.charged_micros > 0 && !o.placed && (
                        <Button
                            size="sm"
                            variant="outline"
                            disabled={busy !== null}
                            onClick={async () =>
                                put(
                                    await act({
                                        id: "place",
                                        confirm: {
                                            title: "Retry placing this order?",
                                            description: `The workspace was charged ${usd(o.charged_micros)} and GraphKit never received the order. It is placed under its original key, so it cannot be placed twice.`,
                                            confirmLabel: "Place order",
                                        },
                                        success: "Order placed",
                                        run: () => placeStoreOrder(o.id),
                                    }),
                                )
                            }
                        >
                            <Send className="size-3.5" />
                            {busy === "place" ? "Placing…" : "Retry placing"}
                        </Button>
                    )}
                    {refundable > 0 && (
                        <Button size="sm" variant="outline" disabled={busy !== null} onClick={() => setRefunding(true)}>
                            <HandCoins className="size-3.5" />
                            Refund
                        </Button>
                    )}
                </Actions>

                <PropertyList title="Money">
                    <Property label="Due at checkout">{usd(o.due_now_micros)}</Property>
                    <Property label="Charged">{usd(o.charged_micros)}</Property>
                    <Property label="Refunded">{usd(o.refunded_micros)}</Property>
                    <Property label="Refundable">{usd(refundable)}</Property>
                </PropertyList>

                <div>
                    <div className="mb-1 text-xs font-medium text-muted-foreground">Lines</div>
                    <div className="divide-y divide-border overflow-hidden rounded-lg border border-border">
                        {o.lines.map((l, i) => (
                            <div key={i} className="flex items-center justify-between gap-3 px-3 py-2 text-[13px]">
                                <div className="min-w-0">
                                    <div className="truncate font-medium">{l.domain}</div>
                                    <div className="text-xs text-muted-foreground">
                                        {l.type === "domain"
                                            ? `Domain, ${l.years ?? 1} yr${l.renew_micros ? `, renews at ${usd(l.renew_micros)}` : ""}`
                                            : `${l.count ?? 0} ${providerLabel(l.provider ?? "")} mailbox${l.count === 1 ? "" : "es"}${l.monthly_micros ? `, ${usd(l.monthly_micros)}/mo each` : ""}`}
                                    </div>
                                </div>
                                <div className="flex shrink-0 items-center gap-2">
                                    {refundedLines.has(i) && <StatusBadge tone="neutral">refunded</StatusBadge>}
                                    <span className="tabular-nums">{usd(l.amount_micros)}</span>
                                </div>
                            </div>
                        ))}
                        {o.lines.length === 0 && <div className="px-3 py-2 text-[13px] text-muted-foreground">No lines</div>}
                    </div>
                </div>

                <div>
                    <div className="mb-1 text-xs font-medium text-muted-foreground">Steps</div>
                    <ol className="divide-y divide-border overflow-hidden rounded-lg border border-border">
                        {o.steps.map((s) => (
                            <li key={s.key} className="flex items-center justify-between gap-3 px-3 py-2 text-[13px]">
                                <span>{label(s.key)}</span>
                                <span className="flex items-center gap-2 text-xs text-muted-foreground">
                                    {s.at && <When ts={s.at} />}
                                    <StatusBadge tone={statusTone(s.status)}>{s.status}</StatusBadge>
                                </span>
                            </li>
                        ))}
                        {o.steps.length === 0 && <li className="px-3 py-2 text-[13px] text-muted-foreground">No steps yet</li>}
                    </ol>
                </div>

                <PropertyList title="Record">
                    <Property label="Order id"><span className={mono}>{o.id}</span></Property>
                    <Property label="GraphKit id">{o.upstream_id ? <span className={mono}>{o.upstream_id}</span> : <span className="text-subtle-foreground">Not placed</span>}</Property>
                    <Property label="Created by">{o.created_by ? <Link to={`/users/${o.created_by}`} className={linkCls}>{o.created_by.slice(0, 8)}</Link> : <span className="text-subtle-foreground">API or system</span>}</Property>
                    <Property label="Updated"><When ts={o.updated_at} /></Property>
                    <Property label="Completed"><When ts={o.completed_at} empty="Open" /></Property>
                </PropertyList>
            </div>
            <RefundDialog
                order={o}
                open={refunding}
                onOpenChange={setRefunding}
                onRefunded={(next) => {
                    put(next);
                    setRefunding(false);
                }}
            />
        </>
    );
}

// ---- Domain ------------------------------------------------------------------

export function DomainDrawer({ domain, onClose }: { domain: AdminStoreDomain | null; onClose: () => void }) {
    return (
        <Drawer open={domain !== null} onClose={onClose}>
            {domain && <DomainDetail key={domain.id} seed={domain} />}
        </Drawer>
    );
}

function DomainDetail({ seed }: { seed: AdminStoreDomain }) {
    const [d, setD] = useState(seed);
    const { act, busy, canManage } = useStoreAction();
    const put = (next: AdminStoreDomain | undefined) => next && setD(next);
    const purchased = d.source === "purchased";

    return (
        <>
            <Header title={d.name}>
                <Workspace id={d.organization_id} name={d.organization_name} />, {purchased ? "bought here" : "connected"}{" "}
                <When ts={d.created_at} />
            </Header>
            <div className="flex-1 space-y-6 overflow-y-auto px-5 py-4">
                <div className="flex flex-wrap items-center gap-1.5">
                    <StatusBadge tone={statusTone(d.status)} dot>
                        {label(d.status)}
                    </StatusBadge>
                    <StatusBadge>{purchased ? "purchased" : "connected"}</StatusBadge>
                    {d.renewal_failures > 0 && <StatusBadge tone="danger">{d.renewal_failures} renewal failure{d.renewal_failures === 1 ? "" : "s"}</StatusBadge>}
                </div>

                <Actions canManage={canManage}>
                    <Button
                        size="sm"
                        variant="outline"
                        disabled={busy !== null}
                        onClick={async () =>
                            put(
                                await act({
                                    id: "refresh",
                                    confirm: { title: `Refresh ${d.name}?`, description: "Re-reads the domain from GraphKit and applies what it says.", confirmLabel: "Refresh" },
                                    success: "Domain refreshed",
                                    run: () => storeDomainAction(d.id, "refresh"),
                                }),
                            )
                        }
                    >
                        <RefreshCw className="size-3.5" />
                        {busy === "refresh" ? "Refreshing…" : "Refresh"}
                    </Button>
                    {purchased && (
                        <Button
                            size="sm"
                            variant="outline"
                            disabled={busy !== null}
                            onClick={async () =>
                                put(
                                    await act({
                                        id: "renew",
                                        confirm: {
                                            title: `Renew ${d.name} now?`,
                                            description: `Starts a renewal now instead of waiting for the renewal loop, at the renewal price of ${usd(d.renew_micros)}.`,
                                            confirmLabel: "Renew now",
                                            destructive: true,
                                        },
                                        success: "Renewal started",
                                        run: () => storeDomainAction(d.id, "renew"),
                                    }),
                                )
                            }
                        >
                            <CalendarClock className="size-3.5" />
                            {busy === "renew" ? "Renewing…" : "Force renewal"}
                        </Button>
                    )}
                </Actions>

                <PropertyList title="Registration">
                    <Property label="Expires">
                        {d.expires_at ? (
                            <span className="tabular-nums" title={absolute(d.expires_at)}>
                                {new Date(d.expires_at).toLocaleDateString()} ({relative(d.expires_at)})
                            </span>
                        ) : (
                            <span className="text-subtle-foreground">—</span>
                        )}
                    </Property>
                    <Property label="Auto-renew">
                        {purchased ? (
                            <span className="inline-flex items-center gap-2">
                                <Switch
                                    checked={d.auto_renew}
                                    disabled={!canManage || busy !== null}
                                    aria-label="Auto-renew"
                                    onCheckedChange={async (on) =>
                                        put(
                                            await act({
                                                id: "auto_renew",
                                                confirm: {
                                                    title: on ? `Turn auto-renew on for ${d.name}?` : `Turn auto-renew off for ${d.name}?`,
                                                    description: on
                                                        ? `The workspace is charged ${usd(d.renew_micros)} each year before it expires.`
                                                        : "The domain expires at the end of its term unless someone renews it by hand.",
                                                    confirmLabel: on ? "Turn on" : "Turn off",
                                                    destructive: !on,
                                                },
                                                success: on ? "Auto-renew on" : "Auto-renew off",
                                                run: () => setStoreDomainAutoRenew(d.id, on),
                                            }),
                                        )
                                    }
                                />
                                {d.auto_renew ? "On" : "Off"}
                            </span>
                        ) : (
                            <span className="text-subtle-foreground">Registered elsewhere</span>
                        )}
                    </Property>
                    <Property label="Renewal price">{purchased ? usd(d.renew_micros) : "—"}</Property>
                    <Property label="Last renewal try"><When ts={d.renewal_attempt_at} empty="None" /></Property>
                    <Property label="Last notice">{d.last_notice ? label(d.last_notice) : <span className="text-subtle-foreground">None</span>}</Property>
                </PropertyList>

                <PropertyList title="Mail">
                    <Property label="Provider">{providerLabel(d.provider)}</Property>
                    <Property label="Mailboxes">
                        <Link to={`/store?tab=mailboxes&org=${d.organization_id}&q=${encodeURIComponent(`@${d.name}`)}`} className={linkCls}>
                            {d.mailbox_count}
                        </Link>
                    </Property>
                    <Property label="Tracking host">{d.tracking_host ? <span className={mono}>{d.tracking_host}</span> : <span className="text-subtle-foreground">None</span>}</Property>
                </PropertyList>

                <PropertyList title="Record">
                    <Property label="Domain id"><span className={mono}>{d.id}</span></Property>
                    <Property label="GraphKit id">{d.upstream_id ? <span className={mono}>{d.upstream_id}</span> : "—"}</Property>
                    <Property label="Updated"><When ts={d.updated_at} /></Property>
                </PropertyList>
            </div>
        </>
    );
}

// ---- Mailbox -----------------------------------------------------------------

export function MailboxDrawer({ mailbox, onClose }: { mailbox: AdminStoreMailbox | null; onClose: () => void }) {
    return (
        <Drawer open={mailbox !== null} onClose={onClose}>
            {mailbox && <MailboxDetail key={mailbox.id} seed={mailbox} />}
        </Drawer>
    );
}

function MailboxDetail({ seed }: { seed: AdminStoreMailbox }) {
    const [m, setM] = useState(seed);
    const { act, busy, canManage } = useStoreAction();
    const put = (next: AdminStoreMailbox | undefined) => next && setM(next);
    const ended = m.status === "cancelled" || m.status === "failed";
    const canRetryConnect = m.connect_state === "failed" || m.connect_state === "waiting";

    return (
        <>
            <Header title={m.email}>
                <Workspace id={m.organization_id} name={m.organization_name} />, {providerLabel(m.provider)}, bought <When ts={m.created_at} />
            </Header>
            <div className="flex-1 space-y-6 overflow-y-auto px-5 py-4">
                <div className="flex flex-wrap items-center gap-1.5">
                    <StatusBadge tone={statusTone(m.status)} dot>
                        {label(m.status)}
                    </StatusBadge>
                    <StatusBadge tone={statusTone(m.connect_state)}>{m.connect_state === "connected" ? "connected" : `connect ${m.connect_state}`}</StatusBadge>
                    {m.is_admin && <StatusBadge>admin mailbox</StatusBadge>}
                    {m.cancels_at && <StatusBadge tone="warning">cancels {new Date(m.cancels_at).toLocaleDateString()}</StatusBadge>}
                    {m.billing_failures > 0 && <StatusBadge tone="danger">{m.billing_failures} billing failure{m.billing_failures === 1 ? "" : "s"}</StatusBadge>}
                </div>

                {m.connect_state === "failed" && (
                    <Callout tone="danger" icon={AlertTriangle} title="Connecting it to Warmbly failed">
                        {m.connect_error ? <span className="font-mono">{m.connect_error}</span> : "No reason recorded."} After {m.connect_attempts} attempt
                        {m.connect_attempts === 1 ? "" : "s"}.
                    </Callout>
                )}

                <Actions canManage={canManage}>
                    <Button
                        size="sm"
                        variant="outline"
                        disabled={busy !== null}
                        onClick={async () =>
                            put(
                                await act({
                                    id: "refresh",
                                    confirm: { title: `Refresh ${m.email}?`, description: "Re-reads the mailbox from GraphKit and applies what it says.", confirmLabel: "Refresh" },
                                    success: "Mailbox refreshed",
                                    run: () => storeMailboxAction(m.id, "refresh"),
                                }),
                            )
                        }
                    >
                        <RefreshCw className="size-3.5" />
                        {busy === "refresh" ? "Refreshing…" : "Refresh"}
                    </Button>
                    {canRetryConnect && (
                        <Button
                            size="sm"
                            variant="outline"
                            disabled={busy !== null}
                            onClick={async () =>
                                put(
                                    await act({
                                        id: "connect",
                                        confirm: {
                                            title: `Retry connecting ${m.email}?`,
                                            description: "Resets the connect attempts and tries to connect it to the workspace now.",
                                            confirmLabel: "Retry connect",
                                        },
                                        success: "Connect retried",
                                        run: () => storeMailboxAction(m.id, "connect"),
                                    }),
                                )
                            }
                        >
                            <PlugZap className="size-3.5" />
                            {busy === "connect" ? "Connecting…" : "Retry connect"}
                        </Button>
                    )}
                    {!ended && !m.cancels_at && (
                        <Button
                            size="sm"
                            variant="outline"
                            disabled={busy !== null}
                            onClick={async () =>
                                put(
                                    await act({
                                        id: "cancel",
                                        confirm: {
                                            title: `Cancel ${m.email}?`,
                                            description: `It keeps working until ${m.paid_through ? new Date(m.paid_through).toLocaleDateString() : "the end of the paid period"}, then it is removed. It can be resumed until then.`,
                                            confirmLabel: "Cancel at period end",
                                            destructive: true,
                                        },
                                        success: "Mailbox set to cancel",
                                        run: () => storeMailboxAction(m.id, "cancel"),
                                    }),
                                )
                            }
                        >
                            <Pause className="size-3.5" />
                            {busy === "cancel" ? "Cancelling…" : "Cancel"}
                        </Button>
                    )}
                    {!ended && m.cancels_at && (
                        <Button
                            size="sm"
                            variant="outline"
                            disabled={busy !== null}
                            onClick={async () =>
                                put(
                                    await act({
                                        id: "resume",
                                        confirm: {
                                            title: `Resume ${m.email}?`,
                                            description: `It stays on and the workspace is billed ${usd(m.monthly_micros)} a month again.`,
                                            confirmLabel: "Resume",
                                        },
                                        success: "Mailbox resumed",
                                        run: () => storeMailboxAction(m.id, "resume"),
                                    }),
                                )
                            }
                        >
                            <Play className="size-3.5" />
                            {busy === "resume" ? "Resuming…" : "Resume"}
                        </Button>
                    )}
                </Actions>

                <PropertyList title="Billing">
                    <Property label="Monthly">{usd(m.monthly_micros)}</Property>
                    <Property label="Paid through">{m.paid_through ? new Date(m.paid_through).toLocaleDateString() : <span className="text-subtle-foreground">—</span>}</Property>
                    <Property label="Cancels">{m.cancels_at ? new Date(m.cancels_at).toLocaleDateString() : <span className="text-subtle-foreground">No</span>}</Property>
                    <Property label="Charge pending">{m.pending_charge ? "Yes" : "No"}</Property>
                    <Property label="Billing failures">{m.billing_failures}</Property>
                </PropertyList>

                <PropertyList title="Connection">
                    <Property label="State">{label(m.connect_state)}</Property>
                    <Property label="Method">{m.connect_method ? label(m.connect_method) : "—"}</Property>
                    <Property label="Attempts">{m.connect_attempts}</Property>
                    <Property label="Mailbox">
                        {m.email_account_id ? (
                            <Link to={`/mailboxes?mailbox_id=${m.email_account_id}`} className={linkCls}>
                                Open in Mailboxes
                            </Link>
                        ) : (
                            <span className="text-subtle-foreground">Not connected</span>
                        )}
                    </Property>
                </PropertyList>

                <PropertyList title="Record">
                    <Property label="Mailbox id"><span className={mono}>{m.id}</span></Property>
                    <Property label="GraphKit id">{m.upstream_id ? <span className={mono}>{m.upstream_id}</span> : "—"}</Property>
                    <Property label="Updated"><When ts={m.updated_at} /></Property>
                </PropertyList>
            </div>
        </>
    );
}
