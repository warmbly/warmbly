// ContactContextPanel : the CRM rail of the unibox thread reader.
//
// Given the sender address of the open thread, it resolves the contact and
// surfaces the customer-relationship context inline: who they are, which
// campaign(s) they came from, their engagement, and their deals / tasks /
// notes with one-click add. New deals stamp the originating campaign + the
// mailbox the thread is on, so a reply turns into attributed pipeline without
// leaving the inbox.

import React from "react";
import toast from "react-hot-toast";
import {
    BanIcon,
    CheckIcon,
    CircleDollarSignIcon,
    ExternalLinkIcon,
    Loader2Icon,
    MailWarningIcon,
    MegaphoneIcon,
    PlusIcon,
    StickyNoteIcon,
    UserIcon,
    UserXIcon,
    CheckSquareIcon,
    XIcon,
} from "lucide-react";
import { Link } from "react-router-dom";
import { TextInput } from "@/components/ui/field";
import useContactByEmail from "@/lib/api/hooks/app/contacts/useContactByEmail";
import useContact from "@/lib/api/hooks/app/contacts/useContact";
import useContactDeals from "@/lib/api/hooks/app/contacts/useContactDeals";
import useContactNotes from "@/lib/api/hooks/app/contacts/useContactNotes";
import useCreateContactNote from "@/lib/api/hooks/app/contacts/useCreateContactNote";
import useCRMTasks from "@/lib/api/hooks/app/crm/tasks/useCRMTasks";
import useCreateCRMTask from "@/lib/api/hooks/app/crm/tasks/useCreateCRMTask";
import useCreateDeal from "@/lib/api/hooks/app/crm/deals/useCreateDeal";
import usePipelines from "@/lib/api/hooks/app/crm/pipelines/usePipelines";
import type Deal from "@/lib/api/models/app/crm/Deal";
import type CRMTask from "@/lib/api/models/app/crm/CRMTask";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

const DEAL_STATUS: Record<Deal["status"], { label: string; cls: string; dot: string }> = {
    open: { label: "Open", cls: "text-slate-600", dot: "bg-slate-400" },
    won: { label: "Won", cls: "text-emerald-700", dot: "bg-emerald-500" },
    lost: { label: "Lost", cls: "text-red-700", dot: "bg-red-500" },
};

const PRIORITY_DOT: Record<CRMTask["priority"], string> = {
    urgent: "bg-red-500",
    high: "bg-amber-500",
    medium: "bg-sky-500",
    low: "bg-slate-400",
};

export default function ContactContextPanel({
    email,
    mailboxId,
    onClose,
}: {
    email?: string;
    mailboxId?: string;
    onClose?: () => void;
}) {
    const lookup = useContactByEmail(email);
    const contact = lookup.data ?? null;
    const contactId = contact?.id;

    const detailQ = useContact(contactId ?? "", !!contactId);
    const detail = detailQ.data;
    const dealsQ = useContactDeals(contactId ?? "");
    const tasksQ = useCRMTasks({ contact_id: contactId, limit: 50 }, !!contactId);
    const notesQ = useContactNotes(contactId ?? "");
    const dealDefault = usePipelinesDefault();

    const campaigns = detail?.campaigns ?? contact?.campaigns ?? [];
    const eng = detail?.engagement;
    const supp = detail?.suppression;

    const name =
        [contact?.first_name, contact?.last_name].filter(Boolean).join(" ").trim() ||
        contact?.email ||
        email ||
        "";

    return (
        <aside className="hidden lg:flex w-80 shrink-0 flex-col border-l border-slate-200 bg-slate-50/40 min-h-0">
            <div className="h-12 px-3 border-b border-slate-200 flex items-center gap-2 shrink-0 bg-white">
                <UserIcon className="w-3.5 h-3.5 text-slate-400" />
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                    Contact
                </span>
                {onClose && (
                    <button
                        type="button"
                        onClick={onClose}
                        aria-label="Hide contact panel"
                        className="ml-auto size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                )}
            </div>

            <div className="flex-1 overflow-y-auto">
                {lookup.isPending ? (
                    <div className="flex items-center justify-center gap-2 py-10 text-[12px] text-slate-400">
                        <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                        Resolving contact…
                    </div>
                ) : !contact ? (
                    <NotAContact email={email} />
                ) : (
                    <div className="divide-y divide-slate-200/70">
                        {/* Identity */}
                        <div className="px-3 py-3">
                            <div className="flex items-start gap-2.5">
                                <div className="size-8 rounded-full bg-sky-100 text-sky-700 flex items-center justify-center text-[12px] font-semibold shrink-0">
                                    {initials(name)}
                                </div>
                                <div className="min-w-0 flex-1">
                                    <div className="text-[13px] font-semibold text-slate-900 truncate">{name}</div>
                                    <div className="text-[11.5px] text-slate-500 truncate">{contact.email}</div>
                                    {contact.company && (
                                        <div className="text-[11px] text-slate-400 truncate mt-0.5">{contact.company}</div>
                                    )}
                                </div>
                            </div>
                            <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
                                {contact.subscribed ? (
                                    <Badge tone="emerald" icon={<CheckIcon className="w-2.5 h-2.5" />}>Subscribed</Badge>
                                ) : (
                                    <Badge tone="slate" icon={<UserXIcon className="w-2.5 h-2.5" />}>Unsubscribed</Badge>
                                )}
                                {supp && (
                                    <Badge tone="red" icon={<BanIcon className="w-2.5 h-2.5" />}>
                                        Suppressed
                                    </Badge>
                                )}
                                <Link
                                    to="/app/contacts"
                                    className="ml-auto inline-flex items-center gap-1 text-[10.5px] text-slate-400 hover:text-sky-700 transition-colors"
                                >
                                    Contacts
                                    <ExternalLinkIcon className="w-2.5 h-2.5" />
                                </Link>
                            </div>
                            {supp && (
                                <div className="mt-2 rounded-md border border-red-200 bg-red-50/60 px-2 py-1.5 flex items-start gap-1.5">
                                    <MailWarningIcon className="w-3 h-3 text-red-600 mt-px shrink-0" />
                                    <span className="text-[10.5px] text-red-700/90 leading-snug">
                                        {supp.reason || "Suppressed"} ({supp.source})
                                    </span>
                                </div>
                            )}
                        </div>

                        {/* Lead source : campaigns */}
                        <Section label="Campaigns" hint={campaigns.length ? undefined : "Not in any campaign"}>
                            {campaigns.length > 0 && (
                                <div className="flex flex-wrap gap-1.5">
                                    {campaigns.map((c) => (
                                        <Link
                                            key={c.id}
                                            to="/app/campaigns"
                                            className="inline-flex items-center gap-1 h-5 px-1.5 rounded bg-white border border-slate-200 hover:border-sky-300 text-[10.5px] text-slate-600 hover:text-sky-700 transition-colors"
                                        >
                                            <MegaphoneIcon className="w-2.5 h-2.5 text-slate-400" />
                                            <span className="truncate max-w-[140px]">{c.name}</span>
                                        </Link>
                                    ))}
                                </div>
                            )}
                        </Section>

                        {/* Engagement */}
                        {eng && (
                            <Section label="Engagement">
                                <div className="grid grid-cols-4 gap-1.5">
                                    <Metric label="Sent" value={eng.total_sent} />
                                    <Metric label="Open" value={eng.total_opened} />
                                    <Metric label="Click" value={eng.total_clicked} />
                                    <Metric label="Reply" value={eng.total_replied} accent />
                                </div>
                            </Section>
                        )}

                        {/* Deals */}
                        <DealsSection
                            contactId={contact.id}
                            deals={dealsQ.data ?? []}
                            loading={dealsQ.isPending}
                            defaultName={contact.company || name}
                            campaignId={campaigns[0]?.id}
                            mailboxId={mailboxId}
                            pipelineId={dealDefault.pipelineId}
                            stageId={dealDefault.stageId}
                        />

                        {/* Tasks */}
                        <TasksSection
                            contactId={contact.id}
                            tasks={(tasksQ.data?.data ?? []).filter((t) => t.status !== "completed" && t.status !== "cancelled")}
                            loading={tasksQ.isPending}
                            defaultTitle={`Follow up with ${name}`}
                        />

                        {/* Notes */}
                        <NotesSection contactId={contact.id} notes={notesQ.data ?? []} loading={notesQ.isPending} />
                    </div>
                )}
            </div>
        </aside>
    );
}

// Resolve the org's first pipeline + its first stage, used as the default
// target when creating a deal from the panel. Kept as a tiny hook so the deal
// section stays declarative.
function usePipelinesDefault() {
    const pipelines = usePipelines();
    const first = pipelines.data?.[0];
    const stage = first ? [...(first.stages ?? [])].sort((a, b) => a.position - b.position)[0] : undefined;
    return { pipelineId: first?.id, stageId: stage?.id };
}

function DealsSection({
    contactId,
    deals,
    loading,
    defaultName,
    campaignId,
    mailboxId,
    pipelineId,
    stageId,
}: {
    contactId: string;
    deals: Deal[];
    loading: boolean;
    defaultName: string;
    campaignId?: string;
    mailboxId?: string;
    pipelineId?: string;
    stageId?: string;
}) {
    const create = useCreateDeal();
    const [open, setOpen] = React.useState(false);
    const [name, setName] = React.useState(defaultName);
    const [value, setValue] = React.useState("");

    React.useEffect(() => setName(defaultName), [defaultName]);

    const canAdd = !!pipelineId && !!stageId;

    async function submit() {
        if (!name.trim()) {
            toast.error("Deal name required");
            return;
        }
        const data: Partial<Deal> = {
            pipeline_id: pipelineId,
            stage_id: stageId,
            name: name.trim(),
            contact_id: contactId,
            currency: "USD",
        };
        if (value.trim()) {
            const n = Number(value);
            if (Number.isFinite(n)) data.value = n;
        }
        if (campaignId) data.campaign_id = campaignId;
        if (mailboxId) data.source_mailbox_id = mailboxId;
        try {
            await toast.promise(create.mutateAsync(data), {
                loading: "Creating deal…",
                success: "Deal created",
                error: (e: AppError) => buildError(e),
            });
            setOpen(false);
            setValue("");
        } catch {
            /* surfaced */
        }
    }

    return (
        <Section
            label="Deals"
            action={
                canAdd ? (
                    <AddButton open={open} onClick={() => setOpen((o) => !o)} />
                ) : (
                    <Link to="/app/crm/pipelines" className="text-[10.5px] text-slate-400 hover:text-sky-700">
                        Add a pipeline
                    </Link>
                )
            }
        >
            {open && canAdd && (
                <div className="mb-2 rounded-md border border-slate-200 bg-white p-2 space-y-1.5">
                    <TextInput value={name} onChange={setName} placeholder="Deal name" className="w-full" autoFocus />
                    <div className="flex items-center gap-1.5">
                        <TextInput value={value} onChange={setValue} placeholder="Value (optional)" className="w-full" />
                        <button
                            type="button"
                            onClick={submit}
                            disabled={create.isPending}
                            className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[11.5px] font-medium inline-flex items-center gap-1 transition-colors disabled:opacity-60 shrink-0"
                        >
                            {create.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CheckIcon className="w-3 h-3" />}
                            Add
                        </button>
                    </div>
                    {(campaignId || mailboxId) && (
                        <p className="text-[10px] text-slate-400 leading-snug">
                            Attributed to this {campaignId ? "campaign" : ""}
                            {campaignId && mailboxId ? " + " : ""}
                            {mailboxId ? "mailbox" : ""}.
                        </p>
                    )}
                </div>
            )}
            {loading ? (
                <RowSkeleton />
            ) : deals.length === 0 ? (
                <Empty icon={<CircleDollarSignIcon className="w-3.5 h-3.5" />} text="No deals yet" />
            ) : (
                <div className="space-y-1">
                    {deals.map((d) => {
                        const st = DEAL_STATUS[d.status];
                        return (
                            <div
                                key={d.id}
                                className="rounded-md border border-slate-200 bg-white px-2 py-1.5 flex items-center gap-2"
                            >
                                <div className="min-w-0 flex-1">
                                    <div className="text-[11.5px] font-medium text-slate-900 truncate">{d.name}</div>
                                    <div className="flex items-center gap-1.5 mt-0.5">
                                        <span className={`inline-flex items-center gap-1 text-[10px] ${st.cls}`}>
                                            <span className={`size-1.5 rounded-full ${st.dot}`} />
                                            {st.label}
                                        </span>
                                        {d.stage?.name && <span className="text-[10px] text-slate-400 truncate">· {d.stage.name}</span>}
                                    </div>
                                </div>
                                {d.value != null && (
                                    <span className="font-mono text-[10.5px] text-emerald-700 tabular-nums shrink-0">
                                        {money(d.value, d.currency)}
                                    </span>
                                )}
                            </div>
                        );
                    })}
                </div>
            )}
        </Section>
    );
}

function TasksSection({
    contactId,
    tasks,
    loading,
    defaultTitle,
}: {
    contactId: string;
    tasks: CRMTask[];
    loading: boolean;
    defaultTitle: string;
}) {
    const create = useCreateCRMTask();
    const [open, setOpen] = React.useState(false);
    const [title, setTitle] = React.useState(defaultTitle);

    React.useEffect(() => setTitle(defaultTitle), [defaultTitle]);

    async function submit() {
        if (!title.trim()) {
            toast.error("Task title required");
            return;
        }
        try {
            await toast.promise(
                create.mutateAsync({ title: title.trim(), contact_id: contactId, priority: "medium" } as Partial<CRMTask>),
                { loading: "Adding task…", success: "Task added", error: (e: AppError) => buildError(e) },
            );
            setOpen(false);
        } catch {
            /* surfaced */
        }
    }

    return (
        <Section label="Tasks" action={<AddButton open={open} onClick={() => setOpen((o) => !o)} />}>
            {open && (
                <div className="mb-2 rounded-md border border-slate-200 bg-white p-2 flex items-center gap-1.5">
                    <TextInput value={title} onChange={setTitle} placeholder="Follow-up…" className="w-full" autoFocus />
                    <button
                        type="button"
                        onClick={submit}
                        disabled={create.isPending}
                        className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[11.5px] font-medium inline-flex items-center gap-1 transition-colors disabled:opacity-60 shrink-0"
                    >
                        {create.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CheckIcon className="w-3 h-3" />}
                        Add
                    </button>
                </div>
            )}
            {loading ? (
                <RowSkeleton />
            ) : tasks.length === 0 ? (
                <Empty icon={<CheckSquareIcon className="w-3.5 h-3.5" />} text="No open tasks" />
            ) : (
                <div className="space-y-1">
                    {tasks.map((t) => (
                        <div key={t.id} className="rounded-md border border-slate-200 bg-white px-2 py-1.5 flex items-center gap-2">
                            <span className={`size-1.5 rounded-full shrink-0 ${PRIORITY_DOT[t.priority]}`} />
                            <span className="text-[11.5px] text-slate-800 truncate flex-1">{t.title}</span>
                            {t.due_date && (
                                <span className="font-mono text-[10px] text-slate-400 tabular-nums shrink-0">
                                    {fmtDate(t.due_date)}
                                </span>
                            )}
                        </div>
                    ))}
                </div>
            )}
        </Section>
    );
}

function NotesSection({
    contactId,
    notes,
    loading,
}: {
    contactId: string;
    notes: { id: string; content: string; created_at: Date | string }[];
    loading: boolean;
}) {
    const create = useCreateContactNote();
    const [draft, setDraft] = React.useState("");

    async function submit() {
        if (!draft.trim()) return;
        try {
            await create.mutateAsync({ contactId, data: { content: draft.trim() } });
            setDraft("");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    return (
        <Section label="Notes">
            <div className="mb-2 rounded-md border border-slate-200 bg-white p-2">
                <textarea
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    placeholder="Add a note…"
                    rows={2}
                    className="w-full bg-transparent text-[11.5px] text-slate-900 placeholder:text-slate-400 outline-none resize-none"
                />
                <div className="flex justify-end">
                    <button
                        type="button"
                        onClick={submit}
                        disabled={create.isPending || !draft.trim()}
                        className="h-6 px-2 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[11px] font-medium inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                    >
                        {create.isPending ? <Loader2Icon className="w-2.5 h-2.5 animate-spin" /> : <PlusIcon className="w-2.5 h-2.5" />}
                        Note
                    </button>
                </div>
            </div>
            {loading ? (
                <RowSkeleton />
            ) : notes.length === 0 ? (
                <Empty icon={<StickyNoteIcon className="w-3.5 h-3.5" />} text="No notes yet" />
            ) : (
                <div className="space-y-1.5">
                    {notes.slice(0, 5).map((n) => (
                        <div key={n.id} className="rounded-md border border-slate-200 bg-white px-2 py-1.5">
                            <p className="text-[11.5px] text-slate-700 leading-snug whitespace-pre-wrap break-words">{n.content}</p>
                            <p className="text-[10px] text-slate-400 mt-1 font-mono">{fmtDate(n.created_at)}</p>
                        </div>
                    ))}
                </div>
            )}
        </Section>
    );
}

// ── small presentational helpers ───────────────────────────────────

function Section({
    label,
    hint,
    action,
    children,
}: {
    label: string;
    hint?: string;
    action?: React.ReactNode;
    children?: React.ReactNode;
}) {
    return (
        <div className="px-3 py-3">
            <div className="flex items-center gap-2 mb-2">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{label}</span>
                {action && <span className="ml-auto">{action}</span>}
            </div>
            {hint ? <p className="text-[11px] text-slate-400">{hint}</p> : children}
        </div>
    );
}

function AddButton({ open, onClick }: { open: boolean; onClick: () => void }) {
    return (
        <button
            type="button"
            onClick={onClick}
            className="h-6 px-1.5 rounded-md border border-slate-200 hover:border-slate-300 text-[10.5px] text-slate-600 hover:text-slate-900 inline-flex items-center gap-1 transition-colors"
        >
            {open ? <XIcon className="w-2.5 h-2.5" /> : <PlusIcon className="w-2.5 h-2.5" />}
            {open ? "Cancel" : "Add"}
        </button>
    );
}

function Badge({
    tone,
    icon,
    children,
}: {
    tone: "emerald" | "slate" | "red";
    icon?: React.ReactNode;
    children: React.ReactNode;
}) {
    const cls = {
        emerald: "bg-emerald-50 text-emerald-700 border-emerald-100",
        slate: "bg-slate-100 text-slate-600 border-slate-200",
        red: "bg-red-50 text-red-700 border-red-100",
    }[tone];
    return (
        <span className={`inline-flex items-center gap-1 h-5 px-1.5 rounded border text-[10px] font-medium ${cls}`}>
            {icon}
            {children}
        </span>
    );
}

function Metric({ label, value, accent }: { label: string; value: number; accent?: boolean }) {
    return (
        <div className="rounded-md border border-slate-200 bg-white px-1.5 py-1.5 text-center">
            <div className={`text-[14px] font-light tabular-nums leading-none ${accent ? "text-sky-700" : "text-slate-900"}`}>
                {value}
            </div>
            <div className="text-[9px] uppercase tracking-[0.08em] text-slate-400 mt-1">{label}</div>
        </div>
    );
}

function Empty({ icon, text }: { icon: React.ReactNode; text: string }) {
    return (
        <div className="flex items-center gap-1.5 text-[11px] text-slate-400 py-1">
            <span className="text-slate-300">{icon}</span>
            {text}
        </div>
    );
}

function RowSkeleton() {
    return (
        <div className="space-y-1">
            {[0, 1].map((i) => (
                <div key={i} className="h-8 rounded-md bg-slate-100 animate-pulse" />
            ))}
        </div>
    );
}

function NotAContact({ email }: { email?: string }) {
    return (
        <div className="px-3 py-8 text-center">
            <div className="mx-auto size-9 rounded-md bg-white border border-slate-200 flex items-center justify-center mb-2.5">
                <UserIcon className="w-4 h-4 text-slate-400" />
            </div>
            <p className="text-[12px] font-medium text-slate-700 mb-0.5">Not a known contact</p>
            {email && <p className="text-[11px] text-slate-400 break-all mb-3">{email}</p>}
            <Link
                to="/app/contacts"
                className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[11.5px] text-slate-700 hover:text-slate-900 transition-colors"
            >
                <PlusIcon className="w-3 h-3" />
                Manage contacts
            </Link>
        </div>
    );
}

function initials(name: string): string {
    const parts = name.trim().split(/\s+/).filter(Boolean);
    if (parts.length === 0) return "?";
    if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
    return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

function money(n: number, currency = "USD") {
    try {
        return new Intl.NumberFormat("en-US", { style: "currency", currency: currency || "USD", maximumFractionDigits: 0 }).format(n);
    } catch {
        return `$${Math.round(n).toLocaleString()}`;
    }
}

function fmtDate(d: string | Date) {
    try {
        return new Date(d).toLocaleDateString("en-US", { month: "short", day: "numeric" });
    } catch {
        return "";
    }
}
