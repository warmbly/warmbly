// Cross-client inbox: conversations from every client workspace whose inbox
// the caller reads, newest first. A thread opens read-only here; replying
// switches into that client's own inbox.

import React from "react";
import { ArrowLeftIcon, ChevronDownIcon, Loader2Icon, ReplyIcon } from "lucide-react";
import { Page, PageTopbar, EmptyBlock } from "@/components/layout/Page";
import { SearchInput } from "@/components/ui/field";
import ScrollStrip from "@/components/ui/scroll-strip";
import EmailBody from "@/components/app/unibox/EmailBody";
import { ClientAvatar } from "@/components/app/agency/home/AgencyHome";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import { useAgencyClients, useCurrentAgency } from "@/lib/api/hooks/app/agency/useAgency";
import {
    useAgencyInbox,
    useAgencyInboxMessage,
    useAgencyInboxThread,
    useAgencyLive,
    useOpenClient,
} from "@/lib/api/hooks/app/agency/useAgencyHome";
import type { AgencyInboxFilters, AgencyInboxThread } from "@/lib/api/models/app/agency/AgencyHome";
import type { UniboxThreadMessage } from "@/lib/api/models/app/unibox/UniboxThread";
import timeAgo from "@/lib/helper/timeAgo";
import { cn } from "@/lib/utils";

function senderLabel(addr?: string) {
    if (!addr) return "Unknown sender";
    const name = addr.replace(/<[^>]*>/, "").replace(/"/g, "").trim();
    return name || addr.replace(/[<>]/g, "");
}

type Toggle = "unread" | "needsReply" | "positive";
const TOGGLES: { key: Toggle; label: string }[] = [
    { key: "unread", label: "Unread" },
    { key: "needsReply", label: "Needs reply" },
    { key: "positive", label: "Positive" },
];

const chip = (active: boolean) =>
    cn(
        "h-7 px-2.5 rounded-md text-[12px] inline-flex items-center gap-1.5 whitespace-nowrap border transition-colors",
        active ? "bg-sky-50 text-sky-700 border-sky-200" : "border-slate-200 text-slate-600 hover:text-slate-900 hover:border-slate-300",
    );

export default function AgencyInboxPage() {
    const { agency } = useCurrentAgency();
    const clients = useAgencyClients(agency?.id);
    const [clientFilter, setClientFilter] = React.useState<string[]>([]);
    const [toggles, setToggles] = React.useState<Record<Toggle, boolean>>({ unread: false, needsReply: false, positive: false });
    const [search, setSearch] = React.useState("");
    const q = useDebouncedValue(search, 300);
    const filters: AgencyInboxFilters = React.useMemo(
        () => ({ clients: clientFilter, unread: toggles.unread, needsReply: toggles.needsReply, positive: toggles.positive, q }),
        [clientFilter, toggles, q],
    );
    const inbox = useAgencyInbox(agency?.id, filters);
    const [selected, setSelected] = React.useState<AgencyInboxThread | null>(null);
    useAgencyLive(agency?.id);

    React.useEffect(() => {
        if (!selected) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape" && !document.querySelector("[data-floating], [role='alertdialog']")) setSelected(null);
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [selected]);

    // The agency shell renders this only once there is an agency.
    if (!agency) return null;

    const byOrg = new Map((clients.data ?? []).map((c) => [c.organization_id, c]));
    const threads = inbox.data?.pages.flatMap((p) => p.data) ?? [];
    const toggleClient = (id: string) =>
        setClientFilter((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]));

    return (
        <Page className="h-full">
            <PageTopbar eyebrow="Inbox" subtitle={agency.name} />
            <div className="flex-1 min-h-0 flex">
                <div className={cn("w-full md:w-[400px] lg:w-[440px] shrink-0 flex flex-col min-h-0 md:border-r border-slate-200", selected && "max-md:hidden")}>
                    <div className="px-3 py-2 border-b border-slate-200 space-y-2 shrink-0">
                        <SearchInput value={search} onChange={setSearch} placeholder="Search every client's inbox" />
                        <div className="flex items-center gap-1.5">
                            {TOGGLES.map((t) => (
                                <button
                                    key={t.key}
                                    type="button"
                                    aria-pressed={toggles[t.key]}
                                    onClick={() => setToggles((cur) => ({ ...cur, [t.key]: !cur[t.key] }))}
                                    className={chip(toggles[t.key])}
                                >
                                    {t.label}
                                </button>
                            ))}
                        </div>
                        {(clients.data?.length ?? 0) > 1 && (
                            <ScrollStrip activeKey={clientFilter.join(",")} innerClassName="flex items-center gap-1.5">
                                <button type="button" aria-pressed={clientFilter.length === 0} data-active={clientFilter.length === 0} onClick={() => setClientFilter([])} className={chip(clientFilter.length === 0)}>
                                    All clients
                                </button>
                                {(clients.data ?? []).map((c) => {
                                    const active = clientFilter.includes(c.organization_id);
                                    return (
                                        <button
                                            key={c.organization_id}
                                            type="button"
                                            aria-pressed={active}
                                            data-active={active}
                                            onClick={() => toggleClient(c.organization_id)}
                                            className={chip(active)}
                                        >
                                            <ClientAvatar name={c.name} avatar={c.avatar_url} size="sm" />
                                            {c.name}
                                        </button>
                                    );
                                })}
                            </ScrollStrip>
                        )}
                    </div>
                    <div className="flex-1 min-h-0 overflow-auto">
                        {inbox.isLoading ? (
                            <div className="py-12 flex justify-center"><Loader2Icon className="w-4 h-4 animate-spin text-slate-400" /></div>
                        ) : threads.length === 0 ? (
                            <EmptyBlock
                                title="No conversations"
                                body={
                                    filters.unread || filters.needsReply || filters.positive || filters.q || filters.clients.length
                                        ? "Nothing matches these filters."
                                        : "Mail from the inboxes of clients you can read shows up here."
                                }
                            />
                        ) : (
                            threads.map((t) => {
                                const client = byOrg.get(t.organization_id);
                                const active = selected?.id === t.id && selected.organization_id === t.organization_id;
                                return (
                                    <button
                                        key={`${t.organization_id}-${t.id}`}
                                        type="button"
                                        onClick={() => setSelected(t)}
                                        className={cn(
                                            "w-full text-left px-4 py-2.5 border-b border-slate-200/60 flex gap-2.5 transition-colors",
                                            active ? "bg-sky-50/70" : "hover:bg-slate-50/80",
                                        )}
                                    >
                                        <span className={cn("mt-1.5 size-1.5 rounded-full shrink-0", t.has_unread ? "bg-sky-500" : "bg-transparent")} />
                                        <span className="flex-1 min-w-0">
                                            <span className="flex items-center gap-2">
                                                <span className={cn("flex-1 min-w-0 truncate text-[12.5px]", t.has_unread ? "font-semibold text-slate-900" : "text-slate-700")}>
                                                    {senderLabel(t.from_addr[0])}
                                                    {t.message_count > 1 && <span className="ml-1 text-[11px] text-slate-400 font-normal">{t.message_count}</span>}
                                                </span>
                                                <span className="text-[11px] text-slate-400 shrink-0">{timeAgo(t.internal_date)}</span>
                                            </span>
                                            <span className={cn("block truncate text-[12px]", t.has_unread ? "text-slate-800" : "text-slate-600")}>{t.subject || "(no subject)"}</span>
                                            <span className="block truncate text-[11.5px] text-slate-400">{t.snippet}</span>
                                            <span className="mt-1 inline-flex items-center gap-1.5 h-5 px-1.5 rounded bg-slate-100 text-[10.5px] text-slate-600 max-w-full">
                                                <ClientAvatar name={client?.name ?? "?"} avatar={client?.avatar_url} size="sm" />
                                                <span className="truncate">{client?.name ?? "Client"}</span>
                                            </span>
                                        </span>
                                    </button>
                                );
                            })
                        )}
                        {inbox.hasNextPage && (
                            <div className="p-3 flex justify-center">
                                <button
                                    type="button"
                                    disabled={inbox.isFetchingNextPage}
                                    onClick={() => void inbox.fetchNextPage()}
                                    className="h-7 px-3 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:border-slate-300 disabled:opacity-60 inline-flex items-center gap-1.5"
                                >
                                    {inbox.isFetchingNextPage && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                    Load more
                                </button>
                            </div>
                        )}
                    </div>
                </div>
                <div className={cn("flex-1 min-w-0 min-h-0 flex flex-col", !selected && "max-md:hidden")}>
                    {selected ? (
                        <ThreadPane
                            agencyId={agency.id}
                            thread={selected}
                            clientName={byOrg.get(selected.organization_id)?.name ?? "the client"}
                            clientAvatar={byOrg.get(selected.organization_id)?.avatar_url}
                            onBack={() => setSelected(null)}
                        />
                    ) : (
                        <div className="flex-1 flex items-center justify-center px-6 text-center text-[12px] text-slate-400">
                            Select a conversation to read it. Reading here leaves it unread for the client's team.
                        </div>
                    )}
                </div>
            </div>
        </Page>
    );
}

function ThreadPane({
    agencyId,
    thread,
    clientName,
    clientAvatar,
    onBack,
}: {
    agencyId: string;
    thread: AgencyInboxThread;
    clientName: string;
    clientAvatar?: string;
    onBack: () => void;
}) {
    const threadKey = thread.thread_id || thread.id;
    const messages = useAgencyInboxThread(agencyId, thread.organization_id, thread.thread_id ? thread.thread_id : undefined);
    const { open, pending } = useOpenClient();
    // A message with no thread is its own conversation.
    const list: UniboxThreadMessage[] = thread.thread_id
        ? [...(messages.data?.data ?? [])].sort((a, b) => new Date(a.internal_date).getTime() - new Date(b.internal_date).getTime())
        : [{ ...thread, answers_mailbox_id: undefined }];
    const lastId = list[list.length - 1]?.id;

    const reply = () =>
        void open(thread.organization_id, (nav) =>
            nav({ to: "/app/unibox/{-$scope}/{-$threadId}", params: { scope: "all", threadId: threadKey } }),
        );

    return (
        <>
            <div className="shrink-0 px-4 py-2.5 border-b border-slate-200 flex items-center gap-2 flex-wrap">
                <button type="button" onClick={onBack} className="md:hidden h-7 px-2 -ml-2 rounded-md text-[12px] text-slate-600 hover:bg-slate-100 inline-flex items-center gap-1">
                    <ArrowLeftIcon className="w-3 h-3" /> Inbox
                </button>
                <div className="flex-1 min-w-0">
                    <div className="text-[13px] font-medium text-slate-900 truncate">{thread.subject || "(no subject)"}</div>
                    <div className="mt-0.5 flex items-center gap-1.5 text-[11px] text-slate-500">
                        <ClientAvatar name={clientName} avatar={clientAvatar} size="sm" />
                        {clientName}
                    </div>
                </div>
                <button
                    type="button"
                    onClick={reply}
                    disabled={pending}
                    className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 disabled:opacity-60"
                >
                    {pending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <ReplyIcon className="w-3 h-3" />}
                    Reply in {clientName}
                </button>
            </div>
            <div className="flex-1 min-h-0 overflow-auto">
                {messages.isLoading && thread.thread_id ? (
                    <div className="py-12 flex justify-center"><Loader2Icon className="w-4 h-4 animate-spin text-slate-400" /></div>
                ) : messages.isError ? (
                    <p className="px-4 py-6 text-[12px] text-slate-500">This conversation could not be loaded. It may have moved, or your access to this client changed.</p>
                ) : (
                    list.map((m) => (
                        <MessageItem key={m.id} agencyId={agencyId} organizationId={thread.organization_id} message={m} defaultOpen={m.id === lastId} />
                    ))
                )}
            </div>
        </>
    );
}

function MessageItem({
    agencyId,
    organizationId,
    message,
    defaultOpen,
}: {
    agencyId: string;
    organizationId: string;
    message: UniboxThreadMessage;
    defaultOpen: boolean;
}) {
    const [open, setOpen] = React.useState(defaultOpen);
    const body = useAgencyInboxMessage(agencyId, organizationId, open ? message.id : undefined);
    return (
        <div className="border-b border-slate-200/60">
            <button type="button" aria-expanded={open} onClick={() => setOpen((v) => !v)} className="w-full text-left px-4 py-2.5 flex items-start gap-2 hover:bg-slate-50/60">
                <span className="flex-1 min-w-0">
                    <span className="flex items-center gap-2">
                        <span className="flex-1 min-w-0 truncate text-[12.5px] font-medium text-slate-900">{senderLabel(message.from_addr[0])}</span>
                        <span className="text-[11px] text-slate-400 shrink-0">{timeAgo(message.internal_date)}</span>
                    </span>
                    {!open && <span className="block truncate text-[11.5px] text-slate-500">{message.snippet}</span>}
                </span>
                <ChevronDownIcon className={cn("w-3.5 h-3.5 text-slate-400 mt-0.5 transition-transform", open && "rotate-180")} />
            </button>
            {open && (
                <div className="px-4 pb-4">
                    {body.isLoading ? (
                        <div className="py-4 flex justify-center"><Loader2Icon className="w-4 h-4 animate-spin text-slate-400" /></div>
                    ) : body.data ? (
                        <EmailBody html={body.data.body_html} plain={body.data.body_plain} blockRemote />
                    ) : (
                        <p className="text-[12px] text-slate-600 whitespace-pre-wrap">{message.snippet}</p>
                    )}
                </div>
            )}
        </div>
    );
}
