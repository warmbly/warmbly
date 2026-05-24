// Tasks — real CRM task list.
//
// Tasks are grouped by due-date bucket: Overdue / Today / Tomorrow /
// This week / Later / No due date. A "Completed" filter pill toggles
// the grouped vs flat view of completed tasks.
//
// Each row has an inline checkbox that toggles status between
// `pending` and `completed`. Clicking the row opens an edit dialog
// for the rest of the fields (title, description, priority, due date,
// status).

import React from "react";
import {
    AlertCircleIcon,
    CalendarClockIcon,
    CheckSquareIcon,
    Loader2Icon,
    PlusIcon,
    SearchIcon,
    SquareIcon,
    TrashIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import { AnimatePresence, motion } from "framer-motion";
import {
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    Stat,
    StatStrip,
    TopbarAction,
} from "@/components/layout/Page";
import { Label, TextInput } from "@/components/ui/field";
import useCRMTasks from "@/lib/api/hooks/app/crm/tasks/useCRMTasks";
import useCreateCRMTask from "@/lib/api/hooks/app/crm/tasks/useCreateCRMTask";
import useUpdateCRMTask from "@/lib/api/hooks/app/crm/tasks/useUpdateCRMTask";
import useDeleteCRMTask from "@/lib/api/hooks/app/crm/tasks/useDeleteCRMTask";
import { useConfirm } from "@/hooks/context/confirm";
import useClickOutside from "@/hooks/useClickOutside";
import type CRMTask from "@/lib/api/models/app/crm/CRMTask";
import type { CRMTaskPriority, CRMTaskStatus } from "@/lib/api/models/app/crm/CRMTask";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

const PRIORITIES: { id: CRMTaskPriority; label: string; dot: string; text: string }[] = [
    { id: "urgent", label: "Urgent", dot: "bg-red-500",     text: "text-red-700" },
    { id: "high",   label: "High",   dot: "bg-amber-500",   text: "text-amber-700" },
    { id: "medium", label: "Medium", dot: "bg-sky-500",     text: "text-sky-700" },
    { id: "low",    label: "Low",    dot: "bg-slate-400",   text: "text-slate-600" },
];

type Bucket = "overdue" | "today" | "tomorrow" | "this_week" | "later" | "no_due";

const BUCKETS: { id: Bucket; label: string; tone: "red" | "sky" | "slate" | "muted" }[] = [
    { id: "overdue",   label: "Overdue",     tone: "red" },
    { id: "today",     label: "Today",       tone: "sky" },
    { id: "tomorrow",  label: "Tomorrow",    tone: "slate" },
    { id: "this_week", label: "This week",   tone: "slate" },
    { id: "later",     label: "Later",       tone: "muted" },
    { id: "no_due",    label: "No due date", tone: "muted" },
];

const TONE = {
    red:    { dot: "bg-red-500",    label: "text-red-600" },
    sky:    { dot: "bg-sky-500",    label: "text-sky-600" },
    slate:  { dot: "bg-slate-400",  label: "text-slate-700" },
    muted:  { dot: "bg-slate-300",  label: "text-slate-500" },
} as const;

function bucketize(due: string | undefined): Bucket {
    if (!due) return "no_due";
    const d = new Date(due);
    if (Number.isNaN(d.getTime())) return "no_due";
    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const tomorrow = new Date(today);
    tomorrow.setDate(tomorrow.getDate() + 1);
    const weekEnd = new Date(today);
    weekEnd.setDate(weekEnd.getDate() + 7);

    const dueDay = new Date(d.getFullYear(), d.getMonth(), d.getDate());

    if (dueDay < today) return "overdue";
    if (dueDay.getTime() === today.getTime()) return "today";
    if (dueDay.getTime() === tomorrow.getTime()) return "tomorrow";
    if (dueDay <= weekEnd) return "this_week";
    return "later";
}

export default function TasksPage() {
    const tasks = useCRMTasks({ limit: 100 });
    const [search, setSearch] = React.useState("");
    const [showCompleted, setShowCompleted] = React.useState(false);
    const [newOpen, setNewOpen] = React.useState(false);
    const [editing, setEditing] = React.useState<CRMTask | null>(null);

    const all = tasks.data?.data ?? [];
    const visible = all
        .filter((t) => (showCompleted ? true : t.status !== "completed"))
        .filter((t) =>
            search.trim()
                ? t.title.toLowerCase().includes(search.trim().toLowerCase()) ||
                  (t.description ?? "").toLowerCase().includes(search.trim().toLowerCase())
                : true,
        );

    const grouped = React.useMemo(() => {
        const g: Record<Bucket, CRMTask[]> = {
            overdue: [],
            today: [],
            tomorrow: [],
            this_week: [],
            later: [],
            no_due: [],
        };
        for (const t of visible) {
            if (t.status === "completed" && showCompleted) {
                // Group completed tasks separately for visibility.
                g.later.push(t);
                continue;
            }
            g[bucketize(t.due_date)].push(t);
        }
        for (const k of Object.keys(g) as Bucket[]) {
            g[k] = g[k].sort((a, b) => {
                const da = a.due_date ? new Date(a.due_date).getTime() : Infinity;
                const db = b.due_date ? new Date(b.due_date).getTime() : Infinity;
                return da - db;
            });
        }
        return g;
    }, [visible, showCompleted]);

    const stats = {
        overdue: grouped.overdue.length,
        today: grouped.today.length,
        week: grouped.this_week.length + grouped.tomorrow.length + grouped.today.length,
        completed: all.filter(
            (t) =>
                t.status === "completed" &&
                t.completed_at &&
                Date.now() - new Date(t.completed_at).getTime() < 7 * 24 * 3600 * 1000,
        ).length,
    };

    return (
        <Page>
            <PageTopbar
                eyebrow="Tasks"
                subtitle="Follow-ups + reminders, grouped by due-date"
            >
                <button
                    type="button"
                    onClick={() => setShowCompleted((c) => !c)}
                    className={`h-7 px-2.5 rounded-md border text-[12px] inline-flex items-center gap-1.5 transition-colors ${
                        showCompleted
                            ? "border-slate-300 bg-slate-100 text-slate-900"
                            : "border-slate-200 text-slate-600 hover:text-slate-900 hover:border-slate-300"
                    }`}
                >
                    <CheckSquareIcon className="w-3 h-3" />
                    Show completed
                </button>
                <SearchPill value={search} onChange={setSearch} />
                <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={() => setNewOpen(true)}>
                    New task
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat label="Overdue" value={stats.overdue} sub="needs attention" />
                <Stat label="Today" value={stats.today} sub="due today" />
                <Stat label="This week" value={stats.week} sub="next 7 days" />
                <Stat label="Completed (7d)" value={stats.completed} sub="last week" last />
            </StatStrip>

            <SectionBar label={tasks.isPending ? "Loading…" : `${visible.length} ${visible.length === 1 ? "task" : "tasks"}`} />
            <PageBody className="px-5 py-5">
                {tasks.isPending ? (
                    <Skeleton />
                ) : visible.length === 0 ? (
                    <EmptyState onCreate={() => setNewOpen(true)} showCompleted={showCompleted} />
                ) : (
                    <div className="space-y-3">
                        {BUCKETS.map((b) => {
                            const items = grouped[b.id];
                            if (items.length === 0) return null;
                            return (
                                <BucketGroup key={b.id} bucket={b} tasks={items} onOpen={setEditing} />
                            );
                        })}
                    </div>
                )}
            </PageBody>

            <TaskDialog open={newOpen} onClose={() => setNewOpen(false)} />
            <TaskDialog open={!!editing} onClose={() => setEditing(null)} editing={editing ?? undefined} />
        </Page>
    );
}

function BucketGroup({
    bucket,
    tasks,
    onOpen,
}: {
    bucket: { id: Bucket; label: string; tone: keyof typeof TONE };
    tasks: CRMTask[];
    onOpen: (t: CRMTask) => void;
}) {
    return (
        <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
            <div className="h-8 px-3 border-b border-slate-200 flex items-center gap-1.5">
                <span className={`size-1.5 rounded-full ${TONE[bucket.tone].dot}`} />
                <span className={`text-[11px] uppercase tracking-[0.1em] font-semibold ${TONE[bucket.tone].label}`}>
                    {bucket.label}
                </span>
                <span className="ml-auto font-mono text-[10.5px] text-slate-400 tabular-nums">
                    {tasks.length}
                </span>
            </div>
            <div className="divide-y divide-slate-200/60">
                {tasks.map((t) => (
                    <TaskRow key={t.id} task={t} onOpen={onOpen} />
                ))}
            </div>
        </div>
    );
}

function TaskRow({ task, onOpen }: { task: CRMTask; onOpen: (t: CRMTask) => void }) {
    const update = useUpdateCRMTask();
    const isDone = task.status === "completed";
    const priority = PRIORITIES.find((p) => p.id === task.priority) ?? PRIORITIES[3];

    async function toggle(e: React.MouseEvent) {
        e.stopPropagation();
        const next: CRMTaskStatus = isDone ? "pending" : "completed";
        try {
            await update.mutateAsync({
                id: task.id,
                data: { status: next } as Partial<CRMTask>,
            });
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    return (
        <div
            onClick={() => onOpen(task)}
            className="h-10 px-3 flex items-center gap-2.5 hover:bg-slate-50 cursor-pointer transition-colors"
        >
            <button
                type="button"
                onClick={toggle}
                aria-label={isDone ? "Mark pending" : "Mark complete"}
                className="size-4 rounded text-slate-400 hover:text-slate-900 inline-flex items-center justify-center shrink-0"
            >
                {isDone ? (
                    <CheckSquareIcon className="w-3.5 h-3.5 text-emerald-600" />
                ) : (
                    <SquareIcon className="w-3.5 h-3.5" />
                )}
            </button>
            <span
                className={`text-[12px] truncate flex-1 ${
                    isDone ? "text-slate-400 line-through" : "text-slate-900"
                }`}
            >
                {task.title}
            </span>
            <span className={`inline-flex items-center gap-1 text-[10.5px] uppercase tracking-[0.08em] font-semibold ${priority.text}`}>
                <span className={`size-1.5 rounded-full ${priority.dot}`} />
                {priority.label}
            </span>
            <span className="inline-flex items-center gap-1 font-mono text-[10.5px] text-slate-400 tabular-nums shrink-0 w-20 justify-end">
                {task.due_date && (
                    <>
                        <CalendarClockIcon className="w-2.5 h-2.5" />
                        {fmtDue(task.due_date)}
                    </>
                )}
            </span>
        </div>
    );
}

function SearchPill({ value, onChange }: { value: string; onChange: (v: string) => void }) {
    return (
        <div className="h-7 px-2 rounded-md border border-slate-200 bg-white flex items-center gap-1.5 focus-within:border-sky-400 transition-colors">
            <SearchIcon className="w-3 h-3 text-slate-400" />
            <input
                value={value}
                onChange={(e) => onChange(e.target.value)}
                placeholder="Search tasks…"
                className="w-[160px] h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
            />
            {value && (
                <button type="button" onClick={() => onChange("")} aria-label="Clear" className="text-slate-400 hover:text-slate-700">
                    <XIcon className="w-3 h-3" />
                </button>
            )}
        </div>
    );
}

function Skeleton() {
    return (
        <div className="space-y-3">
            {[0, 1].map((i) => (
                <div key={i} className="rounded-md border border-slate-200 bg-white overflow-hidden">
                    <div className="h-8 border-b border-slate-200 px-3 flex items-center">
                        <div className="h-3 w-20 bg-slate-200 rounded animate-pulse" />
                    </div>
                    <div className="divide-y divide-slate-200/60">
                        {[0, 1].map((j) => (
                            <div key={j} className="h-10 px-3 flex items-center gap-2.5">
                                <div className="size-3.5 rounded bg-slate-200 animate-pulse" />
                                <div className="flex-1 h-3 bg-slate-200 rounded animate-pulse" />
                            </div>
                        ))}
                    </div>
                </div>
            ))}
        </div>
    );
}

function EmptyState({
    onCreate,
    showCompleted,
}: {
    onCreate: () => void;
    showCompleted: boolean;
}) {
    return (
        <div className="rounded-md border border-dashed border-slate-300 bg-slate-50/40 p-8 text-center">
            <div className="mx-auto size-9 rounded-md bg-white border border-slate-200 flex items-center justify-center mb-3">
                <CheckSquareIcon className="w-4 h-4 text-slate-400" />
            </div>
            <h3 className="text-[13px] font-semibold text-slate-900 mb-1">
                {showCompleted ? "No tasks yet" : "Nothing on deck"}
            </h3>
            <p className="text-[12px] text-slate-500 max-w-md mx-auto mb-4 leading-relaxed">
                Tasks attach to a contact or a deal and show up grouped by due-date.
            </p>
            <button
                type="button"
                onClick={onCreate}
                className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
            >
                <PlusIcon className="w-3 h-3" />
                Create task
            </button>
        </div>
    );
}

function TaskDialog({
    open,
    onClose,
    editing,
}: {
    open: boolean;
    onClose: () => void;
    editing?: CRMTask;
}) {
    const create = useCreateCRMTask();
    const update = useUpdateCRMTask();
    const del = useDeleteCRMTask();
    const confirm = useConfirm();

    const [title, setTitle] = React.useState("");
    const [description, setDescription] = React.useState("");
    const [dueDate, setDueDate] = React.useState("");
    const [priority, setPriority] = React.useState<CRMTaskPriority>("medium");
    const [status, setStatus] = React.useState<CRMTaskStatus>("pending");

    React.useEffect(() => {
        if (!open) return;
        if (editing) {
            setTitle(editing.title);
            setDescription(editing.description ?? "");
            setDueDate(editing.due_date ? String(editing.due_date).split("T")[0] : "");
            setPriority(editing.priority);
            setStatus(editing.status);
        } else {
            setTitle("");
            setDescription("");
            setDueDate("");
            setPriority("medium");
            setStatus("pending");
        }
    }, [open, editing]);

    async function submit() {
        if (!title.trim()) {
            toast.error("Title required");
            return;
        }
        const data: Partial<CRMTask> = {
            title: title.trim(),
            priority,
        };
        if (description.trim()) data.description = description.trim();
        if (dueDate) data.due_date = new Date(dueDate).toISOString();
        if (editing) data.status = status;

        try {
            if (editing) {
                await toast.promise(update.mutateAsync({ id: editing.id, data }), {
                    loading: "Saving…",
                    success: "Task updated",
                    error: (e: AppError) => buildError(e),
                });
            } else {
                await toast.promise(create.mutateAsync(data), {
                    loading: "Creating task…",
                    success: "Task created",
                    error: (e: AppError) => buildError(e),
                });
            }
            onClose();
        } catch {
            /* surfaced */
        }
    }

    function doDelete() {
        if (!editing) return;
        confirm?.show(`Delete task "${editing.title}"?`, async () => {
            try {
                await toast.promise(del.mutateAsync(editing.id), {
                    loading: "Deleting…",
                    success: "Task deleted",
                    error: (e: AppError) => buildError(e),
                });
                onClose();
            } catch {
                /* surfaced */
            }
        });
    }

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onClick={onClose}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.16 }}
                        onClick={(e) => e.stopPropagation()}
                        className="w-full max-w-[480px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18)] overflow-hidden"
                    >
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <CheckSquareIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                {editing ? "Edit" : "New"}
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">Task</span>
                            {editing && (
                                <button
                                    type="button"
                                    onClick={doDelete}
                                    className="ml-2 size-7 rounded-md text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors"
                                    aria-label="Delete task"
                                >
                                    <TrashIcon className="w-3 h-3" />
                                </button>
                            )}
                            <button
                                type="button"
                                onClick={onClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        <div className="px-4 py-4 space-y-3">
                            <div>
                                <Label>Title</Label>
                                <TextInput
                                    value={title}
                                    onChange={setTitle}
                                    placeholder="e.g. Follow up on Acme cold reply"
                                    autoFocus
                                    className="w-full"
                                />
                            </div>
                            <div>
                                <Label>Description</Label>
                                <textarea
                                    value={description}
                                    onChange={(e) => setDescription(e.target.value)}
                                    placeholder="Optional notes…"
                                    rows={3}
                                    className="w-full px-2.5 py-1.5 rounded-md border border-slate-200 bg-white text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none transition-colors focus:border-sky-400 focus:ring-2 focus:ring-sky-100 resize-y"
                                />
                            </div>
                            <div className="grid grid-cols-2 gap-2">
                                <div>
                                    <Label>Due date</Label>
                                    <TextInput value={dueDate} onChange={setDueDate} type="date" className="w-full" />
                                </div>
                                <div>
                                    <Label>Priority</Label>
                                    <PriorityPill value={priority} onChange={setPriority} />
                                </div>
                            </div>
                            {editing && (
                                <div>
                                    <Label>Status</Label>
                                    <div className="inline-flex rounded-md border border-slate-200 bg-white p-0.5 w-full">
                                        {(
                                            [
                                                ["pending", "Pending"],
                                                ["in_progress", "In progress"],
                                                ["completed", "Done"],
                                                ["cancelled", "Cancelled"],
                                            ] as const
                                        ).map(([id, label]) => (
                                            <button
                                                key={id}
                                                type="button"
                                                onClick={() => setStatus(id)}
                                                className={`flex-1 h-6 px-2 rounded text-[11px] font-medium transition-colors ${
                                                    status === id
                                                        ? "bg-slate-900 text-white"
                                                        : "text-slate-500 hover:text-slate-900"
                                                }`}
                                            >
                                                {label}
                                            </button>
                                        ))}
                                    </div>
                                </div>
                            )}
                        </div>

                        <div className="px-3 h-12 border-t border-slate-200 flex items-center gap-1.5">
                            <button
                                type="button"
                                onClick={onClose}
                                className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={submit}
                                disabled={create.isPending || update.isPending}
                                className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {(create.isPending || update.isPending) && (
                                    <Loader2Icon className="w-3 h-3 animate-spin" />
                                )}
                                {editing ? "Save task" : "Create task"}
                            </button>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function PriorityPill({
    value,
    onChange,
}: {
    value: CRMTaskPriority;
    onChange: (p: CRMTaskPriority) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));
    const cur = PRIORITIES.find((p) => p.id === value)!;

    return (
        <div ref={ref} className="relative">
            <button
                type="button"
                onClick={() => setOpen((o) => !o)}
                className="h-7 w-full px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1.5"
            >
                <span className={`size-1.5 rounded-full ${cur.dot}`} />
                <span className="truncate">{cur.label}</span>
                <span className="ml-auto text-slate-400">▾</span>
            </button>
            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={{ opacity: 0, y: -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -4 }}
                        transition={{ duration: 0.12 }}
                        className="absolute top-full left-0 right-0 mt-1 z-30 rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] py-1"
                    >
                        {PRIORITIES.map((p) => (
                            <button
                                key={p.id}
                                type="button"
                                onClick={() => {
                                    onChange(p.id);
                                    setOpen(false);
                                }}
                                className={`w-full px-2.5 h-7 flex items-center gap-2 text-[12px] transition-colors ${
                                    p.id === value ? "bg-slate-100 text-slate-900" : "text-slate-700 hover:bg-slate-100"
                                }`}
                            >
                                <span className={`size-1.5 rounded-full ${p.dot}`} />
                                <span className="truncate">{p.label}</span>
                            </button>
                        ))}
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

function fmtDue(d: string) {
    try {
        const dt = new Date(d);
        const now = new Date();
        const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
        const tomorrow = new Date(today);
        tomorrow.setDate(tomorrow.getDate() + 1);
        const dueDay = new Date(dt.getFullYear(), dt.getMonth(), dt.getDate());

        if (dueDay.getTime() === today.getTime()) return "Today";
        if (dueDay.getTime() === tomorrow.getTime()) return "Tomorrow";
        return dt.toLocaleDateString("en-US", { month: "short", day: "numeric" });
    } catch {
        return "—";
    }
}

void AlertCircleIcon; // kept for future overdue badge
