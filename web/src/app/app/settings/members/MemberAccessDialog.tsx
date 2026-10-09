// Which resources a member's roles apply to: the entire workspace, or only
// the campaign folders, campaigns and mailboxes chosen here, read-only.

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import { FolderIcon, GlobeIcon, InboxIcon, Loader2Icon, LockIcon, MegaphoneIcon, PlusIcon, SparklesIcon, XIcon } from "lucide-react";

import { CheckSquare } from "@/components/ui/check-square";
import { Label } from "@/components/ui/field";
import { useConfirm } from "@/hooks/context/confirm";
import { useUserProfile } from "@/hooks/context/user";
import useClickOutside from "@/hooks/useClickOutside";
import useFlipPlacement from "@/hooks/useFlipPlacement";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import useEmails from "@/lib/api/hooks/app/emails/useEmails";
import { useSuggestedSenders } from "@/lib/api/hooks/app/organizations/useMemberAccess";
import type MemberAccess from "@/lib/api/models/app/organizations/MemberAccess";
import { WORKSPACE_ACCESS } from "@/lib/api/models/app/organizations/MemberAccess";
import { cn } from "@/lib/utils";

/** One-line summary of a scope, for the roster and the invite form. */
function accessSummary(access?: MemberAccess | null): string {
    if (!access || access.scope !== "restricted") return "Entire workspace";
    const parts = [
        count(access.campaign_folder_ids.length, "folder"),
        count(access.campaign_ids.length, "campaign"),
        count(access.email_account_ids.length, "mailbox", "mailboxes"),
    ].filter(Boolean);
    return parts.length ? parts.join(", ") : "Nothing granted";
}

function count(n: number, one: string, many = `${one}s`): string {
    return n === 0 ? "" : `${n} ${n === 1 ? one : many}`;
}

/** The roster's access cell: a button for managers, plain text otherwise. */
export function AccessBadge({ access, onClick }: { access?: MemberAccess | null; onClick?: () => void }) {
    const restricted = access?.scope === "restricted";
    const body = (
        <>
            {restricted ? <LockIcon className="w-2.5 h-2.5 shrink-0" /> : <GlobeIcon className="w-2.5 h-2.5 shrink-0" />}
            <span className="truncate">{accessSummary(access)}</span>
        </>
    );
    const cls = cn(
        "inline-flex max-w-full items-center gap-1 h-5 px-1.5 rounded text-[11px] font-medium border",
        restricted ? "bg-amber-50 text-amber-800 border-amber-100" : "bg-slate-50 text-slate-600 border-slate-200",
    );
    if (!onClick) return <span className={cls}>{body}</span>;
    return (
        <button
            type="button"
            onClick={(e) => {
                e.stopPropagation();
                onClick();
            }}
            className={cn(cls, "hover:border-slate-300 transition-colors")}
            title="Change access"
        >
            {body}
        </button>
    );
}

function sameIds(a: string[], b: string[]) {
    if (a.length !== b.length) return false;
    const s = new Set(a);
    return b.every((x) => s.has(x));
}

function sameAccess(a: MemberAccess, b: MemberAccess) {
    if (a.scope !== b.scope) return false;
    if (a.scope !== "restricted") return true;
    return (
        sameIds(a.campaign_folder_ids, b.campaign_folder_ids) &&
        sameIds(a.campaign_ids, b.campaign_ids) &&
        sameIds(a.email_account_ids, b.email_account_ids)
    );
}

export default function MemberAccessDialog({
    title,
    subject,
    initial,
    saveLabel = "Save access",
    onSave,
    onClose,
}: {
    title: string;
    /** Who the scope is for, shown under the title. */
    subject: string;
    initial?: MemberAccess | null;
    saveLabel?: string;
    onSave: (access: MemberAccess) => Promise<void>;
    onClose: () => void;
}) {
    const confirm = useConfirm();
    const start = React.useMemo(() => normalize(initial), [initial]);
    const [access, setAccess] = React.useState<MemberAccess>(start);
    const [saving, setSaving] = React.useState(false);
    const dirty = !sameAccess(access, start);

    const requestClose = React.useCallback(() => {
        if (dirty && !saving) {
            confirm.show("Discard your changes to this member's access?", async () => onClose());
            return;
        }
        onClose();
    }, [dirty, saving, confirm, onClose]);

    React.useEffect(() => {
        function onKey(e: KeyboardEvent) {
            if (e.key !== "Escape") return;
            if (document.querySelector('[data-floating], [role="alertdialog"]')) return;
            requestClose();
        }
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [requestClose]);

    async function save() {
        setSaving(true);
        try {
            await onSave(access.scope === "restricted" ? access : WORKSPACE_ACCESS);
            onClose();
        } catch {
            // The caller surfaced it; the dialog stays open with the draft.
        } finally {
            setSaving(false);
        }
    }

    return createPortal(
        <div className="fixed inset-0 z-[60] flex items-start justify-center p-4 pt-[8vh]">
            <motion.div
                className="absolute inset-0 bg-slate-900/40"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                transition={{ duration: 0.12 }}
                onMouseDown={requestClose}
            />
            <motion.div
                role="dialog"
                aria-modal="true"
                aria-label={title}
                onMouseDown={(e) => e.stopPropagation()}
                initial={{ opacity: 0, y: 8 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: 8 }}
                transition={{ duration: 0.16, ease: "easeOut" }}
                className="relative w-full max-w-[560px] max-h-[84vh] flex flex-col overflow-hidden rounded-lg border border-slate-200 bg-white shadow-[0_24px_60px_-12px_rgba(15,23,42,0.25)]"
            >
                <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-3 shrink-0">
                    <div className="min-w-0">
                        <div className="text-[13px] font-semibold text-slate-900 leading-tight">{title}</div>
                        <div className="text-[11px] text-slate-500 truncate leading-tight">{subject}</div>
                    </div>
                    <button
                        type="button"
                        onClick={requestClose}
                        aria-label="Close"
                        className="ml-auto h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-slate-100"
                    >
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>

                <div className="flex-1 overflow-y-auto p-4">
                    <AccessEditor value={access} onChange={setAccess} />
                </div>

                <div className="h-12 px-4 border-t border-slate-200 flex items-center gap-2 shrink-0">
                    {access.scope === "restricted" && accessSummary(access) === "Nothing granted" && (
                        <span className="text-[11.5px] text-amber-700">Nothing is selected, so this member will see nothing.</span>
                    )}
                    <button
                        type="button"
                        onClick={requestClose}
                        className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:bg-slate-100 transition-colors"
                    >
                        Cancel
                    </button>
                    <button
                        type="button"
                        onClick={save}
                        disabled={saving || !dirty}
                        className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {saving && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        {saveLabel}
                    </button>
                </div>
            </motion.div>
        </div>,
        document.body,
    );
}

function normalize(a?: MemberAccess | null): MemberAccess {
    if (!a || a.scope !== "restricted") return { ...WORKSPACE_ACCESS };
    return {
        scope: "restricted",
        campaign_folder_ids: a.campaign_folder_ids ?? [],
        campaign_ids: a.campaign_ids ?? [],
        email_account_ids: a.email_account_ids ?? [],
    };
}

/** The scope choice and, for a restricted scope, the three resource pickers. */
export function AccessEditor({ value, onChange }: { value: MemberAccess; onChange: (next: MemberAccess) => void }) {
    const restricted = value.scope === "restricted";
    const { user } = useUserProfile();
    const campaigns = useCampaigns({ query: "", folder: "", enabled: restricted });
    const emails = useEmails({ query: "", tag: "", enabled: restricted });

    const folderItems = React.useMemo(
        () =>
            [...(user.folders ?? [])]
                .sort((a, b) => a.position - b.position)
                .map((f) => ({ id: f.id, label: f.title, color: f.color })),
        [user.folders],
    );
    const campaignItems = React.useMemo(
        () => campaigns.campaigns.map((c) => ({ id: c.id, label: c.name || "Untitled campaign" })),
        [campaigns.campaigns],
    );
    const mailboxItems = React.useMemo(
        () => emails.emails.map((e) => ({ id: e.id, label: e.email, sub: e.name })),
        [emails.emails],
    );

    const set = (patch: Partial<MemberAccess>) => onChange({ ...value, ...patch });

    return (
        <div className="flex flex-col gap-4">
            <div>
                <Label>Access</Label>
                <div className="grid grid-cols-2 gap-1.5">
                    {(
                        [
                            ["workspace", "Entire workspace", "Everything the roles allow", GlobeIcon],
                            ["restricted", "Selected resources", "Only what you choose, read-only", LockIcon],
                        ] as const
                    ).map(([scope, label, sub, Icon]) => (
                        <button
                            key={scope}
                            type="button"
                            onClick={() => set({ scope })}
                            className={cn(
                                "rounded-md border px-2.5 py-2 text-left transition-colors",
                                value.scope === scope ? "border-sky-400 bg-sky-50" : "border-slate-200 hover:border-slate-300",
                            )}
                        >
                            <span className="flex items-center gap-1.5 text-[12.5px] font-medium text-slate-900">
                                <Icon className="w-3 h-3 text-slate-500" />
                                {label}
                            </span>
                            <span className="block text-[11px] text-slate-500">{sub}</span>
                        </button>
                    ))}
                </div>
            </div>

            <AnimatePresence initial={false}>
                {restricted && (
                    <motion.div
                        initial={{ opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={{ opacity: 0, height: 0 }}
                        transition={{ duration: 0.16, ease: "easeOut" }}
                        className="flex flex-col gap-4"
                    >
                        <p className="text-[11.5px] leading-relaxed text-slate-500">
                            The member can view the selected campaigns and their analytics, and read the conversations
                            in the selected mailboxes. They cannot change, send or reply to anything, and contacts,
                            settings and the rest of the workspace are hidden from them. Their roles still apply on
                            top: a role without inbox access shows no mailbox.
                        </p>

                        <div>
                            <Label>Campaign folders</Label>
                            <ResourcePicker
                                icon={FolderIcon}
                                items={folderItems}
                                value={value.campaign_folder_ids}
                                onChange={(ids) => set({ campaign_folder_ids: ids })}
                                placeholder="Choose folders…"
                                empty="This workspace has no campaign folders."
                            />
                            <p className="mt-1 text-[11px] text-slate-400">
                                A folder grants whatever campaigns it holds at the time, including ones added later.
                            </p>
                        </div>

                        <div>
                            <Label>Campaigns</Label>
                            <ResourcePicker
                                icon={MegaphoneIcon}
                                items={campaignItems}
                                loading={campaigns.isPending}
                                value={value.campaign_ids}
                                onChange={(ids) => set({ campaign_ids: ids })}
                                placeholder="Choose individual campaigns…"
                                empty="This workspace has no campaigns."
                            />
                        </div>

                        <div>
                            <Label>Mailboxes</Label>
                            <ResourcePicker
                                icon={InboxIcon}
                                items={mailboxItems}
                                loading={emails.isPending}
                                value={value.email_account_ids}
                                onChange={(ids) => set({ email_account_ids: ids })}
                                placeholder="Choose mailboxes…"
                                empty="This workspace has no mailboxes."
                            />
                            <p className="mt-1 text-[11px] text-slate-400">
                                Campaign access never includes a mailbox. Grant the ones whose conversations they should read.
                            </p>
                            <SuggestedSenders
                                campaignIds={value.campaign_ids}
                                folderIds={value.campaign_folder_ids}
                                granted={value.email_account_ids}
                                onAdd={(ids) => set({ email_account_ids: Array.from(new Set([...value.email_account_ids, ...ids])) })}
                            />
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

/** Mailboxes the chosen campaigns send from, offered for review, never granted on their own. */
function SuggestedSenders({
    campaignIds,
    folderIds,
    granted,
    onAdd,
}: {
    campaignIds: string[];
    folderIds: string[];
    granted: string[];
    onAdd: (ids: string[]) => void;
}) {
    const suggested = useSuggestedSenders(campaignIds, folderIds, true);
    const missing = (suggested.data ?? []).filter((s) => !granted.includes(s.id));
    if (campaignIds.length + folderIds.length === 0 || missing.length === 0) return null;
    return (
        <div className="mt-2 rounded-md border border-slate-200 bg-slate-50/60 p-2.5">
            <div className="flex items-center gap-1.5 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                <SparklesIcon className="w-3 h-3" />
                Senders of the selected campaigns
                <button
                    type="button"
                    onClick={() => onAdd(missing.map((s) => s.id))}
                    className="ml-auto normal-case tracking-normal text-[11px] text-sky-700 hover:text-sky-800 font-medium"
                >
                    Add all
                </button>
            </div>
            <ul className="mt-1.5 flex flex-col gap-0.5">
                {missing.map((s) => (
                    <li key={s.id} className="flex items-center gap-2 h-6 text-[12px] text-slate-700">
                        <span className="truncate">{s.email}</span>
                        <button
                            type="button"
                            onClick={() => onAdd([s.id])}
                            className="ml-auto shrink-0 inline-flex items-center gap-1 h-5 px-1.5 rounded text-[11px] text-slate-600 border border-slate-200 bg-white hover:border-slate-300"
                        >
                            <PlusIcon className="w-2.5 h-2.5" />
                            Add
                        </button>
                    </li>
                ))}
            </ul>
        </div>
    );
}

interface PickerItem {
    id: string;
    label: string;
    sub?: string;
    color?: string;
}

/** Chip box plus a searchable dropdown of checkbox rows. */
function ResourcePicker({
    icon: Icon,
    items,
    value,
    onChange,
    placeholder,
    empty,
    loading = false,
}: {
    icon: React.ComponentType<{ className?: string }>;
    items: PickerItem[];
    value: string[];
    onChange: (ids: string[]) => void;
    placeholder: string;
    empty: string;
    loading?: boolean;
}) {
    const [open, setOpen] = React.useState(false);
    const [query, setQuery] = React.useState("");
    const ref = React.useRef<HTMLDivElement>(null);
    const triggerRef = React.useRef<HTMLDivElement>(null);
    useClickOutside(open, () => setOpen(false), ref);
    const placement = useFlipPlacement(triggerRef, open, 270);

    const byId = React.useMemo(() => new Map(items.map((i) => [i.id, i])), [items]);
    const filtered = React.useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q) return items;
        return items.filter((i) => i.label.toLowerCase().includes(q) || i.sub?.toLowerCase().includes(q));
    }, [items, query]);

    const toggle = (id: string) => onChange(value.includes(id) ? value.filter((x) => x !== id) : [...value, id]);

    return (
        <div ref={ref} className="relative">
            <div
                ref={triggerRef}
                onClick={() => setOpen((o) => !o)}
                className={cn(
                    "rounded-md border bg-white min-h-[34px] px-2 py-1.5 flex flex-wrap items-center gap-1 cursor-pointer transition-colors",
                    open ? "border-sky-400 ring-2 ring-sky-100" : "border-slate-200 hover:border-slate-300",
                )}
            >
                {value.length === 0 && <span className="px-1 text-[11.5px] text-slate-400">{placeholder}</span>}
                {value.map((id) => {
                    const item = byId.get(id);
                    return (
                        <span
                            key={id}
                            className="inline-flex items-center gap-1 h-5 max-w-full min-w-0 px-1.5 rounded bg-slate-100 text-[11px] font-medium text-slate-700"
                        >
                            {item?.color ? (
                                <span className="size-2 rounded-full shrink-0" style={{ backgroundColor: item.color }} />
                            ) : (
                                <Icon className="w-2.5 h-2.5 shrink-0 text-slate-400" />
                            )}
                            <span className="truncate">{item?.label ?? (loading ? "Loading…" : "Removed")}</span>
                            <button
                                type="button"
                                onClick={(e) => {
                                    e.stopPropagation();
                                    toggle(id);
                                }}
                                aria-label={`Remove ${item?.label ?? "item"}`}
                                className="opacity-60 hover:opacity-100 shrink-0"
                            >
                                <XIcon className="w-2.5 h-2.5" />
                            </button>
                        </span>
                    );
                })}
            </div>

            <AnimatePresence>
                {open && (
                    <motion.div
                        data-floating
                        initial={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        transition={{ duration: 0.12 }}
                        className={cn(
                            "absolute left-0 right-0 z-30 rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] overflow-hidden",
                            placement === "top" ? "bottom-full mb-1" : "top-full mt-1",
                        )}
                    >
                        <div className="px-2 py-1.5 border-b border-slate-200">
                            <input
                                value={query}
                                onChange={(e) => setQuery(e.target.value)}
                                placeholder="Search…"
                                autoFocus
                                className="w-full h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                            />
                        </div>
                        <div className="max-h-56 overflow-y-auto py-1">
                            {loading && items.length === 0 ? (
                                <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">Loading…</div>
                            ) : filtered.length === 0 ? (
                                <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">
                                    {items.length === 0 ? empty : "No matches."}
                                </div>
                            ) : (
                                filtered.map((item) => (
                                    <button
                                        key={item.id}
                                        type="button"
                                        onClick={() => toggle(item.id)}
                                        className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-100 transition-colors"
                                    >
                                        <CheckSquare checked={value.includes(item.id)} />
                                        {item.color && <span className="size-2.5 rounded-full shrink-0" style={{ backgroundColor: item.color }} />}
                                        <span className="truncate">{item.label}</span>
                                        {item.sub && <span className="truncate text-[11px] text-slate-400">{item.sub}</span>}
                                    </button>
                                ))
                            )}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
