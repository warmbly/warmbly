import { AgencyPage } from "@/components/app/agency/AgencyPage";
import React from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowUpRightIcon, MoreHorizontalIcon, PlusIcon, UnlinkIcon, GaugeIcon, WalletIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { EmptyBlock, PageBody, SectionBar, TopbarAction } from "@/components/layout/Page";
import { NumberInput } from "@/components/ui/field";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuSeparator, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import { useAppStore } from "@/stores";
import useSwitchOrganization from "@/lib/api/hooks/app/organizations/useSwitchOrganization";
import { organizationsQuery } from "@/lib/api/hooks/app/organizations/useOrganizations";
import {
    useAgencyAIBalance,
    useAgencyClients,
    useAgencyUsage,
    useCurrentAgency,
    useDetachAgencyClient,
    useUpdateAgencyClientAILimit,
    useUpdateAgencyClientLimit,
} from "@/lib/api/hooks/app/agency/useAgency";
import type { AgencyClient } from "@/lib/api/models/app/agency/Agency";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { AddClientDialog } from "@/components/app/agency/AddClientDialog";
import { formatNumber } from "@/components/app/agency/styles";
import { AddBalanceDialog } from "@/components/app/agency/AddBalanceDialog";
import { fmtUsd, roundCents } from "@/lib/money";

function LimitCell({ client, agencyId, canManage }: { client: AgencyClient; agencyId: string; canManage: boolean }) {
    const update = useUpdateAgencyClientLimit(agencyId);
    const [editing, setEditing] = React.useState(false);
    const [value, setValue] = React.useState(client.monthly_send_limit ?? 10_000);
    const limit = client.monthly_send_limit;

    const save = (next: number | null) => {
        setEditing(false);
        update.mutate({ orgId: client.organization_id, limit: next }, { onError: (e) => toast.error(buildError(e as unknown as AppError)) });
    };

    if (!editing) {
        return (
            <button
                type="button"
                disabled={!canManage}
                onClick={(e) => {
                    e.stopPropagation();
                    setValue(limit ?? 10_000);
                    setEditing(true);
                }}
                className="h-7 px-2 rounded-md text-[12px] tabular-nums text-slate-600 hover:bg-slate-100 disabled:hover:bg-transparent disabled:cursor-default"
            >
                {limit === null || limit === undefined ? "No cap" : `${formatNumber(limit)} / month`}
            </button>
        );
    }
    return (
        <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
            <NumberInput value={value} onChange={setValue} min={0} max={100_000_000} step={1000} className="w-32" />
            <button type="button" onClick={() => save(value)} className="h-7 px-2 rounded-md bg-slate-900 text-white text-[12px]">
                Save
            </button>
            <button type="button" onClick={() => save(null)} className="h-7 px-2 rounded-md text-[12px] text-slate-600 hover:bg-slate-100">
                No cap
            </button>
            <button
                type="button"
                onClick={() => setEditing(false)}
                onKeyDown={(e) => e.key === "Escape" && setEditing(false)}
                className="h-7 px-2 rounded-md text-[12px] text-slate-400 hover:text-slate-700"
            >
                Cancel
            </button>
        </div>
    );
}

function AILimitCell({ client, agencyId, canManage }: { client: AgencyClient; agencyId: string; canManage: boolean }) {
    const update = useUpdateAgencyClientAILimit(agencyId);
    const [editing, setEditing] = React.useState(false);
    const [value, setValue] = React.useState(client.monthly_ai_limit_usd ?? 10);
    const limit = client.monthly_ai_limit_usd;

    const save = (next: number | null) => {
        setEditing(false);
        update.mutate(
            { orgId: client.organization_id, limitUsd: next === null ? null : roundCents(next) },
            { onError: (e) => toast.error(buildError(e as unknown as AppError)) },
        );
    };

    if (!editing) {
        return (
            <button
                type="button"
                disabled={!canManage}
                onClick={(e) => {
                    e.stopPropagation();
                    setValue(limit ?? 10);
                    setEditing(true);
                }}
                className="h-7 px-2 rounded-md text-[12px] tabular-nums text-slate-600 hover:bg-slate-100 disabled:hover:bg-transparent disabled:cursor-default"
            >
                {limit === null || limit === undefined ? "No AI cap" : `${fmtUsd(limit)} AI / month`}
            </button>
        );
    }
    return (
        <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
            <NumberInput value={value} onChange={setValue} min={0} max={50_000} step={5} suffix="USD" className="w-28" />
            <button type="button" onClick={() => save(value)} className="h-7 px-2 rounded-md bg-slate-900 text-white text-[12px]">
                Save
            </button>
            <button type="button" onClick={() => save(null)} className="h-7 px-2 rounded-md text-[12px] text-slate-600 hover:bg-slate-100">
                No cap
            </button>
            <button
                type="button"
                onClick={() => setEditing(false)}
                onKeyDown={(e) => e.key === "Escape" && setEditing(false)}
                className="h-7 px-2 rounded-md text-[12px] text-slate-400 hover:text-slate-700"
            >
                Cancel
            </button>
        </div>
    );
}

function AgencyClients() {
    const { agency } = useCurrentAgency();
    const agencyId = agency?.id ?? "";
    const clients = useAgencyClients(agency?.id);
    const usage = useAgencyUsage(agency?.id);
    const ai = useAgencyAIBalance(agency?.id);
    const metered = !!ai.data && !ai.data.unmetered;
    const [funding, setFunding] = React.useState<AgencyClient | null>(null);
    const detach = useDetachAgencyClient(agencyId);
    const confirm = useConfirm();
    const navigate = useNavigate();
    const qc = useQueryClient();
    const switchOrg = useSwitchOrganization();
    const organizations = useAppStore((s) => s.organizations);
    const setCurrentOrganization = useAppStore((s) => s.setCurrentOrganization);
    const [adding, setAdding] = React.useState(false);
    const canManage = agency?.role === "owner" || agency?.role === "admin";

    const used = new Map((usage.data?.clients ?? []).map((c) => [c.organization_id, c.used]));
    const list = clients.data ?? [];

    const open = async (orgId: string) => {
        try {
            await switchOrg.mutateAsync(orgId);
            // A client created a moment ago may not be in the cached list yet.
            const org =
                organizations.find((o) => o.id === orgId) ??
                (await qc.fetchQuery({ ...organizationsQuery, staleTime: 0 })).find((o) => o.id === orgId);
            if (org) setCurrentOrganization(org);
            navigate({ to: "/app/emails" });
        } catch (e) {
            toast.error(buildError(e as unknown as AppError));
        }
    };

    const askDetach = (c: AgencyClient) =>
        confirm.show(
            `Remove ${c.name} from the agency? The workspace keeps its mailboxes, contacts and campaigns, but loses the agency's plan, and your staff lose access to it.`,
            async () => {
                try {
                    await detach.mutateAsync(c.organization_id);
                    toast.success("Client removed");
                } catch (e) {
                    toast.error(buildError(e as unknown as AppError));
                }
            },
            undefined,
            { title: "Remove client", confirmLabel: "Remove" },
        );

    if (!agency) return null;

    return (
        <PageBody>
            <SectionBar label="Clients" count={list.length}>
                {canManage && (
                    <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={() => setAdding(true)}>
                        Add client
                    </TopbarAction>
                )}
            </SectionBar>
            {clients.isLoading ? null : list.length === 0 ? (
                <EmptyBlock
                    title="No clients yet"
                    body="Each client gets its own workspace: its own mailboxes, contacts, campaigns and inbox. Add one to start."
                    cta={canManage ? <TopbarAction onClick={() => setAdding(true)}>Add client</TopbarAction> : undefined}
                />
            ) : (
                <div>
                    <div className="hidden md:flex h-8 px-5 items-center gap-3 border-b border-slate-200/60 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                        <span className="flex-1">Workspace</span>
                        <span className="w-32 text-right">Sent this period</span>
                        <span className="w-64">Monthly cap</span>
                        {metered && <span className="w-60">AI cap</span>}
                        <span className="w-7" />
                    </div>
                    {list.map((c) => (
                        <div
                            key={c.organization_id}
                            role="button"
                            tabIndex={0}
                            onClick={() => void open(c.organization_id)}
                            onKeyDown={(e) => {
                                if (e.key === "Enter" && e.target === e.currentTarget) void open(c.organization_id);
                            }}
                            className="group min-h-11 px-5 py-1.5 flex flex-wrap md:flex-nowrap items-center gap-x-3 gap-y-1 border-b border-slate-200/60 hover:bg-slate-50/80 cursor-pointer"
                        >
                            <span className="flex-1 min-w-0 text-[12.5px] text-slate-900 font-medium truncate">{c.name}</span>
                            <span className="w-32 text-right text-[12px] tabular-nums text-slate-600">{formatNumber(used.get(c.organization_id) ?? 0)}</span>
                            <span className="w-64">
                                <LimitCell client={c} agencyId={agencyId} canManage={canManage} />
                            </span>
                            {metered && (
                                <span className="w-60">
                                    <AILimitCell client={c} agencyId={agencyId} canManage={canManage} />
                                </span>
                            )}
                            <PopoverMenu align="end">
                                <PopoverMenuTrigger asChild>
                                    <button
                                        type="button"
                                        aria-label={`Actions for ${c.name}`}
                                        className="size-7 rounded-md text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100 focus:opacity-100"
                                    >
                                        <MoreHorizontalIcon className="w-3.5 h-3.5" />
                                    </button>
                                </PopoverMenuTrigger>
                                <PopoverMenuContent minWidth={200}>
                                    <PopoverMenuItem icon={<ArrowUpRightIcon className="w-3 h-3" />} onSelect={() => void open(c.organization_id)}>
                                        Open workspace
                                    </PopoverMenuItem>
                                    <PopoverMenuItem icon={<GaugeIcon className="w-3 h-3" />} onSelect={() => navigate({ to: "/app/agency/usage" })}>
                                        Usage
                                    </PopoverMenuItem>
                                    {canManage && metered && (
                                        <PopoverMenuItem icon={<WalletIcon className="w-3 h-3" />} onSelect={() => setFunding(c)}>
                                            Add balance
                                        </PopoverMenuItem>
                                    )}
                                    {canManage && (
                                        <>
                                            <PopoverMenuSeparator />
                                            <PopoverMenuItem danger icon={<UnlinkIcon className="w-3 h-3" />} onSelect={() => askDetach(c)}>
                                                Remove from agency
                                            </PopoverMenuItem>
                                        </>
                                    )}
                                </PopoverMenuContent>
                            </PopoverMenu>
                        </div>
                    ))}
                </div>
            )}
            <AddClientDialog open={adding} onClose={() => setAdding(false)} agencyId={agencyId} clients={list} />
            <AddBalanceDialog agencyId={agencyId} client={funding} available={ai.data?.balance_usd ?? 0} onClose={() => setFunding(null)} />
        </PageBody>
    );
}

export default function AgencyClientsPage() {
    return (
        <AgencyPage title="Clients">
            <AgencyClients />
        </AgencyPage>
    );
}
