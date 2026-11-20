// Store domains, bought through Warmbly or connected from elsewhere: renewals, forwarding, the connection, DNS and transfer-out.
import React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import toast from "react-hot-toast/headless";
import {
    ArrowUpRightIcon,
    CheckIcon,
    ChevronRightIcon,
    CopyIcon,
    GlobeIcon,
    MailCheckIcon,
    MoreHorizontalIcon,
    RefreshCwIcon,
    ShoppingBagIcon,
    Trash2Icon,
    TruckIcon,
    XIcon,
} from "lucide-react";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { FieldError, TextInput } from "@/components/ui/field";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuSeparator, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission } from "@/hooks/usePermission";
import {
    usePatchStoreDomain,
    useRemoveStoreDomain,
    useRenewStoreDomain,
    useStoreDomain,
    useStoreDomains,
    useStoreStatus,
    useTransferOutStoreDomain,
} from "@/lib/api/hooks/app/store/useStore";
import type { StoreDomain, StoreStatus } from "@/lib/api/models/app/store/Store";
import { forwardingProblem, normalizeForwarding } from "@/lib/buy/addresses";
import { fmtMicros, fmtYearly } from "@/lib/buy/money";
import { storeErrorCode, storeErrorMessage } from "@/lib/buy/errors";
import { cn } from "@/lib/utils";
import { BuyMailboxesButton } from "./BuyEntry";
import ConnectionGuide from "./ConnectionGuide";
import DnsRecords from "./DnsRecords";
import { PressButton } from "./Press";
import { OrdersLink, StoreChargesLink } from "./StoreLinks";
import StorePaused from "./StorePaused";
import { useStorePausedStatus } from "./usePurchasedMailbox";

const STATUS_LABEL: Record<string, string> = {
    registering: "Registering",
    expired: "Expired, still renewable",
    redemption: "In redemption",
    transferring_out: "Transferring out",
    transferred_out: "Transferred out",
    failed: "Failed",
    connecting: "Connecting",
    removed: "Removed",
};

const CONNECTION_LABEL: Record<string, { text: string; tone: "slate" | "amber" | "red" | "sky" | "emerald" }> = {
    pending: { text: "Waiting for DNS", tone: "sky" },
    active: { text: "Connected · registered elsewhere", tone: "slate" },
    moved: { text: "DNS moved away from Warmbly", tone: "red" },
    expired: { text: "Connection instructions expired", tone: "amber" },
};

/** Renew, auto-renew and transfer-out apply: bought here, and active or expired. */
function renewable(d: StoreDomain): boolean {
    return d.source !== "connected" && (d.renewable ?? (d.status === "active" || d.status === "expired"));
}

function asDate(v: Date | string | null | undefined): Date | null {
    if (!v) return null;
    const d = v instanceof Date ? v : new Date(v);
    return Number.isNaN(d.getTime()) ? null : d;
}

const fmtDay = (d: Date) => d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });

/** "Renews Nov 16, 2027" / "Expires in 12 days", with a tone for the row. */
function expiryLine(d: StoreDomain): { text: string; tone: keyof typeof TONE } {
    if (d.source === "connected" && d.connection) {
        const c = CONNECTION_LABEL[d.connection.status];
        if (d.connection.status === "active" && (d.dns_conflicts ?? []).length > 0) return { text: "Records in the way of mail", tone: "amber" };
        if (c) return c;
    }
    if (d.status !== "active") return { text: STATUS_LABEL[d.status] ?? d.status, tone: d.status === "failed" || d.status === "redemption" ? "red" : "amber" };
    const at = asDate(d.expires_at);
    if (!at) return { text: "Active", tone: "slate" };
    const days = Math.ceil((at.getTime() - Date.now()) / 86_400_000);
    if (d.auto_renew) return { text: `Renews ${fmtDay(at)} · ${fmtYearly(d.renew_micros)}`, tone: "slate" };
    if (days <= 30) return { text: days <= 0 ? "Expires today" : `Expires in ${days} ${days === 1 ? "day" : "days"}`, tone: days <= 7 ? "red" : "amber" };
    return { text: `Expires ${fmtDay(at)}`, tone: "slate" };
}

const TONE = { slate: "text-slate-500", amber: "text-amber-700", red: "text-red-600", sky: "text-sky-700", emerald: "text-emerald-700" } as const;

/** Renew and transfer-out, shared by the row menu and the drawer; both ask first. */
function useDomainActions(onCode: (id: string, code: string) => void) {
    const confirm = useConfirm();
    const status = useStoreStatus();
    const renew = useRenewStoreDomain();
    const transfer = useTransferOutStoreDomain();
    const remove = useRemoveStoreDomain();
    const metered = status.data?.metered !== false;

    const renewNow = (d: StoreDomain) =>
        confirm.show(
            `Renew ${d.name} for another year? ${metered ? `${fmtMicros(d.renew_micros)} comes out of the workspace balance.` : "This instance's operator covers it."}`,
            async () => {
                try {
                    const next = await renew.mutateAsync({ id: d.id, idempotencyKey: `renew-${d.id}-${asDate(d.expires_at)?.toISOString() ?? "none"}` });
                    const at = asDate(next.expires_at);
                    toast.success(at ? `${d.name} now runs until ${fmtDay(at)}` : `${d.name} renewed`);
                } catch (e) {
                    toast.error(storeErrorMessage(e));
                }
            },
            undefined,
            { title: "Renew domain", confirmLabel: metered ? `Renew for ${fmtMicros(d.renew_micros)}` : "Renew", tone: "primary" },
        );

    const transferOut = (d: StoreDomain) =>
        confirm.show(
            `Transfer ${d.name} away from Warmbly? It is unlocked and you get the code your new registrar asks for. Its mailboxes keep working until the transfer completes, then mail stops unless you set it up again there.`,
            async () => {
                try {
                    const { auth_code } = await transfer.mutateAsync({ id: d.id, idempotencyKey: crypto.randomUUID() });
                    onCode(d.id, auth_code);
                } catch (e) {
                    toast.error(storeErrorMessage(e));
                }
            },
            undefined,
            { title: "Transfer out", confirmLabel: "Unlock and get the code" },
        );

    const removeDomain = (d: StoreDomain, after?: () => void) =>
        confirm.show(
            `Remove ${d.name} from Warmbly? It stays registered where it is, and the records Warmbly set up stop being looked after.`,
            async () => {
                try {
                    await remove.mutateAsync(d.id);
                    toast.success(`${d.name} removed`);
                    after?.();
                } catch (e) {
                    const code = storeErrorCode(e);
                    toast.error(
                        code === "domain_in_use"
                            ? `${d.name} still has ${d.mailbox_count === 1 ? "a mailbox" : "mailboxes"}. Cancel ${d.mailbox_count === 1 ? "it" : "them"} first, then remove the domain.`
                            : storeErrorMessage(e),
                    );
                }
            },
            undefined,
            { title: "Remove domain", confirmLabel: "Remove domain" },
        );

    return { renewNow, transferOut, removeDomain, renewing: renew.isPending, transferring: transfer.isPending, removing: remove.isPending };
}

export default function PurchasedDomains({ query }: { query: string }) {
    const list = useStoreDomains();
    const canBill = usePermission("MANAGE_BILLING");
    const patch = usePatchStoreDomain();
    // Without GraphKit every domain action answers store_disabled, so only what is recorded here is shown.
    const paused = useStorePausedStatus();
    const [open, setOpen] = React.useState<string | null>(null);
    const [codes, setCodes] = React.useState<Record<string, string>>({});
    const actions = useDomainActions((id, code) => {
        setCodes((c) => ({ ...c, [id]: code }));
        setOpen(id);
    });

    const all = list.data?.data ?? [];
    const q = query.trim().toLowerCase();
    const shown = q ? all.filter((d) => d.name.includes(q)) : all;
    const current = all.find((d) => d.id === open) ?? null;

    if (list.isPending) {
        return (
            <div aria-busy="true">
                {Array.from({ length: 3 }).map((_, i) => (
                    <div key={i} className="px-5 py-3 border-b border-slate-200/60 flex items-center gap-3">
                        <div className="size-6 rounded-md skeleton-shimmer" />
                        <div className="h-3 w-40 rounded skeleton-shimmer" />
                        <div className="ml-auto h-4 w-8 rounded-full skeleton-shimmer" />
                    </div>
                ))}
            </div>
        );
    }
    if (list.isError) {
        return <p className="px-5 py-16 text-center text-[12.5px] text-slate-500">{storeErrorMessage(list.error)}</p>;
    }
    if (all.length === 0) {
        return (
            <div className="px-5 py-16 flex flex-col items-center text-center">
                <div className="size-10 rounded-full bg-sky-50 text-sky-600 inline-flex items-center justify-center mb-3">
                    <ShoppingBagIcon className="w-4 h-4" />
                </div>
                <p className="text-[13px] text-slate-800 font-medium">No purchased domains yet</p>
                <p className="mt-1 text-[12px] text-slate-500 max-w-[44ch] leading-relaxed">
                    Domains you buy here are registered, authenticated and connected for you, and listed here with their renewals.
                </p>
                <div className="mt-4 flex flex-wrap items-center justify-center gap-1.5">
                    <BuyMailboxesButton size="sm" />
                    <OrdersLink />
                </div>
            </div>
        );
    }

    return (
        <>
            <div className="min-h-9 pl-5 pr-3 py-1 border-b border-slate-200/60 flex flex-wrap items-center gap-x-1 gap-y-1">
                <span className="mr-auto text-[11.5px] text-slate-500">Renewals, forwarding and DNS for domains bought or connected here.</span>
                <OrdersLink />
                <StoreChargesLink />
            </div>
            <div className="hidden md:grid grid-cols-[minmax(0,1.6fr)_72px_minmax(0,1fr)_96px_28px] gap-x-3 items-center px-5 h-8 border-b border-slate-200/60 bg-slate-50/40">
                {["Domain", "Mailboxes", "Renewal", "Auto-renew", ""].map((h, i) => (
                    <span key={i} className={cn("text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]", i === 1 && "text-right")}>
                        {h}
                    </span>
                ))}
            </div>
            <AnimatePresence initial={false}>
                {shown.map((d) => {
                    const exp = expiryLine(d);
                    const live = renewable(d);
                    const connected = d.source === "connected";
                    return (
                        <motion.div key={d.id} layout="position" initial={{ opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0 }}>
                            <div
                                role="button"
                                tabIndex={0}
                                onClick={() => setOpen(d.id)}
                                onKeyDown={(e) => {
                                    if (e.key === "Enter" || e.key === " ") {
                                        e.preventDefault();
                                        setOpen(d.id);
                                    }
                                }}
                                className="group grid grid-cols-[minmax(0,1fr)_auto_28px] md:grid-cols-[minmax(0,1.6fr)_72px_minmax(0,1fr)_96px_28px] gap-x-3 items-center px-5 py-2.5 border-b border-slate-200/60 bg-white hover:bg-slate-50/80 cursor-pointer transition-colors outline-none focus-visible:bg-slate-50"
                            >
                                <div className="flex items-center gap-2.5 min-w-0">
                                    {d.provider ? (
                                        <ProviderLogo id={d.provider} size="md" />
                                    ) : (
                                        <span className="size-6 rounded-md border border-slate-200 inline-flex items-center justify-center shrink-0">
                                            <GlobeIcon className="w-3 h-3 text-slate-400" />
                                        </span>
                                    )}
                                    <div className="min-w-0">
                                        <div className="text-[12.5px] text-slate-900 font-medium [overflow-wrap:anywhere]">{d.name}</div>
                                        <div className={cn("md:hidden text-[11px] truncate", TONE[exp.tone])}>
                                            {exp.text} · {d.mailbox_count} {d.mailbox_count === 1 ? "mailbox" : "mailboxes"}
                                        </div>
                                    </div>
                                </div>
                                <div className="hidden md:block text-right text-[12px] text-slate-700 tabular-nums">{d.mailbox_count}</div>
                                <div className={cn("hidden md:block text-[12px] truncate", TONE[exp.tone])}>{exp.text}</div>
                                {connected || paused ? (
                                    <div className="text-[11px] text-slate-400 truncate">
                                        <span className="hidden md:inline">{connected ? "At its registrar" : "Paused"}</span>
                                    </div>
                                ) : (
                                    <div onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()} className="flex items-center">
                                        <Toggle
                                            value={d.auto_renew}
                                            onChange={(v) => patch.mutate({ id: d.id, patch: { auto_renew: v } })}
                                            disabled={!canBill || !live}
                                            ariaLabel={`Auto-renew ${d.name}`}
                                        />
                                    </div>
                                )}
                                <div onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()} className="flex justify-end">
                                    <PopoverMenu align="end">
                                        <PopoverMenuTrigger asChild>
                                            <button
                                                type="button"
                                                aria-label={`Actions for ${d.name}`}
                                                className="size-7 rounded-md text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center transition-colors opacity-100 md:opacity-0 md:group-hover:opacity-100 md:focus-visible:opacity-100 data-[state=open]:opacity-100"
                                            >
                                                <MoreHorizontalIcon className="w-3.5 h-3.5" />
                                            </button>
                                        </PopoverMenuTrigger>
                                        <PopoverMenuContent minWidth={200}>
                                            <PopoverMenuItem onSelect={() => setOpen(d.id)} icon={<ChevronRightIcon className="w-3 h-3" />}>
                                                {paused ? "Details" : connected ? "Connection and DNS" : "Details and DNS"}
                                            </PopoverMenuItem>
                                            {paused ? null : connected ? (
                                                <>
                                                    <PopoverMenuSeparator />
                                                    <PopoverMenuItem danger onSelect={() => actions.removeDomain(d)} icon={<Trash2Icon className="w-3 h-3" />}>
                                                        Remove domain…
                                                    </PopoverMenuItem>
                                                </>
                                            ) : (
                                                <>
                                                    <PopoverMenuItem
                                                        onSelect={() => actions.renewNow(d)}
                                                        disabled={!canBill || !live}
                                                        icon={<RefreshCwIcon className="w-3 h-3" />}
                                                        description={`${fmtYearly(d.renew_micros)}`}
                                                    >
                                                        Renew now
                                                    </PopoverMenuItem>
                                                    <PopoverMenuSeparator />
                                                    <PopoverMenuItem
                                                        danger
                                                        onSelect={() => actions.transferOut(d)}
                                                        disabled={!canBill || !live}
                                                        icon={<TruckIcon className="w-3 h-3" />}
                                                    >
                                                        Transfer out…
                                                    </PopoverMenuItem>
                                                </>
                                            )}
                                        </PopoverMenuContent>
                                    </PopoverMenu>
                                </div>
                            </div>
                        </motion.div>
                    );
                })}
            </AnimatePresence>
            {shown.length === 0 && <p className="px-5 py-12 text-center text-[12.5px] text-slate-500">No purchased domain matches.</p>}

            <AnimatePresence>
                {current && (
                    <DomainDrawer
                        key={current.id}
                        domain={current}
                        code={codes[current.id]}
                        canBill={canBill}
                        paused={paused}
                        actions={actions}
                        onClose={() => setOpen(null)}
                    />
                )}
            </AnimatePresence>
        </>
    );
}

function DomainDrawer({
    domain: row,
    code,
    canBill,
    paused,
    actions,
    onClose,
}: {
    domain: StoreDomain;
    code?: string;
    canBill: boolean;
    paused: StoreStatus | null;
    actions: ReturnType<typeof useDomainActions>;
    onClose: () => void;
}) {
    const reduced = useReducedMotion();
    const confirm = useConfirm();
    // The detail read reaches GraphKit for the DNS records, so it waits with everything else while paused.
    const detail = useStoreDomain(paused ? null : row.id);
    const d = detail.data ? { ...row, ...detail.data } : row;
    const patch = usePatchStoreDomain();
    const [fwd, setFwd] = React.useState<string | null>(null);
    const fwdValue = fwd ?? d.forwarding_url ?? "";
    const fwdDirty = fwd !== null && normalizeForwarding(fwd) !== (d.forwarding_url ?? "");
    const fwdProblem = forwardingProblem(fwdValue);
    const exp = expiryLine(d);
    const live = renewable(d);
    const connected = d.source === "connected";
    const at = asDate(d.expires_at);
    const verification = d.owner_verification?.status === "pending" ? d.owner_verification : null;
    const verifyBy = asDate(verification?.deadline);
    const transferAt = asDate(d.transfer?.auto_approve_at);

    const requestClose = React.useCallback(() => {
        if (fwdDirty) confirm.show("Discard the forwarding address you typed?", async () => onClose());
        else onClose();
    }, [fwdDirty, confirm, onClose]);

    React.useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            requestClose();
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [requestClose]);

    const saveForwarding = () => {
        if (!fwdDirty || fwdProblem) return;
        patch.mutate(
            { id: d.id, patch: { forwarding_url: normalizeForwarding(fwdValue) } },
            {
                onSuccess: () => {
                    setFwd(null);
                    toast.success(fwdValue.trim() ? `${d.name} now forwards to your site` : `${d.name} no longer forwards`);
                },
            },
        );
    };

    return (
        <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            className="fixed inset-0 z-[110] flex justify-end bg-slate-900/30 backdrop-blur-[2px]"
            onMouseDown={requestClose}
        >
            <motion.aside
                role="dialog"
                aria-modal="true"
                aria-label={d.name}
                initial={reduced ? { opacity: 0 } : { x: 32, opacity: 0 }}
                animate={{ x: 0, opacity: 1 }}
                exit={reduced ? { opacity: 0 } : { x: 32, opacity: 0 }}
                transition={{ duration: 0.2, ease: [0.32, 0.72, 0, 1] }}
                onMouseDown={(e) => e.stopPropagation()}
                className="flex flex-col w-full max-w-full md:w-[34rem] md:max-w-[95%] h-full bg-white border-l border-slate-200 shadow-[-12px_0_24px_-12px_rgba(15,23,42,0.08)]"
            >
                <div className="shrink-0 px-5 h-14 flex items-center gap-3 border-b border-slate-200">
                    {d.provider ? <ProviderLogo id={d.provider} size="xl" /> : <GlobeIcon className="w-4 h-4 text-slate-400" />}
                    <div className="min-w-0 flex-1">
                        <div className="text-[13px] font-medium text-slate-900 [overflow-wrap:anywhere]">{d.name}</div>
                        <div className={cn("text-[10.5px] truncate", TONE[exp.tone])}>
                            {exp.text} · {d.mailbox_count} {d.mailbox_count === 1 ? "mailbox" : "mailboxes"}
                        </div>
                    </div>
                    <button
                        type="button"
                        onClick={requestClose}
                        aria-label="Close"
                        className="size-7 rounded-md flex items-center justify-center text-slate-400 hover:text-slate-900 hover:bg-slate-100 transition-colors shrink-0"
                    >
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>

                <div className="flex-1 min-h-0 overflow-y-auto divide-y divide-slate-200/60">
                    {paused && (
                        <div className="px-5 py-3">
                            <StorePaused status={paused} compact />
                        </div>
                    )}

                    {(verification || d.transfer?.status === "requested") && (
                        <div className="px-5 py-3 space-y-2">
                            {verification && (
                                <div className="rounded-md border border-amber-200 bg-amber-50/60 px-3 py-2 flex items-start gap-2">
                                    <MailCheckIcon className="w-3.5 h-3.5 text-amber-600 mt-0.5 shrink-0" />
                                    <p className="text-[11.5px] text-amber-900 leading-relaxed">
                                        Confirm the domain owner email. A link went to <span className="font-medium">{verification.email}</span>
                                        {verifyBy ? `; open it before ${fmtDay(verifyBy)}` : ""}, or the domain can be suspended until it is confirmed.
                                    </p>
                                </div>
                            )}
                            {d.transfer?.status === "requested" && (
                                <div className="rounded-md border border-sky-100 bg-sky-50/60 px-3 py-2 flex items-start gap-2">
                                    <TruckIcon className="w-3.5 h-3.5 text-sky-600 mt-0.5 shrink-0" />
                                    <p className="text-[11.5px] text-sky-900 leading-relaxed">
                                        A transfer to another registrar was requested
                                        {asDate(d.transfer.requested_at) ? ` on ${fmtDay(asDate(d.transfer.requested_at) as Date)}` : ""}.
                                        {transferAt ? ` It goes through on its own by ${fmtDay(transferAt)}.` : " It goes through once the new registrar completes it."} Mail
                                        keeps working until then.
                                    </p>
                                </div>
                            )}
                        </div>
                    )}

                    {connected && d.connection && (
                        <section className="px-5 py-4 space-y-2.5">
                            <Eyebrow>Connection</Eyebrow>
                            <ConnectionGuide domain={d} canEdit={!paused} />
                        </section>
                    )}

                    {!connected && !paused && (
                    <section className="px-5 py-4 space-y-3">
                        <Eyebrow>Renewal</Eyebrow>
                        <div className="flex items-start justify-between gap-3">
                            <div className="min-w-0">
                                <div className="text-[12.5px] font-medium text-slate-900">Renew automatically</div>
                                <div className="text-[11px] text-slate-500 leading-relaxed">
                                    {d.auto_renew
                                        ? `Renewed 30 days before ${at ? fmtDay(at) : "it expires"} for ${fmtYearly(d.renew_micros)}, from the workspace balance.`
                                        : `Off. ${at ? `It expires ${fmtDay(at)}` : "It expires"} unless you renew it, and its mailboxes stop with it.`}
                                </div>
                            </div>
                            <Toggle
                                value={d.auto_renew}
                                onChange={(v) => patch.mutate({ id: d.id, patch: { auto_renew: v } })}
                                disabled={!canBill || !live}
                                ariaLabel="Renew automatically"
                            />
                        </div>
                        <PressButton tone="secondary" disabled={!canBill || !live || actions.renewing} onClick={() => actions.renewNow(d)}>
                            <RefreshCwIcon className={cn("w-3 h-3", actions.renewing && "animate-spin")} /> Renew now · {fmtMicros(d.renew_micros)}
                        </PressButton>
                        {!canBill && <p className="text-[11px] text-slate-400">Renewals need the Manage billing permission.</p>}
                    </section>
                    )}

                    {!connected && !paused && (
                    <section className="px-5 py-4 space-y-2">
                        <div className="flex items-center gap-2">
                            <Eyebrow>Website forward</Eyebrow>
                            {d.forwarding_url && d.forwarding_status && d.forwarding_status !== "none" && (
                                <span
                                    className={cn(
                                        "h-[18px] px-1.5 rounded-full border text-[10px] font-medium inline-flex items-center gap-1",
                                        d.forwarding_status === "active" ? "bg-emerald-50 border-emerald-100 text-emerald-700" : "bg-sky-50 border-sky-100 text-sky-700",
                                    )}
                                >
                                    <span className="size-1.5 rounded-full bg-current" />
                                    {d.forwarding_status === "active" ? "Live" : "Setting up"}
                                </span>
                            )}
                        </div>
                        <p className="text-[11px] text-slate-500">Visitors to {d.name} go to this address.</p>
                        <div className="flex items-start gap-2">
                            <div className="flex-1 min-w-0">
                                <TextInput
                                    value={fwdValue}
                                    onChange={setFwd}
                                    placeholder="https://yourcompany.com"
                                    invalid={!!fwdProblem}
                                    onKeyDown={(e) => {
                                        if (e.key === "Enter") saveForwarding();
                                    }}
                                    className="w-full"
                                />
                                <FieldError message={fwdProblem} />
                            </div>
                            <PressButton disabled={!fwdDirty || !!fwdProblem || patch.isPending} onClick={saveForwarding}>
                                Save
                            </PressButton>
                        </div>
                    </section>
                    )}

                    <section className="px-5 py-4 space-y-2">
                        <Eyebrow>Mail authentication</Eyebrow>
                        <div className="flex flex-wrap gap-1.5">
                            {(["mx", "spf", "dkim", "dmarc"] as const).map((k) => {
                                const v = d.mail_auth?.[k];
                                const ok = v === "ok";
                                const bad = v === "failed";
                                return (
                                    <span
                                        key={k}
                                        className={cn(
                                            "h-6 px-2 rounded-full border text-[11px] font-medium inline-flex items-center gap-1 uppercase",
                                            ok ? "bg-emerald-50 border-emerald-100 text-emerald-700" : bad ? "bg-red-50 border-red-100 text-red-700" : "bg-amber-50 border-amber-100 text-amber-800",
                                        )}
                                    >
                                        {ok ? <CheckIcon className="w-3 h-3" /> : <span className="size-1.5 rounded-full bg-current" />}
                                        {k}
                                    </span>
                                );
                            })}
                        </div>
                        <p className="text-[11px] text-slate-500">
                            {d.tracking_host ? (
                                <>
                                    Opens and clicks are tracked on <span className="font-mono text-slate-700">{d.tracking_host}</span>, set up by Warmbly.
                                </>
                            ) : (
                                "Warmbly sets up an open and click tracking host on this domain once its DNS is in place."
                            )}
                        </p>
                    </section>

                    {!paused && <DnsRecords domain={d} loading={detail.isPending} canEdit />}

                    {!paused && (
                    <section className="px-5 py-5 space-y-3">
                        <Eyebrow>Danger zone</Eyebrow>
                        {connected ? (
                            <div className="flex flex-col sm:flex-row gap-2 sm:gap-4 sm:items-center border-l-2 border-red-200 pl-3">
                                <div className="min-w-0 flex-1">
                                    <div className="text-[12.5px] font-medium text-red-700 leading-tight flex items-center gap-1.5">
                                        <Trash2Icon className="w-3 h-3" /> Remove domain
                                    </div>
                                    <div className="text-[11.5px] text-red-700/70 leading-snug mt-0.5">
                                        {d.mailbox_count > 0
                                            ? `It has ${d.mailbox_count} ${d.mailbox_count === 1 ? "mailbox" : "mailboxes"}. Cancel ${d.mailbox_count === 1 ? "it" : "them"} first, then it can be removed.`
                                            : "Stops Warmbly looking after its records. The domain stays registered where it is."}
                                    </div>
                                </div>
                                <PressButton
                                    tone="danger"
                                    disabled={actions.removing || d.mailbox_count > 0}
                                    onClick={() => actions.removeDomain(d, onClose)}
                                    className="self-start shrink-0"
                                >
                                    Remove domain…
                                </PressButton>
                            </div>
                        ) : (
                        <div className="flex flex-col sm:flex-row gap-2 sm:gap-4 sm:items-center border-l-2 border-red-200 pl-3">
                            <div className="min-w-0 flex-1">
                                <div className="text-[12.5px] font-medium text-red-700 leading-tight flex items-center gap-1.5">
                                    <TruckIcon className="w-3 h-3" /> Transfer out
                                </div>
                                <div className="text-[11.5px] text-red-700/70 leading-snug mt-0.5">
                                    Unlocks the domain and gives you the code another registrar asks for. Mail stops once it leaves, unless you set it up there.
                                </div>
                            </div>
                            <PressButton tone="danger" disabled={!canBill || !live || actions.transferring} onClick={() => actions.transferOut(d)} className="self-start shrink-0">
                                Transfer out…
                            </PressButton>
                        </div>
                        )}
                        <AnimatePresence>{code && <AuthCode code={code} />}</AnimatePresence>
                    </section>
                    )}

                    {d.forwarding_url && (
                        <div className="px-5 py-3">
                            <a href={d.forwarding_url} target="_blank" rel="noreferrer" className="text-[11.5px] text-sky-700 hover:text-sky-900 inline-flex items-center gap-1">
                                Open {d.forwarding_url} <ArrowUpRightIcon className="w-3 h-3" />
                            </a>
                        </div>
                    )}
                </div>
            </motion.aside>
        </motion.div>
    );
}

function AuthCode({ code }: { code: string }) {
    const [copied, setCopied] = React.useState(false);
    const copy = async () => {
        try {
            await navigator.clipboard.writeText(code);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1600);
        } catch {
            toast.error("Copy did not work here. Select the code and copy it.");
        }
    };
    return (
        <motion.div initial={{ opacity: 0, height: 0 }} animate={{ opacity: 1, height: "auto" }} exit={{ opacity: 0, height: 0 }} className="overflow-hidden">
            <div className="rounded-md border border-slate-200 bg-slate-50 px-3 py-2">
                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Transfer code</div>
                <div className="mt-1 flex items-center gap-2">
                    <code className="flex-1 min-w-0 font-mono text-[12.5px] text-slate-900 break-all select-all">{code}</code>
                    <PressButton tone="secondary" onClick={() => void copy()} aria-label="Copy transfer code">
                        {copied ? <CheckIcon className="w-3 h-3 text-emerald-600" /> : <CopyIcon className="w-3 h-3" />}
                        {copied ? "Copied" : "Copy"}
                    </PressButton>
                </div>
                <p className="mt-1.5 text-[11px] text-slate-500">Give this to your new registrar. It is shown once here; transfer out again for a fresh one.</p>
            </div>
        </motion.div>
    );
}

function Eyebrow({ children }: { children: React.ReactNode }) {
    return <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{children}</div>;
}
