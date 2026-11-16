// ConnectLinksDialog: "Invite client to connect". A member makes a link a
// client opens to connect their own mailboxes into this workspace, sees it once
// to copy, and manages the links already sent (status, mailboxes connected,
// revoke). Lives on the mailboxes page.
import React from "react";
import { createPortal } from "react-dom";
import toast from "react-hot-toast/headless";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, CopyIcon, Link2Icon, ListIcon, PlusIcon, XIcon } from "lucide-react";
import { Label, NumberInput, TextInput } from "@/components/ui/field";
import { Checkbox } from "@/components/ui/checkbox";
import ScrollStrip from "@/components/ui/scroll-strip";
import { Segmented, Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import TagSelector from "@/components/app/popup/select/TagSelector";
import { useConfirm } from "@/hooks/context/confirm";
import useConnectLinks, { useConnectLinkUses, useCreateConnectLink, useRevokeConnectLink } from "@/lib/api/hooks/app/emails/useConnectLinks";
import {
    CONNECT_LINK_PROVIDERS,
    type ConnectLink,
    type ConnectLinkProvider,
    type ConnectLinkStatus,
    type CreatedConnectLink,
} from "@/lib/api/models/app/emails/ConnectLink";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import timeAgo from "@/lib/helper/timeAgo";
import { cn } from "@/lib/utils";

type Tab = "new" | "links";
type Expiry = "1" | "7" | "30" | "90";

const EXPIRY_OPTIONS: { value: Expiry; label: string }[] = [
    { value: "1", label: "1 day" },
    { value: "7", label: "7 days" },
    { value: "30", label: "30 days" },
    { value: "90", label: "90 days" },
];

const STATUS: Record<ConnectLinkStatus, { label: string; tone: string }> = {
    active: { label: "Active", tone: "bg-emerald-50 text-emerald-700" },
    exhausted: { label: "Complete", tone: "bg-sky-50 text-sky-700" },
    expired: { label: "Expired", tone: "bg-slate-100 text-slate-500" },
    revoked: { label: "Revoked", tone: "bg-slate-100 text-slate-500" },
};

interface Draft {
    label: string;
    providers: ConnectLinkProvider[];
    max: number;
    expiry: Expiry;
    tags: string[];
    warmup: boolean;
}

const emptyDraft = (): Draft => ({
    label: "",
    providers: ["gmail", "outlook", "smtp_imap", "vendor"],
    max: 5,
    expiry: "7",
    tags: [],
    warmup: true,
});

const isDirty = (d: Draft) => JSON.stringify(d) !== JSON.stringify(emptyDraft());

export default function ConnectLinksDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
    const confirm = useConfirm();
    const [tab, setTab] = React.useState<Tab>("new");
    const [draft, setDraft] = React.useState<Draft>(emptyDraft);
    const [created, setCreated] = React.useState<CreatedConnectLink | null>(null);
    const idempotencyKey = React.useRef(crypto.randomUUID());
    const links = useConnectLinks(open);
    const create = useCreateConnectLink();

    React.useEffect(() => {
        if (!open) return;
        setTab("new");
        setDraft(emptyDraft());
        setCreated(null);
        idempotencyKey.current = crypto.randomUUID();
    }, [open]);

    const requestClose = React.useCallback(() => {
        if (create.isPending) return;
        if (tab === "new" && !created && isDirty(draft)) {
            confirm.show("Discard this link? Nothing has been created yet.", async () => onClose());
            return;
        }
        onClose();
    }, [create.isPending, tab, created, draft, confirm, onClose]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            e.preventDefault();
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, requestClose]);

    const submit = async () => {
        try {
            const out = await create.mutateAsync({
                body: {
                    label: draft.label.trim(),
                    providers: draft.providers,
                    max_mailboxes: draft.max,
                    expires_in_days: Number(draft.expiry),
                    tag_ids: draft.tags,
                    warmup: draft.warmup,
                },
                idempotencyKey: idempotencyKey.current,
            });
            setCreated(out);
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    };

    const activeCount = (links.data?.data ?? []).filter((l) => l.status === "active").length;
    const blocker = draft.providers.length === 0 ? "Choose at least one way for the client to connect." : null;

    return createPortal(
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onMouseDown={requestClose}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        role="dialog"
                        aria-modal="true"
                        aria-label="Invite client to connect"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.16 }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[560px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[88dvh]"
                    >
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <Link2Icon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Mailbox</span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12px] text-slate-600 truncate">Invite client to connect</span>
                            <button
                                type="button"
                                onClick={requestClose}
                                disabled={create.isPending}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-40"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>
                        <ScrollStrip activeKey={tab} className="shrink-0 border-b border-slate-200" innerClassName="px-3 gap-1">
                            <TabButton active={tab === "new"} onClick={() => setTab("new")} icon={<PlusIcon className="w-3.5 h-3.5" />}>
                                New link
                            </TabButton>
                            <TabButton active={tab === "links"} onClick={() => setTab("links")} icon={<ListIcon className="w-3.5 h-3.5" />}>
                                Sent links{activeCount > 0 && <span className="text-slate-400 tabular-nums">{activeCount}</span>}
                            </TabButton>
                        </ScrollStrip>
                        <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden">
                            {tab === "links" ? (
                                <LinkList links={links.data?.data} loading={links.isLoading} error={links.isError} onNew={() => setTab("new")} />
                            ) : created ? (
                                <CreatedView
                                    created={created}
                                    onAnother={() => {
                                        setCreated(null);
                                        setDraft(emptyDraft());
                                        idempotencyKey.current = crypto.randomUUID();
                                    }}
                                    onDone={onClose}
                                />
                            ) : (
                                <NewLinkForm draft={draft} setDraft={setDraft} />
                            )}
                        </div>
                        {tab === "new" && !created && (
                            <div className="shrink-0 px-4 py-3 border-t border-slate-200 flex items-center gap-3">
                                {blocker && <span className="text-[11.5px] text-amber-700">{blocker}</span>}
                                <button
                                    type="button"
                                    onClick={() => void submit()}
                                    disabled={!!blocker || create.isPending}
                                    title={blocker ?? undefined}
                                    className="ml-auto h-7 px-3 rounded-md bg-slate-900 text-white text-[12px] font-medium hover:bg-slate-800 disabled:opacity-50 transition-colors"
                                >
                                    {create.isPending ? "Creating…" : "Create link"}
                                </button>
                            </div>
                        )}
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>,
        document.body,
    );
}

function TabButton({ active, onClick, icon, children }: { active: boolean; onClick: () => void; icon: React.ReactNode; children: React.ReactNode }) {
    return (
        <button
            type="button"
            onClick={onClick}
            data-active={active ? "true" : undefined}
            className={cn(
                "relative h-10 px-2.5 inline-flex items-center gap-1.5 text-[12.5px] transition-colors",
                active ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-800",
            )}
        >
            {icon}
            {children}
            {active && <span className="absolute left-2 right-2 -bottom-px h-0.5 bg-sky-600 rounded-full" />}
        </button>
    );
}

function NewLinkForm({ draft, setDraft }: { draft: Draft; setDraft: React.Dispatch<React.SetStateAction<Draft>> }) {
    const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setDraft((d) => ({ ...d, [k]: v }));
    const toggleProvider = (p: ConnectLinkProvider) =>
        set("providers", draft.providers.includes(p) ? draft.providers.filter((x) => x !== p) : [...draft.providers, p]);
    return (
        <div className="px-4 py-4 flex flex-col gap-4">
            <p className="text-[12.5px] text-slate-600 leading-relaxed">
                Send this link to a client. They connect their own mailboxes by signing in with Google or Microsoft, or with their server
                details, and each mailbox lands in this workspace. Nobody shares a password with you.
            </p>
            <div className="flex flex-col gap-1.5">
                <Label>Label</Label>
                <TextInput value={draft.label} onChange={(v) => set("label", v)} placeholder="Acme Corp onboarding" maxLength={64} />
                <span className="text-[11px] text-slate-400">Only your team sees this.</span>
            </div>
            <div className="flex flex-col gap-1.5">
                <Label>Ways to connect</Label>
                <div className="flex flex-wrap gap-x-4 gap-y-2">
                    {CONNECT_LINK_PROVIDERS.map((p) => (
                        <label key={p.id} className="inline-flex items-center gap-2 text-[12.5px] text-slate-700 cursor-pointer">
                            <Checkbox tone="slate" checked={draft.providers.includes(p.id)} onChange={() => toggleProvider(p.id)} />
                            {p.label}
                        </label>
                    ))}
                </div>
                {draft.providers.includes("vendor") && (
                    <span className="text-[11px] leading-snug text-slate-400">
                        With a vendor account, the client pastes the API key of Zapmail, InboxKit or another vendor and picks its mailboxes, many at
                        once. The key is kept here as one of this workspace's vendor accounts.
                    </span>
                )}
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="flex flex-col gap-1.5">
                    <Label>Mailboxes allowed</Label>
                    <NumberInput value={draft.max} onChange={(v) => set("max", v)} min={1} max={500} className="w-32" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <Label>Expires after</Label>
                    <Segmented value={draft.expiry} onChange={(v) => set("expiry", v)} options={EXPIRY_OPTIONS} />
                </div>
            </div>
            <div className="flex flex-col gap-1.5">
                <Label>Tags for connected mailboxes</Label>
                <TagSelector
                    selected={draft.tags}
                    onAdd={(id) => set("tags", draft.tags.length >= 20 ? draft.tags : [...draft.tags, id])}
                    onRemove={(id) => set("tags", draft.tags.filter((t) => t !== id))}
                />
            </div>
            <div className="flex items-center justify-between gap-4 rounded-md border border-slate-200 px-3 py-2.5">
                <div className="min-w-0">
                    <div className="text-[12.5px] text-slate-800">Start warmup</div>
                    <div className="text-[11.5px] text-slate-500">Each mailbox starts warming up as soon as it connects.</div>
                </div>
                <Toggle value={draft.warmup} onChange={(v) => set("warmup", v)} ariaLabel="Start warmup on connected mailboxes" />
            </div>
        </div>
    );
}

function CreatedView({ created, onAnother, onDone }: { created: CreatedConnectLink; onAnother: () => void; onDone: () => void }) {
    const [copied, setCopied] = React.useState(false);
    const copy = async () => {
        try {
            await navigator.clipboard.writeText(created.url);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1600);
        } catch {
            toast.error("Copy failed. Select the link and copy it yourself.");
        }
    };
    const days = Math.max(1, Math.round((new Date(created.link.expires_at).getTime() - Date.now()) / 86_400_000));
    return (
        <div className="px-4 py-5 flex flex-col gap-4">
            <div>
                <div className="text-[13px] font-medium text-slate-900">Your link is ready</div>
                <p className="mt-1 text-[12.5px] text-slate-600">
                    Copy it now. For security it is shown only once; if it is lost, revoke it and make a new one.
                </p>
            </div>
            <div className="flex items-center gap-2">
                <input
                    readOnly
                    value={created.url}
                    onFocus={(e) => e.currentTarget.select()}
                    aria-label="Connect link"
                    className="h-7 flex-1 min-w-0 px-2.5 rounded-md border border-slate-200 bg-slate-50 text-[12.5px] text-slate-800 font-mono outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                />
                <button
                    type="button"
                    onClick={() => void copy()}
                    className="h-7 px-2.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:border-slate-300 inline-flex items-center gap-1.5"
                >
                    {copied ? <CheckIcon className="w-3 h-3 text-emerald-600" /> : <CopyIcon className="w-3 h-3" />}
                    {copied ? "Copied" : "Copy"}
                </button>
            </div>
            <p className="text-[11.5px] text-slate-500">
                Connects up to {created.link.max_mailboxes} mailbox{created.link.max_mailboxes === 1 ? "" : "es"} and expires in {days} day
                {days === 1 ? "" : "s"}. You get a notification each time a client connects one.
            </p>
            <div className="flex items-center gap-2 justify-end">
                <button type="button" onClick={onAnother} className="h-7 px-3 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100">
                    Make another
                </button>
                <button type="button" onClick={onDone} className="h-7 px-3 rounded-md bg-slate-900 text-white text-[12px] font-medium hover:bg-slate-800">
                    Done
                </button>
            </div>
        </div>
    );
}

function LinkList({ links, loading, error, onNew }: { links?: ConnectLink[]; loading: boolean; error: boolean; onNew: () => void }) {
    const [openId, setOpenId] = React.useState<string | null>(null);
    if (loading) {
        return (
            <div className="divide-y divide-slate-100">
                {Array.from({ length: 3 }).map((_, i) => (
                    <div key={i} className="h-14 px-4 flex items-center gap-3">
                        <div className="h-3 w-40 bg-slate-100 rounded animate-pulse" />
                        <div className="ml-auto h-3 w-12 bg-slate-100 rounded animate-pulse" />
                    </div>
                ))}
            </div>
        );
    }
    if (error) return <p className="px-4 py-6 text-[12.5px] text-slate-500">The links could not be loaded.</p>;
    if (!links || links.length === 0) {
        return (
            <div className="px-4 py-8 text-center">
                <p className="text-[12.5px] text-slate-500">No links sent yet.</p>
                <button type="button" onClick={onNew} className="mt-2 text-[12px] text-sky-700 hover:underline">
                    Make the first one
                </button>
            </div>
        );
    }
    return (
        <ul className="divide-y divide-slate-100">
            {links.map((l) => (
                <LinkRow key={l.id} link={l} open={openId === l.id} onToggle={() => setOpenId(openId === l.id ? null : l.id)} />
            ))}
        </ul>
    );
}

function LinkRow({ link, open, onToggle }: { link: ConnectLink; open: boolean; onToggle: () => void }) {
    const confirm = useConfirm();
    const revoke = useRevokeConnectLink();
    const st = STATUS[link.status] ?? STATUS.expired;
    const live = link.status === "active";
    const ask = (e: React.MouseEvent) => {
        e.stopPropagation();
        confirm.show(
            `Revoke ${link.label ? `"${link.label}"` : "this link"}? The client can no longer connect mailboxes with it. Mailboxes already connected stay connected.`,
            async () => {
                try {
                    await revoke.mutateAsync(link.id);
                    toast.success("Link revoked");
                } catch (err) {
                    toast.error(buildError(err as AppError));
                }
            },
        );
    };
    return (
        <li>
            <div
                role="button"
                tabIndex={0}
                onClick={onToggle}
                onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        onToggle();
                    }
                }}
                aria-expanded={open}
                className="group px-4 py-2.5 flex items-center gap-3 cursor-pointer hover:bg-slate-50 transition-colors"
            >
                <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 min-w-0">
                        <span className="text-[12.5px] text-slate-800 truncate">{link.label || "Untitled link"}</span>
                        <span className={cn("shrink-0 px-1.5 h-[18px] rounded text-[10.5px] font-medium inline-flex items-center", st.tone)}>{st.label}</span>
                    </div>
                    <div className="mt-0.5 text-[11.5px] text-slate-500 tabular-nums">
                        {link.mailboxes_connected} of {link.max_mailboxes} connected
                        {" · "}
                        {live ? `expires ${new Date(link.expires_at).toLocaleDateString()}` : `made ${timeAgo(link.created_at)}`}
                        {link.last_used_at && ` · last used ${timeAgo(link.last_used_at)}`}
                    </div>
                </div>
                {live && (
                    <button
                        type="button"
                        onClick={ask}
                        disabled={revoke.isPending}
                        className="shrink-0 h-6 px-2 rounded text-[11.5px] text-red-600 hover:bg-red-50 opacity-100 md:opacity-0 md:group-hover:opacity-100 focus-visible:opacity-100 transition-opacity disabled:opacity-50"
                    >
                        Revoke
                    </button>
                )}
            </div>
            {open && <LinkUses id={link.id} />}
        </li>
    );
}

function LinkUses({ id }: { id: string }) {
    const uses = useConnectLinkUses(id);
    if (uses.isLoading) return <p className="px-4 pb-3 text-[11.5px] text-slate-400">Loading…</p>;
    const rows = uses.data?.data ?? [];
    if (rows.length === 0) return <p className="px-4 pb-3 text-[11.5px] text-slate-400">No mailboxes connected through this link yet.</p>;
    return (
        <ul className="px-4 pb-3 flex flex-col gap-1">
            {rows.map((u) => (
                <li key={u.id} className="flex items-center gap-2 text-[11.5px] text-slate-600">
                    <CheckIcon className="w-3 h-3 text-emerald-600 shrink-0" />
                    <span className="truncate">{u.email}</span>
                    <span className="ml-auto text-slate-400 shrink-0">{timeAgo(u.connected_at)}</span>
                </li>
            ))}
        </ul>
    );
}
