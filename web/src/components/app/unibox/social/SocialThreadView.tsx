// One social conversation in the unibox: the messages, a reply box, and the contact it belongs to.
import React from "react";
import toast from "react-hot-toast/headless";
import { AlertCircleIcon, ClockIcon, Loader2Icon, RotateCwIcon, SendIcon, UserIcon, XIcon } from "lucide-react";
import {
    PENDING_PREFIX,
    useDiscardPendingSocialMessage,
    useMarkSocialRead,
    useSendSocialMessage,
    useSocialAccounts,
    useSocialConversation,
    useSocialMessages,
} from "@/lib/api/hooks/app/social/useSocial";
import type { SocialMessage } from "@/lib/api/models/app/social/Social";
import useContact from "@/lib/api/hooks/app/contacts/useContact";
import { usePresenceResource } from "@/hooks/PresenceProvider";
import ResourceViewers from "@/components/app/presence/ResourceViewers";
import { useUserProfile } from "@/hooks/context/user";
import { useAccessRestricted, usePermission } from "@/hooks/usePermission";
import { useMediaQuery, LG_QUERY } from "@/hooks/useMediaQuery";
import { useAppStore } from "@/stores";
import ChannelMark from "@/components/app/social/ChannelMark";
import { Avatar } from "@/components/app/social/parts";
import { SocialProfileLink } from "@/components/app/social/SocialLinks";
import { channelLabel } from "@/lib/social/channels";
import { socialErrorMessage } from "@/lib/social/errors";
import { cn } from "@/lib/utils";
import ContactContextPanel from "../ContactContextPanel";

const newKey = () => (typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`);

const KIND_LABEL: Record<string, string> = { inmail: "InMail", invite_note: "Invitation note" };

function stamp(v: Date | string | null | undefined): string {
    if (!v) return "";
    const d = new Date(v);
    const sameDay = d.toDateString() === new Date().toDateString();
    return sameDay ? d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" }) : d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}

export function SocialThreadView({ conversationId, onClose }: { conversationId: string; onClose: () => void }) {
    const conversation = useSocialConversation(conversationId);
    const messagesQ = useSocialMessages(conversationId);
    const accounts = useSocialAccounts();
    const markRead = useMarkSocialRead();
    const send = useSendSocialMessage(conversationId);
    const discard = useDiscardPendingSocialMessage(conversationId);
    const { user } = useUserProfile();
    const restricted = useAccessRestricted();
    const canReply = usePermission("ACCESS_UNIBOX") && !restricted;
    const [draft, setDraft] = React.useState("");
    // Why the last reply was refused, by its temporary id.
    const [failures, setFailures] = React.useState<Record<string, string>>({});

    usePresenceResource(`social:${conversationId}`, draft.trim() ? "replying" : "viewing");

    const c = conversation.data;
    const account = accounts.data?.data.find((a) => a.id === c?.account_id);
    const network = c ? channelLabel(c.channel) : "";
    const name = c?.participant.full_name || c?.participant.handle || `${network || "Social"} contact`;

    // Opening marks it read once; a later message arriving while it is open is marked as it lands.
    const unread = c?.unread_count ?? 0;
    const markReadMutate = markRead.mutate;
    React.useEffect(() => {
        if (unread > 0) markReadMutate(conversationId);
    }, [unread, conversationId, markReadMutate]);

    // Pages come newest first; the thread reads oldest to newest.
    const messages = React.useMemo(() => (messagesQ.data?.pages.flatMap((p) => p.data) ?? []).slice().reverse(), [messagesQ.data]);

    const scroller = React.useRef<HTMLDivElement>(null);
    const newest = messages[messages.length - 1]?.id;
    React.useEffect(() => {
        const el = scroller.current;
        if (el) el.scrollTop = el.scrollHeight;
    }, [newest]);

    const submit = (body: string, retryOf?: string) => {
        const text = body.trim();
        if (!text || !canReply) return;
        if (retryOf) {
            discard(retryOf);
            setFailures(({ [retryOf]: _, ...rest }) => rest);
        }
        const tempId = `${PENDING_PREFIX}${newKey()}`;
        send.mutate(
            { body: text, idempotencyKey: newKey(), tempId, userId: user?.id },
            { onError: (e) => setFailures((f) => ({ ...f, [tempId]: socialErrorMessage(e) })) },
        );
        if (!retryOf) setDraft("");
    };

    // The contact panel follows the email unibox's remembered preference.
    const isWide = useMediaQuery(LG_QUERY);
    const railPref = useAppStore((s) => s.uniboxContactRailOpen);
    const setRailPref = useAppStore((s) => s.setUniboxContactRailOpen);
    const [overlayOpen, setOverlayOpen] = React.useState(false);
    const contactOpen = isWide ? railPref : overlayOpen;
    const setContactOpen = (open: boolean) => (isWide ? setRailPref(open) : setOverlayOpen(open));
    const contact = useContact(c?.contact_id ?? "", !!c?.contact_id);
    const contactEmail = contact.data?.email || undefined;

    if (conversation.isError && !c) {
        return (
            <div className="flex flex-1 items-center justify-center px-5 text-center">
                <div>
                    <p className="text-[12.5px] font-medium text-slate-700">This conversation could not be opened</p>
                    <p className="mt-1 text-[11.5px] text-slate-500">{socialErrorMessage(conversation.error)}</p>
                </div>
            </div>
        );
    }

    return (
        <div className="flex h-full min-h-0 min-w-0 overflow-hidden">
            <div className="flex min-h-0 min-w-0 flex-1 flex-col bg-white">
                <div className="min-h-12 shrink-0 border-b border-slate-200 px-4 py-2 sm:px-5 flex items-center gap-3">
                    <span className="relative shrink-0">
                        <Avatar src={c?.participant.picture_url} name={name} size={32} />
                        {c && (
                            <span className="absolute -bottom-0.5 -right-0.5 rounded-[4px] bg-white p-px">
                                <ChannelMark channel={c.channel} className="size-3" />
                            </span>
                        )}
                    </span>
                    <div className="min-w-0 flex-1">
                        <div className="flex min-w-0 items-center gap-1.5">
                            <h1 className="truncate text-[13.5px] font-semibold text-slate-900">{c ? name : "Loading…"}</h1>
                            {c && <SocialProfileLink channel={c.channel} handle={c.participant.profile_url || c.participant.handle} label={name} className="shrink-0" />}
                            {c?.kind === "inmail" && <span className="shrink-0 rounded bg-amber-50 px-1 text-[10px] font-medium text-amber-700 ring-1 ring-amber-200">InMail</span>}
                        </div>
                        <div className="flex min-w-0 items-center gap-1.5 text-[11px] text-slate-400">
                            {(c?.participant.headline || c?.participant.handle) && <span className="min-w-0 truncate">{c?.participant.headline || c?.participant.handle}</span>}
                            {account && (
                                <>
                                    {(c?.participant.headline || c?.participant.handle) && <span aria-hidden className="text-slate-300">&middot;</span>}
                                    <span className="shrink-0 truncate">via {account.display_name}</span>
                                </>
                            )}
                            <ResourceViewers resource={`social:${conversationId}`} className="ml-1 shrink-0" />
                        </div>
                    </div>
                    {c?.contact_id && !restricted && (
                        <button
                            type="button"
                            onClick={() => setContactOpen(!contactOpen)}
                            aria-label={contactOpen ? "Hide contact panel" : "Show contact panel"}
                            title={contactOpen ? "Hide contact" : "Show contact"}
                            className={cn("size-7 shrink-0 rounded-md inline-flex items-center justify-center transition-colors", contactOpen ? "bg-sky-50 text-sky-700" : "text-slate-500 hover:bg-slate-100 hover:text-slate-900")}
                        >
                            <UserIcon className="size-[15px]" />
                        </button>
                    )}
                    <button
                        type="button"
                        onClick={onClose}
                        aria-label="Close conversation"
                        className="hidden md:inline-flex size-7 shrink-0 rounded-md items-center justify-center text-slate-500 hover:bg-slate-100 hover:text-slate-900"
                    >
                        <XIcon className="size-[15px]" />
                    </button>
                </div>

                <div ref={scroller} className="flex-1 min-h-0 overflow-y-auto px-4 py-4 sm:px-5">
                    {messagesQ.hasNextPage && (
                        <div className="mb-3 flex justify-center">
                            <button
                                type="button"
                                onClick={() => void messagesQ.fetchNextPage()}
                                disabled={messagesQ.isFetchingNextPage}
                                className="h-7 rounded-md border border-slate-200 px-3 text-[11.5px] font-medium text-slate-600 hover:bg-slate-50 disabled:opacity-60"
                            >
                                {messagesQ.isFetchingNextPage ? "Loading…" : "Load earlier messages"}
                            </button>
                        </div>
                    )}
                    {messagesQ.isPending ? (
                        <div className="space-y-3" aria-busy aria-label="Loading messages">
                            <div className="h-10 w-2/3 rounded-lg bg-slate-100 animate-pulse" />
                            <div className="ml-auto h-10 w-1/2 rounded-lg bg-slate-100 animate-pulse" />
                        </div>
                    ) : messagesQ.isError ? (
                        <p className="text-center text-[12px] text-slate-500">{socialErrorMessage(messagesQ.error)}</p>
                    ) : messages.length === 0 ? (
                        <p className="text-center text-[12px] text-slate-400">No messages in this conversation yet.</p>
                    ) : (
                        <ol className="space-y-2.5">
                            {messages.map((m) => (
                                <li key={m.id}>
                                    <Bubble
                                        message={m}
                                        failure={failures[m.id]}
                                        onRetry={canReply ? () => submit(m.body, m.id) : undefined}
                                        onDiscard={() => {
                                            discard(m.id);
                                            setFailures(({ [m.id]: _, ...rest }) => rest);
                                        }}
                                    />
                                </li>
                            ))}
                        </ol>
                    )}
                </div>

                {canReply && c && (
                    <div className="shrink-0 border-t border-slate-200 px-4 py-3 sm:px-5">
                        <div className="rounded-md border border-slate-200 bg-white focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100">
                            <textarea
                                value={draft}
                                onChange={(e) => setDraft(e.target.value)}
                                onKeyDown={(e) => {
                                    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                                        e.preventDefault();
                                        submit(draft);
                                    }
                                }}
                                rows={3}
                                placeholder={`Reply to ${name} on ${network}`}
                                aria-label={`Reply on ${network}`}
                                className="block w-full resize-none rounded-t-md bg-transparent px-2.5 py-2 text-[12.5px] leading-relaxed text-slate-800 placeholder:text-slate-400 focus:outline-none"
                            />
                            <div className="flex items-center justify-between gap-2 border-t border-slate-100 px-2 py-1.5">
                                <span className="truncate text-[11px] text-slate-400">{account ? `Sends as ${account.display_name}` : "Sends from the account in this conversation"}</span>
                                <button
                                    type="button"
                                    onClick={() => submit(draft)}
                                    disabled={!draft.trim()}
                                    title="Send (Ctrl or Cmd + Enter)"
                                    className="h-7 shrink-0 rounded-md bg-sky-600 px-3 text-[12px] font-medium text-white inline-flex items-center gap-1.5 hover:bg-sky-700 disabled:opacity-50"
                                >
                                    <SendIcon className="size-3.5" />
                                    Send
                                </button>
                            </div>
                        </div>
                    </div>
                )}
            </div>

            {contactOpen && c?.contact_id && !restricted && (
                contactEmail ? (
                    <ContactContextPanel email={contactEmail} name={name} onClose={() => setContactOpen(false)} />
                ) : contact.isPending ? (
                    <div className="hidden lg:flex w-80 shrink-0 items-center justify-center border-l border-slate-200">
                        <Loader2Icon className="size-4 animate-spin text-slate-300" />
                    </div>
                ) : null
            )}
        </div>
    );
}

function Bubble({ message: m, failure, onRetry, onDiscard }: { message: SocialMessage; failure?: string; onRetry?: () => void; onDiscard: () => void }) {
    const outbound = m.direction === "outbound";
    const pending = m.status === "pending";
    const failed = m.status === "failed";
    const kind = KIND_LABEL[m.kind];
    const local = m.id.startsWith(PENDING_PREFIX);
    return (
        <div className={cn("flex flex-col", outbound ? "items-end" : "items-start")}>
            {kind && <span className="mb-0.5 px-1 text-[10px] font-medium uppercase tracking-[0.12em] text-slate-400">{kind}</span>}
            <div
                className={cn(
                    "max-w-[min(32rem,85%)] whitespace-pre-wrap break-words rounded-lg px-3 py-2 text-[12.5px] leading-relaxed",
                    outbound ? "bg-sky-600 text-white" : "bg-slate-100 text-slate-800",
                    pending && "opacity-70",
                    failed && "bg-rose-50 text-rose-900 ring-1 ring-rose-200",
                )}
            >
                {m.body}
            </div>
            <div className="mt-0.5 flex items-center gap-1.5 px-1 text-[10.5px] text-slate-400">
                {pending ? (
                    <>
                        <ClockIcon className="size-3" />
                        Sending
                    </>
                ) : failed ? (
                    <>
                        <AlertCircleIcon className="size-3 text-rose-500" />
                        <span className="text-rose-600">{failure ?? "Not sent"}</span>
                        {local && onRetry && (
                            <button type="button" onClick={onRetry} className="inline-flex items-center gap-0.5 font-medium text-sky-700 hover:underline">
                                <RotateCwIcon className="size-3" />
                                Retry
                            </button>
                        )}
                        {local && (
                            <button
                                type="button"
                                onClick={() => {
                                    onDiscard();
                                    toast.success("Reply discarded");
                                }}
                                className="font-medium text-slate-500 hover:underline"
                            >
                                Discard
                            </button>
                        )}
                    </>
                ) : (
                    stamp(m.sent_at ?? m.created_at)
                )}
            </div>
        </div>
    );
}
