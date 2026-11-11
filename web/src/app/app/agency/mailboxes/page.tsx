// Agency > Mailboxes: every client's mailboxes on one list, the inbox vendor accounts the agency
// holds once for all of them, and the ways to add more, by the agency or by the client.

import React from "react";
import { KeyRoundIcon, Loader2Icon, MoreHorizontalIcon, PlusIcon, Trash2Icon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { AgencyPage } from "@/components/app/agency/AgencyPage";
import { ClientAvatar } from "@/components/app/agency/home/AgencyHome";
import AddMailboxesDialog, { type AddMailboxesChoice } from "@/components/app/agency/mailboxes/AddMailboxesDialog";
import VendorAccountDialog from "@/components/app/agency/mailboxes/VendorAccountDialog";
import ConnectLinksDialog from "@/components/app/emails/connect-links/ConnectLinksDialog";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { EmptyBlock, PageBody, SectionBar, TopbarAction } from "@/components/layout/Page";
import { SearchInput } from "@/components/ui/field";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuSeparator, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import ScrollStrip from "@/components/ui/scroll-strip";
import { useConfirm } from "@/hooks/context/confirm";
import { useUserProfile } from "@/hooks/context/user";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import { setAddEmailIntent } from "@/lib/addEmailIntent";
import { useCurrentAgency } from "@/lib/api/hooks/app/agency/useAgency";
import { useAgencyOverview, useOpenClient } from "@/lib/api/hooks/app/agency/useAgencyHome";
import {
    useAgencyMailboxes,
    useAgencyVendorForClient,
    useAgencyVendors,
    useDeleteAgencyVendor,
} from "@/lib/api/hooks/app/agency/useAgencyMailboxes";
import type { AgencyMailbox, AgencyMailboxFilters, AgencyVendorAccount } from "@/lib/api/models/app/agency/AgencyMailboxes";
import { vendorLabel } from "@/lib/api/models/app/emails/MailboxSources";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

const chip = (active: boolean) =>
    cn(
        "h-7 px-2.5 rounded-md text-[12px] inline-flex items-center gap-1.5 whitespace-nowrap border transition-colors",
        active ? "bg-sky-50 text-sky-700 border-sky-200" : "border-slate-200 text-slate-600 hover:text-slate-900 hover:border-slate-300",
    );

const plural = (n: number, one: string, many: string) => `${n.toLocaleString()} ${n === 1 ? one : many}`;

const PROVIDERS: Record<string, string> = { gmail: "Google", outlook: "Microsoft", smtp_imap: "IMAP and SMTP" };
const HOLDS: Record<NonNullable<AgencyMailbox["warmup_state"]>, string> = {
    watch: "Warmup: watch",
    throttled: "Warmup: slowed",
    quarantined: "Warmup: paused",
    blocked: "Warmup: blocked",
};

/** What state a mailbox is in, worst first: the one thing someone has to act on. */
function standing(m: AgencyMailbox): { label: string; cls: string } {
    if (m.status === "revoked") return { label: "Disconnected", cls: "bg-red-50 text-red-700" };
    if (m.status !== "active") return { label: "Inactive", cls: "bg-slate-100 text-slate-600" };
    if (m.erroring) return { label: "Failing to sign in", cls: "bg-red-50 text-red-700" };
    if (m.warmup_state === "quarantined" || m.warmup_state === "blocked") return { label: HOLDS[m.warmup_state], cls: "bg-red-50 text-red-700" };
    if (m.warmup_state) return { label: HOLDS[m.warmup_state], cls: "bg-amber-50 text-amber-700" };
    return { label: "Connected", cls: "bg-emerald-50 text-emerald-700" };
}

/** A list that could not be loaded says so and offers another try, instead of looking empty. */
function LoadFailed({ what, error, onRetry }: { what: string; error: unknown; onRetry: () => void }) {
    return (
        <div role="alert" className="px-5 py-4 border-b border-slate-200/60 flex flex-wrap items-center gap-3">
            <p className="min-w-[220px] flex-1 text-[12px] leading-relaxed text-red-700">
                We could not load {what}. {buildError(error as AppError)}
            </p>
            <button
                type="button"
                onClick={onRetry}
                className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:border-slate-300 hover:text-slate-900"
            >
                Try again
            </button>
        </div>
    );
}

function VendorAccounts({
    accounts,
    failed,
    onRetry,
    canManage,
    onConnect,
    onAdd,
    onUpdate,
    onRemove,
}: {
    accounts: AgencyVendorAccount[];
    /** The accounts could not be loaded; nothing can be connected until they are. */
    failed: unknown;
    onRetry: () => void;
    canManage: boolean;
    onConnect: () => void;
    onAdd: (a: AgencyVendorAccount) => void;
    onUpdate: (a: AgencyVendorAccount) => void;
    onRemove: (a: AgencyVendorAccount) => void;
}) {
    return (
        <>
            <SectionBar label="Inbox vendor accounts" count={failed ? undefined : accounts.length}>
                {canManage && !failed && (
                    <TopbarAction variant="ghost" icon={<PlusIcon className="w-3 h-3" />} onClick={onConnect}>
                        Connect a vendor
                    </TopbarAction>
                )}
            </SectionBar>
            {failed ? (
                <LoadFailed what="your vendor accounts" error={failed} onRetry={onRetry} />
            ) : accounts.length === 0 ? (
                <div className="px-5 py-4 border-b border-slate-200/60">
                    <p className="text-[12px] leading-relaxed text-slate-500 max-w-[80ch]">
                        Bought mailboxes from Zapmail, InboxKit, Mailforge or another vendor for your clients? Connect the account once and hand
                        its mailboxes to any client, with no key to paste in each workspace.
                        {!canManage && " An owner or admin of the agency connects it."}
                    </p>
                </div>
            ) : (
                accounts.map((a) => (
                    <div key={a.id} className="group min-h-12 px-5 py-2 flex items-center gap-3 border-b border-slate-200/60">
                        <ProviderLogo id={a.vendor} size="lg" title={vendorLabel(a.vendor) || a.label} />
                        <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-2 min-w-0">
                                <span className="text-[12.5px] font-medium text-slate-900 truncate">{a.label}</span>
                                {a.label !== vendorLabel(a.vendor) && <span className="text-[11.5px] text-slate-500 truncate">{vendorLabel(a.vendor)}</span>}
                                {a.status === "invalid" && (
                                    <span className="h-[18px] px-1.5 rounded bg-red-50 text-red-700 text-[10.5px] font-medium inline-flex items-center shrink-0">
                                        Key refused
                                    </span>
                                )}
                            </div>
                            <div className="text-[11.5px] text-slate-500 truncate">
                                {a.clients.length === 0
                                    ? "Not used for a client yet"
                                    : `${plural(a.mailboxes, "mailbox", "mailboxes")} in ${a.clients.map((c) => c.name).join(", ")}`}
                            </div>
                            {a.status === "invalid" && (
                                <p className="text-[11.5px] text-red-700 leading-snug">{a.last_error || "The vendor no longer accepts this key."}</p>
                            )}
                        </div>
                        <button
                            type="button"
                            onClick={() => onAdd(a)}
                            disabled={a.status === "invalid"}
                            className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:border-slate-300 hover:text-slate-900 inline-flex items-center gap-1.5 disabled:opacity-60"
                        >
                            <PlusIcon className="w-3 h-3" />
                            Add to a client
                        </button>
                        {canManage && (
                            <PopoverMenu align="end">
                                <PopoverMenuTrigger asChild>
                                    <button
                                        type="button"
                                        aria-label={`More for ${a.label}`}
                                        className="size-7 rounded-md text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center"
                                    >
                                        <MoreHorizontalIcon className="w-3.5 h-3.5" />
                                    </button>
                                </PopoverMenuTrigger>
                                <PopoverMenuContent minWidth={190}>
                                    <PopoverMenuItem icon={<KeyRoundIcon className="w-3 h-3" />} onSelect={() => onUpdate(a)}>
                                        Rename or update key
                                    </PopoverMenuItem>
                                    <PopoverMenuSeparator />
                                    <PopoverMenuItem danger icon={<Trash2Icon className="w-3 h-3" />} onSelect={() => onRemove(a)}>
                                        Remove
                                    </PopoverMenuItem>
                                </PopoverMenuContent>
                            </PopoverMenu>
                        )}
                    </div>
                ))
            )}
        </>
    );
}

function AgencyMailboxes() {
    const { agency } = useCurrentAgency();
    const agencyId = agency?.id ?? "";
    const overview = useAgencyOverview(agency?.id);
    const vendors = useAgencyVendors(agency?.id);
    const user = useUserProfile();
    const confirm = useConfirm();
    const { open, pending } = useOpenClient();
    const giveAccount = useAgencyVendorForClient(agencyId);
    const removeAccount = useDeleteAgencyVendor(agencyId);

    const [search, setSearch] = React.useState("");
    const [clientFilter, setClientFilter] = React.useState<string[]>([]);
    const [attention, setAttention] = React.useState(false);
    const q = useDebouncedValue(search, 300);
    const filters = React.useMemo<AgencyMailboxFilters>(() => ({ clients: clientFilter, attention, q }), [clientFilter, attention, q]);
    const mailboxes = useAgencyMailboxes(agency?.id, filters);

    const [adding, setAdding] = React.useState<{ accountId?: string } | null>(null);
    const [vendorDialog, setVendorDialog] = React.useState<{ account?: AgencyVendorAccount } | null>(null);
    const [linkOpen, setLinkOpen] = React.useState(false);

    if (!agency) return null;
    const canManage = agency.role === "owner" || agency.role === "admin";
    const clients = overview.data?.clients ?? [];
    const accounts = vendors.data?.data ?? [];
    const rows = mailboxes.data?.pages.flatMap((p) => p.data) ?? [];
    const totals = overview.data?.totals;
    const filtered = attention || q !== "" || clientFilter.length > 0;
    const busy = pending || giveAccount.isPending;

    const toggleClient = (id: string) => setClientFilter((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]));

    // Every way in ends inside the client's own workspace, where the mailboxes live.
    const choose = async (c: AddMailboxesChoice) => {
        if (c.how === "link") {
            if (await open(c.orgId, () => undefined)) {
                setAdding(null);
                setLinkOpen(true);
            }
            return;
        }
        let vendorConnectionId: string | undefined;
        if (c.how === "vendor" && c.accountId) {
            try {
                vendorConnectionId = (await giveAccount.mutateAsync({ id: c.accountId, orgId: c.orgId })).id;
            } catch (e) {
                toast.error(buildError(e as AppError));
                return;
            }
        }
        if (!(await open(c.orgId, (nav) => nav({ to: "/app/emails" })))) return;
        setAdding(null);
        setAddEmailIntent({ view: c.how === "vendor" ? "vendor" : c.how === "file" ? "bulk" : "pick", vendorConnectionId });
        user.setAddEmail(true);
    };

    const askRemove = (a: AgencyVendorAccount) =>
        confirm.show(
            `Remove ${a.label}? The ${plural(a.mailboxes, "mailbox", "mailboxes")} it brought in stay connected in every client. They just stop picking up new passwords from ${vendorLabel(a.vendor)} on their own.`,
            async () => {
                try {
                    await removeAccount.mutateAsync(a.id);
                    toast.success("Vendor account removed");
                } catch (e) {
                    toast.error(buildError(e as AppError));
                }
            },
            undefined,
            { title: "Remove vendor account", confirmLabel: "Remove" },
        );

    return (
        <PageBody>
            <VendorAccounts
                accounts={accounts}
                failed={vendors.isError && !vendors.data ? vendors.error : null}
                onRetry={() => void vendors.refetch()}
                canManage={canManage}
                onConnect={() => setVendorDialog({})}
                onAdd={(a) => setAdding({ accountId: a.id })}
                onUpdate={(a) => setVendorDialog({ account: a })}
                onRemove={askRemove}
            />

            <SectionBar
                label="Mailboxes"
                count={totals?.mailboxes ?? undefined}
                meta={
                    totals && totals.mailbox_problems > 0 ? (
                        <span className="text-[11.5px] text-red-700">{plural(totals.mailbox_problems, "needs", "need")} attention</span>
                    ) : undefined
                }
            >
                <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={() => setAdding({})} disabled={clients.length === 0}>
                    Add mailboxes
                </TopbarAction>
            </SectionBar>
            <div className="px-5 py-2 border-b border-slate-200/60 flex flex-wrap items-center gap-2">
                <SearchInput value={search} onChange={setSearch} placeholder="Search addresses…" className="w-56" />
                <button type="button" aria-pressed={attention} onClick={() => setAttention((v) => !v)} className={chip(attention)}>
                    Needs attention
                </button>
                {clients.length > 1 && (
                    <ScrollStrip activeKey={clientFilter.join(",")} innerClassName="flex items-center gap-1.5">
                        <button type="button" aria-pressed={clientFilter.length === 0} data-active={clientFilter.length === 0} onClick={() => setClientFilter([])} className={chip(clientFilter.length === 0)}>
                            All clients
                        </button>
                        {clients.map((c) => {
                            const active = clientFilter.includes(c.organization_id);
                            return (
                                <button key={c.organization_id} type="button" aria-pressed={active} data-active={active} onClick={() => toggleClient(c.organization_id)} className={chip(active)}>
                                    <ClientAvatar name={c.name} avatar={c.avatar_url} size="sm" />
                                    {c.name}
                                </button>
                            );
                        })}
                    </ScrollStrip>
                )}
            </div>

            {mailboxes.isLoading ? (
                <div className="py-12 flex justify-center">
                    <Loader2Icon className="w-4 h-4 animate-spin text-slate-400" />
                </div>
            ) : mailboxes.isError && rows.length === 0 ? (
                <LoadFailed what="the mailboxes" error={mailboxes.error} onRetry={() => void mailboxes.refetch()} />
            ) : rows.length === 0 ? (
                <EmptyBlock
                    title={filtered ? "No mailbox matches" : "No mailboxes yet"}
                    body={
                        filtered
                            ? "Nothing matches these filters."
                            : "Add mailboxes for a client yourself, from a vendor account or a file, or send the client a link to connect its own."
                    }
                    cta={!filtered && clients.length > 0 ? <TopbarAction onClick={() => setAdding({})}>Add mailboxes</TopbarAction> : undefined}
                />
            ) : (
                <div>
                    <div className="hidden md:flex h-8 px-5 items-center gap-3 border-b border-slate-200/60 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                        <span className="flex-1">Mailbox</span>
                        <span className="w-44">Client</span>
                        <span className="w-36">State</span>
                        <span className="w-20">Warmup</span>
                        <span className="w-24 text-right">Daily limit</span>
                    </div>
                    {rows.map((m) => {
                        const s = standing(m);
                        const go = () => void open(m.organization_id, (nav) => nav({ to: "/app/emails", search: { mailbox: m.id } }));
                        return (
                            <div
                                key={m.id}
                                role="button"
                                tabIndex={0}
                                onClick={go}
                                onKeyDown={(e) => {
                                    if ((e.key === "Enter" || e.key === " ") && e.target === e.currentTarget) {
                                        e.preventDefault();
                                        go();
                                    }
                                }}
                                title={`Open in ${m.client}`}
                                className="min-h-11 px-5 py-1.5 flex flex-wrap md:flex-nowrap items-center gap-x-3 gap-y-1 border-b border-slate-200/60 hover:bg-slate-50/80 cursor-pointer"
                            >
                                <span className="flex-1 min-w-[220px] flex items-center gap-2.5">
                                    <ProviderLogo id={m.vendor || m.provider} size="sm" title={m.vendor ? vendorLabel(m.vendor) : PROVIDERS[m.provider] ?? m.provider} />
                                    <span className="min-w-0">
                                        <span className="block truncate text-[12.5px] text-slate-900">{m.email}</span>
                                        <span className="block truncate text-[11px] text-slate-500">
                                            {[m.name, m.vendor ? `${vendorLabel(m.vendor)} · ${PROVIDERS[m.provider] ?? m.provider}` : PROVIDERS[m.provider] ?? m.provider]
                                                .filter(Boolean)
                                                .join(" · ")}
                                        </span>
                                    </span>
                                </span>
                                <span className="w-44 truncate text-[12px] text-slate-600">{m.client}</span>
                                <span className="w-36">
                                    <span className={cn("h-[18px] px-1.5 rounded text-[10.5px] font-medium inline-flex items-center", s.cls)}>{s.label}</span>
                                </span>
                                <span className="w-20 text-[12px] text-slate-600">{m.warmup ? "On" : "Off"}</span>
                                <span className="w-24 text-right text-[12px] tabular-nums text-slate-600">{m.campaign_limit.toLocaleString()}</span>
                            </div>
                        );
                    })}
                    {mailboxes.hasNextPage && (
                        <div className="p-3 flex justify-center">
                            <button
                                type="button"
                                disabled={mailboxes.isFetchingNextPage}
                                onClick={() => void mailboxes.fetchNextPage()}
                                className="h-7 px-3 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:border-slate-300 disabled:opacity-60 inline-flex items-center gap-1.5"
                            >
                                {mailboxes.isFetchingNextPage && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                Load more
                            </button>
                        </div>
                    )}
                </div>
            )}

            <AddMailboxesDialog
                open={!!adding}
                onClose={() => setAdding(null)}
                clients={clients}
                accounts={accounts}
                presetAccountId={adding?.accountId}
                busy={busy}
                onChoose={(c) => void choose(c)}
            />
            <VendorAccountDialog
                open={!!vendorDialog}
                onClose={() => setVendorDialog(null)}
                agencyId={agencyId}
                catalog={vendors.data?.catalog ?? []}
                account={vendorDialog?.account}
            />
            <ConnectLinksDialog open={linkOpen} onClose={() => setLinkOpen(false)} />
        </PageBody>
    );
}

export default function AgencyMailboxesPage() {
    return (
        <AgencyPage title="Mailboxes">
            <AgencyMailboxes />
        </AgencyPage>
    );
}
