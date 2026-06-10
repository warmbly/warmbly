// Tasks — the server-driven, scale-safe CRM task surface.
//
// This page is built to hold thousands of tasks. Unlike a client-side list
// that slices the first 100 rows and totals them in memory, it:
//   - searches, filters and sorts on the server (status, priority, type,
//     assignee, overdue, text) via POST /crm/tasks/search
//   - pages the full result set with offset infinite scroll ("N of M loaded")
//   - reads every header total from POST /crm/tasks/summary, so the Overdue /
//     High priority / Completed numbers are COUNTs over the whole filtered set,
//     never a reduce over the loaded page
//   - resolves assigned_to user ids to org members (useMembers) for a real
//     ASSIGNEE column (avatar initials + name) and an assignee facet
//
// Two views share the one paged result list: a flat table and a grouped
// "by due-date" view. Both page server-side; the grouped view buckets only the
// rows already loaded, and the same "Load more" affordance pulls the next page.
//
// The inline checkbox toggles status between pending and completed. Clicking a
// row opens the create/edit dialog (title, description, type, due date,
// priority, status), which preserves the TaskTypePicker + themed selectors.

import React from "react";
import {
    AlertTriangleIcon,
    ArrowUpDownIcon,
    CalendarClockIcon,
    CheckSquareIcon,
    LayoutListIcon,
    ListTreeIcon,
    Loader2Icon,
    MoreHorizontalIcon,
    PencilIcon,
    PlusIcon,
    SquareIcon,
    TagIcon,
    TrashIcon,
    UsersIcon,
    UsersRoundIcon,
    XIcon,
} from "lucide-react";
import { Link } from "react-router-dom";
import useTeams from "@/lib/api/hooks/app/teams/useTeams";
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
import { Label, SearchInput, TextInput } from "@/components/ui/field";
import DueInDays from "@/components/app/crm/DueInDays";
import { dueInDaysToISO, isoToDueInDays } from "@/lib/helper/dueDate";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import useSearchTasks from "@/lib/api/hooks/app/crm/tasks/useSearchTasks";
import useTasksSummary from "@/lib/api/hooks/app/crm/tasks/useTasksSummary";
import useCreateCRMTask from "@/lib/api/hooks/app/crm/tasks/useCreateCRMTask";
import useUpdateCRMTask from "@/lib/api/hooks/app/crm/tasks/useUpdateCRMTask";
import useDeleteCRMTask from "@/lib/api/hooks/app/crm/tasks/useDeleteCRMTask";
import useTaskTypes from "@/lib/api/hooks/app/crm/taskTypes/useTaskTypes";
import useMembers from "@/lib/api/hooks/app/organizations/useMembers";
import { useConfirm } from "@/hooks/context/confirm";
import type CRMTask from "@/lib/api/models/app/crm/CRMTask";
import type { CRMTaskPriority, CRMTaskStatus } from "@/lib/api/models/app/crm/CRMTask";
import type SearchTasks from "@/lib/api/models/app/crm/SearchTasks";
import type { TaskSortBy } from "@/lib/api/models/app/crm/SearchTasks";
import { EMPTY_TASK_SEARCH } from "@/lib/api/models/app/crm/SearchTasks";
import type OrganizationMember from "@/lib/api/models/app/organizations/OrganizationMember";
import type Team from "@/lib/api/models/app/teams/Team";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import TaskTypePicker from "@/components/app/crm/TaskTypePicker";
import { taskTypeColor } from "@/components/app/crm/taskTypes";

const PRIORITIES: { id: CRMTaskPriority; label: string; dot: string; text: string }[] = [
    { id: "urgent", label: "Urgent", dot: "bg-red-500", text: "text-red-700" },
    { id: "high", label: "High", dot: "bg-amber-500", text: "text-amber-700" },
    { id: "medium", label: "Medium", dot: "bg-sky-500", text: "text-sky-700" },
    { id: "low", label: "Low", dot: "bg-slate-400", text: "text-slate-600" },
];

const STATUS_TABS: { id: "all" | CRMTaskStatus; label: string }[] = [
    { id: "all", label: "All" },
    { id: "pending", label: "Pending" },
    { id: "in_progress", label: "Active" },
    { id: "completed", label: "Done" },
    { id: "cancelled", label: "Cancelled" },
];

const SORTS: { id: string; label: string; sort_by: TaskSortBy; reverse: boolean }[] = [
    { id: "newest", label: "Newest", sort_by: "created_at", reverse: false },
    { id: "oldest", label: "Oldest", sort_by: "created_at", reverse: true },
    { id: "due_soon", label: "Due soonest", sort_by: "due_date", reverse: true },
    { id: "due_late", label: "Due latest", sort_by: "due_date", reverse: false },
    { id: "priority", label: "Priority · high → low", sort_by: "priority", reverse: false },
    { id: "title", label: "Title · A → Z", sort_by: "title", reverse: true },
];

type Bucket = "overdue" | "today" | "tomorrow" | "this_week" | "later" | "no_due";

const BUCKETS: { id: Bucket; label: string; tone: "red" | "sky" | "slate" | "muted" }[] = [
    { id: "overdue", label: "Overdue", tone: "red" },
    { id: "today", label: "Today", tone: "sky" },
    { id: "tomorrow", label: "Tomorrow", tone: "slate" },
    { id: "this_week", label: "This week", tone: "slate" },
    { id: "later", label: "Later", tone: "muted" },
    { id: "no_due", label: "No due date", tone: "muted" },
];

const TONE = {
    red: { dot: "bg-red-500", label: "text-red-600" },
    sky: { dot: "bg-sky-500", label: "text-sky-600" },
    slate: { dot: "bg-slate-400", label: "text-slate-700" },
    muted: { dot: "bg-slate-300", label: "text-slate-500" },
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

// Whether a task is past due AND still actionable (mirrors the server's
// `overdue` predicate so the row badge agrees with the summary count).
function isOverdue(t: CRMTask): boolean {
    if (!t.due_date) return false;
    if (t.status === "completed" || t.status === "cancelled") return false;
    const d = new Date(t.due_date);
    return !Number.isNaN(d.getTime()) && d.getTime() < Date.now();
}

export default function TasksPage() {
    const [filters, setFilters] = React.useState<SearchTasks>(EMPTY_TASK_SEARCH);
    const [view, setView] = React.useState<"flat" | "grouped">("flat");
    const [newOpen, setNewOpen] = React.useState(false);
    const [editing, setEditing] = React.useState<CRMTask | null>(null);

    const search = useSearchTasks({ filters, limit: 50 });
    const summary = useTasksSummary(filters);
    const tasks = search.tasks ?? [];
    const total = search.total;
    const sum = summary.data;

    const { data: members = [] } = useMembers();
    const memberByUser = React.useMemo(() => {
        const m = new Map<string, OrganizationMember>();
        for (const mem of members) m.set(mem.user_id, mem);
        return m;
    }, [members]);

    const { data: teams = [] } = useTeams();
    const teamById = React.useMemo(() => {
        const m = new Map<string, Team>();
        for (const t of teams) m.set(t.id, t);
        return m;
    }, [teams]);

    const { data: types = [] } = useTaskTypes();

    const statusTab: "all" | CRMTaskStatus =
        filters.statuses.length === 1 ? filters.statuses[0] : "all";

    function setStatusTab(tab: "all" | CRMTaskStatus) {
        setFilters((f) => ({ ...f, statuses: tab === "all" ? [] : [tab] }));
    }

    const activeSort =
        SORTS.find((s) => s.sort_by === filters.sort_by && s.reverse === filters.reverse) ?? SORTS[0];

    const advancedCount =
        filters.priorities.length +
        filters.types.length +
        filters.assigned_to.length +
        filters.team_ids.length +
        (filters.overdue ? 1 : 0);

    return (
        <Page>
            <PageTopbar eyebrow="Tasks" subtitle="Follow-ups + reminders across the org">
                <ViewToggle view={view} onChange={setView} />
                <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={() => setNewOpen(true)}>
                    New task
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={5}>
                <Stat
                    label="Overdue"
                    value={sum ? sum.overdue_count : "—"}
                    sub="needs attention"
                    accent={!!sum && sum.overdue_count > 0}
                />
                <Stat
                    label="High priority"
                    value={sum ? sum.high_priority_count : "—"}
                    sub="urgent + high"
                />
                <Stat label="Pending" value={sum ? sum.pending_count : "—"} sub="not started" />
                <Stat label="Active" value={sum ? sum.in_progress_count : "—"} sub="in progress" />
                <Stat label="Completed" value={sum ? sum.completed_count : "—"} sub="done" last />
            </StatStrip>

            <SectionBar label={search.isPending ? "Loading…" : `${total} ${total === 1 ? "task" : "tasks"}`}>
                <SearchInput
                    value={filters.query}
                    onChange={(v) => setFilters((f) => ({ ...f, query: v }))}
                    placeholder="Search tasks…"
                    className="w-full sm:w-[180px]"
                />
                <div className="inline-flex rounded-md bg-slate-100 p-0.5 gap-0.5">
                    {STATUS_TABS.map((t) => (
                        <button
                            key={t.id}
                            type="button"
                            onClick={() => setStatusTab(t.id)}
                            className={`h-6 px-2 rounded text-[11px] font-medium transition-colors ${
                                statusTab === t.id
                                    ? "bg-white text-slate-900 shadow-sm"
                                    : "text-slate-500 hover:text-slate-900"
                            }`}
                        >
                            {t.label}
                        </button>
                    ))}
                </div>
                <AssigneeFacet
                    members={members}
                    selected={filters.assigned_to}
                    onChange={(ids) => setFilters((f) => ({ ...f, assigned_to: ids }))}
                />
                <TeamFacet
                    teams={teams}
                    selected={filters.team_ids}
                    onChange={(ids) => setFilters((f) => ({ ...f, team_ids: ids }))}
                />
                <TypeFacet
                    types={types}
                    selected={filters.types}
                    onChange={(names) => setFilters((f) => ({ ...f, types: names }))}
                />
                <FilterPopover filters={filters} onChange={setFilters} activeCount={advancedCount} />
                <SortPopover
                    active={activeSort.id}
                    onChange={(s) => setFilters((f) => ({ ...f, sort_by: s.sort_by, reverse: s.reverse }))}
                />
            </SectionBar>

            <PageBody className={view === "grouped" ? "px-5 py-5" : ""}>
                {search.isError ? (
                    <div className="px-5 py-16 text-center text-[12.5px] text-red-600">
                        Couldn't load tasks. Try again.
                    </div>
                ) : search.isPending ? (
                    view === "grouped" ? (
                        <GroupedSkeleton />
                    ) : (
                        <FlatSkeleton />
                    )
                ) : tasks.length === 0 ? (
                    <EmptyState
                        hasFilters={total === 0 && hasAnyFilter(filters)}
                        onCreate={() => setNewOpen(true)}
                        onClear={() => setFilters(EMPTY_TASK_SEARCH)}
                    />
                ) : view === "grouped" ? (
                    <GroupedView
                        tasks={tasks}
                        memberByUser={memberByUser}
                        teamById={teamById}
                        types={types}
                        onOpen={setEditing}
                    />
                ) : (
                    <FlatView
                        tasks={tasks}
                        memberByUser={memberByUser}
                        teamById={teamById}
                        types={types}
                        onOpen={setEditing}
                    />
                )}

                {!search.isPending && tasks.length > 0 && (
                    <LoadMore
                        hasNextPage={!!search.hasNextPage}
                        isFetchingNextPage={search.isFetchingNextPage}
                        onLoadMore={() => search.fetchNextPage()}
                        loaded={tasks.length}
                        total={total}
                    />
                )}
            </PageBody>

            <TaskDialog
                open={newOpen}
                onClose={() => setNewOpen(false)}
                members={members}
                teams={teams}
            />
            <TaskDialog
                open={!!editing}
                onClose={() => setEditing(null)}
                editing={editing ?? undefined}
                members={members}
                teams={teams}
            />
        </Page>
    );
}

// ── Views ────────────────────────────────────────────────────────────────

function FlatView({
    tasks,
    memberByUser,
    teamById,
    types,
    onOpen,
}: {
    tasks: CRMTask[];
    memberByUser: Map<string, OrganizationMember>;
    teamById: Map<string, Team>;
    types: { name: string; color: string }[];
    onOpen: (t: CRMTask) => void;
}) {
    return (
        <table className="w-full border-collapse">
            <thead className="sticky top-0 bg-white z-[1]">
                <tr className="border-b border-slate-200">
                    <Th className="text-left">Task</Th>
                    <Th className="text-left hidden md:table-cell">Type</Th>
                    <Th className="text-left hidden md:table-cell">Assignee</Th>
                    <Th className="text-left">Priority</Th>
                    <Th className="text-left hidden md:table-cell">Status</Th>
                    <Th className="text-left">Due</Th>
                    <Th className="text-right"> </Th>
                </tr>
            </thead>
            <tbody>
                {tasks.map((t) => (
                    <FlatRow
                        key={t.id}
                        task={t}
                        member={t.assigned_to ? memberByUser.get(t.assigned_to) : undefined}
                        team={t.assigned_team_id ? teamById.get(t.assigned_team_id) : undefined}
                        types={types}
                        onOpen={() => onOpen(t)}
                    />
                ))}
            </tbody>
        </table>
    );
}

function FlatRow({
    task,
    member,
    team,
    types,
    onOpen,
}: {
    task: CRMTask;
    member?: OrganizationMember;
    team?: Team;
    types: { name: string; color: string }[];
    onOpen: () => void;
}) {
    const update = useUpdateCRMTask();
    const del = useDeleteCRMTask();
    const confirm = useConfirm();
    const [menuOpen, setMenuOpen] = React.useState(false);
    const isDone = task.status === "completed";
    const priority = PRIORITIES.find((p) => p.id === task.priority) ?? PRIORITIES[3];
    const overdue = isOverdue(task);

    async function setDone(done: boolean) {
        try {
            await update.mutateAsync({
                id: task.id,
                data: { status: done ? "completed" : "pending" } as Partial<CRMTask>,
            });
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    function doDelete() {
        confirm?.show(`Delete task "${task.title}"?`, async () => {
            try {
                await del.mutateAsync(task.id);
            } catch (err) {
                toast.error(buildError(err as AppError));
            }
        });
    }

    return (
        <tr
            onClick={onOpen}
            className="group h-11 border-b border-slate-200/60 hover:bg-slate-50/80 cursor-pointer transition-colors"
        >
            <td className="px-3 max-w-0">
                <div className="flex items-center gap-2.5 min-w-0">
                    <button
                        type="button"
                        onClick={(e) => {
                            e.stopPropagation();
                            setDone(!isDone);
                        }}
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
                        className={`text-[12.5px] truncate ${
                            isDone ? "text-slate-400 line-through" : "text-slate-900 font-medium"
                        }`}
                    >
                        {task.title}
                    </span>
                </div>
            </td>
            <td className="px-3 whitespace-nowrap hidden md:table-cell">
                <TaskTypeTag type={task.type} types={types} done={isDone} />
            </td>
            <td className="px-3 whitespace-nowrap hidden md:table-cell">
                <AssigneeCell
                    member={member}
                    assignedTo={task.assigned_to}
                    team={team}
                    assignedTeamId={task.assigned_team_id}
                />
            </td>
            <td className="px-3 whitespace-nowrap">
                <span
                    className={`inline-flex items-center gap-1.5 text-[11px] uppercase tracking-[0.08em] font-semibold ${priority.text}`}
                >
                    <span className={`size-1.5 rounded-full ${priority.dot}`} />
                    {priority.label}
                </span>
            </td>
            <td className="px-3 whitespace-nowrap hidden md:table-cell">
                <StatusTag status={task.status} />
            </td>
            <td className="px-3 whitespace-nowrap">
                <DueCell due={task.due_date} overdue={overdue} />
            </td>
            <td className="px-2 w-9 text-right" onClick={(e) => e.stopPropagation()}>
                <PopoverMenu open={menuOpen} onOpenChange={setMenuOpen} align="end">
                    <PopoverMenuTrigger asChild>
                        <button
                            type="button"
                            aria-label="Task actions"
                            className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-opacity opacity-100 md:opacity-0 md:group-hover:opacity-100"
                        >
                            <MoreHorizontalIcon className="w-4 h-4" />
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={170}>
                        <PopoverMenuItem onSelect={onOpen} icon={<PencilIcon className="w-3 h-3" />}>
                            Edit
                        </PopoverMenuItem>
                        <PopoverMenuItem
                            onSelect={() => setDone(!isDone)}
                            icon={isDone ? <SquareIcon className="w-3 h-3" /> : <CheckSquareIcon className="w-3 h-3" />}
                        >
                            {isDone ? "Mark pending" : "Mark complete"}
                        </PopoverMenuItem>
                        <PopoverMenuItem onSelect={doDelete} danger icon={<TrashIcon className="w-3 h-3" />}>
                            Delete
                        </PopoverMenuItem>
                    </PopoverMenuContent>
                </PopoverMenu>
            </td>
        </tr>
    );
}

function GroupedView({
    tasks,
    memberByUser,
    teamById,
    types,
    onOpen,
}: {
    tasks: CRMTask[];
    memberByUser: Map<string, OrganizationMember>;
    teamById: Map<string, Team>;
    types: { name: string; color: string }[];
    onOpen: (t: CRMTask) => void;
}) {
    // Bucket only the rows already paged in. "Load more" pulls the next server
    // page, so this is never an in-memory slice of a larger set: it is exactly
    // the rows we have, regrouped.
    const grouped = React.useMemo(() => {
        const g: Record<Bucket, CRMTask[]> = {
            overdue: [],
            today: [],
            tomorrow: [],
            this_week: [],
            later: [],
            no_due: [],
        };
        for (const t of tasks) {
            // A completed/cancelled past-due task is not "overdue" work, so it
            // falls through to its real calendar bucket instead.
            const b = isOverdue(t) ? "overdue" : bucketize(t.due_date);
            g[b].push(t);
        }
        for (const k of Object.keys(g) as Bucket[]) {
            g[k] = g[k].sort((a, b) => {
                const da = a.due_date ? new Date(a.due_date).getTime() : Infinity;
                const db = b.due_date ? new Date(b.due_date).getTime() : Infinity;
                return da - db;
            });
        }
        return g;
    }, [tasks]);

    return (
        <div className="space-y-3">
            {BUCKETS.map((b) => {
                const items = grouped[b.id];
                if (items.length === 0) return null;
                return (
                    <BucketGroup
                        key={b.id}
                        bucket={b}
                        tasks={items}
                        memberByUser={memberByUser}
                        teamById={teamById}
                        types={types}
                        onOpen={onOpen}
                    />
                );
            })}
        </div>
    );
}

function BucketGroup({
    bucket,
    tasks,
    memberByUser,
    teamById,
    types,
    onOpen,
}: {
    bucket: { id: Bucket; label: string; tone: keyof typeof TONE };
    tasks: CRMTask[];
    memberByUser: Map<string, OrganizationMember>;
    teamById: Map<string, Team>;
    types: { name: string; color: string }[];
    onOpen: (t: CRMTask) => void;
}) {
    return (
        <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
            <div className="h-8 px-3 border-b border-slate-200 flex items-center gap-1.5">
                <span className={`size-1.5 rounded-full ${TONE[bucket.tone].dot}`} />
                <span
                    className={`text-[11px] uppercase tracking-[0.1em] font-semibold ${TONE[bucket.tone].label}`}
                >
                    {bucket.label}
                </span>
                <span className="ml-auto font-mono text-[10.5px] text-slate-400 tabular-nums">
                    {tasks.length}
                </span>
            </div>
            <div className="divide-y divide-slate-200/60">
                {tasks.map((t) => (
                    <GroupedRow
                        key={t.id}
                        task={t}
                        member={t.assigned_to ? memberByUser.get(t.assigned_to) : undefined}
                        team={t.assigned_team_id ? teamById.get(t.assigned_team_id) : undefined}
                        types={types}
                        onOpen={onOpen}
                    />
                ))}
            </div>
        </div>
    );
}

function GroupedRow({
    task,
    member,
    team,
    types,
    onOpen,
}: {
    task: CRMTask;
    member?: OrganizationMember;
    team?: Team;
    types: { name: string; color: string }[];
    onOpen: (t: CRMTask) => void;
}) {
    const update = useUpdateCRMTask();
    const isDone = task.status === "completed";
    const priority = PRIORITIES.find((p) => p.id === task.priority) ?? PRIORITIES[3];
    const overdue = isOverdue(task);

    async function toggle(e: React.MouseEvent) {
        e.stopPropagation();
        const next: CRMTaskStatus = isDone ? "pending" : "completed";
        try {
            await update.mutateAsync({ id: task.id, data: { status: next } as Partial<CRMTask> });
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
            <TaskTypeTag type={task.type} types={types} done={isDone} compact />
            <span
                className={`text-[12px] truncate flex-1 ${
                    isDone ? "text-slate-400 line-through" : "text-slate-900"
                }`}
            >
                {task.title}
            </span>
            <AssigneeCell
                member={member}
                assignedTo={task.assigned_to}
                team={team}
                assignedTeamId={task.assigned_team_id}
                compact
            />
            <span
                className={`hidden md:inline-flex items-center gap-1 text-[10.5px] uppercase tracking-[0.08em] font-semibold ${priority.text}`}
            >
                <span className={`size-1.5 rounded-full ${priority.dot}`} />
                {priority.label}
            </span>
            <span className="inline-flex items-center gap-1 font-mono text-[10.5px] tabular-nums shrink-0 w-20 justify-end">
                <DueCell due={task.due_date} overdue={overdue} />
            </span>
        </div>
    );
}

// ── Cells ────────────────────────────────────────────────────────────────

function memberLabel(member?: OrganizationMember, assignedTo?: string): string {
    const name = member?.name?.trim();
    if (name) return name;
    const email = member?.email?.trim();
    if (email) return email.split("@")[0] || email;
    // No resolved name/email yet (e.g. the member API hasn't been re-read).
    // Show a distinct, truthful identifier per person rather than "Unknown"
    // for everyone, so the list never reads as duplicate/phantom rows.
    const id = member?.user_id ?? assignedTo;
    if (id) return `Member ${id.slice(0, 6)}`;
    return "";
}

function memberInitials(member?: OrganizationMember, assignedTo?: string): string {
    const src = (member?.name?.trim() || member?.email?.trim() || "").trim();
    if (src) {
        const local = src.split("@")[0] || src;
        const parts = local.split(/[\s.\-_+]/).filter(Boolean);
        if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
        return (local.slice(0, 2) || "?").toUpperCase();
    }
    const id = member?.user_id ?? assignedTo;
    return id ? id.slice(0, 2).toUpperCase() : "?";
}

function AssigneeCell({
    member,
    assignedTo,
    team,
    assignedTeamId,
    compact = false,
}: {
    member?: OrganizationMember;
    assignedTo?: string;
    team?: Team;
    assignedTeamId?: string;
    compact?: boolean;
}) {
    // A task may carry a person, a team, both, or neither. Render whichever are
    // present; only fall back to "Unassigned" when nothing is set.
    if (!assignedTo && !assignedTeamId) {
        return <span className="text-slate-300 text-[11.5px]">{compact ? "" : "Unassigned"}</span>;
    }
    const label = memberLabel(member, assignedTo);
    const initials = memberInitials(member, assignedTo);
    return (
        <span className="inline-flex items-center gap-1.5 min-w-0">
            {assignedTo && (
                <span
                    className="inline-flex items-center gap-1.5 min-w-0"
                    title={member?.name || member?.email || assignedTo}
                >
                    <span className="size-5 shrink-0 rounded-full bg-sky-50 border border-sky-200 text-sky-700 text-[9px] font-semibold inline-flex items-center justify-center uppercase tracking-tight">
                        {initials}
                    </span>
                    {!compact && <span className="text-[11.5px] text-slate-600 truncate">{label}</span>}
                </span>
            )}
            {assignedTeamId && <TeamChip team={team} teamId={assignedTeamId} compact={compact} />}
        </span>
    );
}

function teamLabel(team?: Team, teamId?: string): string {
    const name = team?.name?.trim();
    if (name) return name;
    return teamId ? `Team ${teamId.slice(0, 6)}` : "";
}

function TeamChip({ team, teamId, compact = false }: { team?: Team; teamId?: string; compact?: boolean }) {
    const label = teamLabel(team, teamId);
    return (
        <span
            className="inline-flex items-center gap-1.5 min-w-0 h-5 px-1.5 rounded-full border border-slate-200 bg-slate-50"
            title={label}
        >
            <span
                className="size-2 shrink-0 rounded-full"
                style={{ backgroundColor: team?.color || "#94a3b8" }}
            />
            {!compact && <span className="text-[11px] text-slate-600 truncate max-w-[90px]">{label}</span>}
        </span>
    );
}

function StatusTag({ status }: { status: CRMTaskStatus }) {
    const map: Record<CRMTaskStatus, { label: string; cls: string; dot: string }> = {
        pending: { label: "Pending", cls: "text-slate-600", dot: "bg-slate-400" },
        in_progress: { label: "Active", cls: "text-sky-700", dot: "bg-sky-500" },
        completed: { label: "Done", cls: "text-emerald-700", dot: "bg-emerald-500" },
        cancelled: { label: "Cancelled", cls: "text-slate-400", dot: "bg-slate-300" },
    };
    const s = map[status];
    return (
        <span className={`inline-flex items-center gap-1.5 text-[11px] font-medium ${s.cls}`}>
            <span className={`size-1.5 rounded-full ${s.dot}`} />
            {s.label}
        </span>
    );
}

function TaskTypeTag({
    type,
    types,
    done,
    compact = false,
}: {
    type: string | undefined;
    types: { name: string; color: string }[];
    done: boolean;
    compact?: boolean;
}) {
    if (!type) {
        return compact ? null : <span className="text-slate-300 text-[11.5px]">—</span>;
    }
    // Render the type as a colour-tinted chip (faint background in the type's
    // colour + a solid dot + text in the colour) so the colour is unmistakable,
    // not a single 8px dot. Done tasks fade to grey. Colours are DB-enforced hex.
    const color = done ? "#94a3b8" : taskTypeColor(type, types);
    return (
        <span
            title={type}
            className={`shrink-0 inline-flex items-center gap-1 rounded h-[18px] px-1.5 text-[10.5px] font-medium ${
                compact ? "max-w-[110px]" : ""
            }`}
            style={{ backgroundColor: `${color}1f`, color }}
        >
            <span
                className="w-1.5 h-1.5 rounded-full shrink-0"
                style={{ backgroundColor: color }}
            />
            <span className="truncate">{type}</span>
        </span>
    );
}

function DueCell({ due, overdue }: { due: string | undefined; overdue: boolean }) {
    if (!due) return <span className="text-slate-300 text-[11px]">—</span>;
    if (overdue) {
        return (
            <span className="inline-flex items-center gap-1 font-mono text-[10.5px] text-red-600 tabular-nums">
                <AlertTriangleIcon className="w-2.5 h-2.5" />
                {fmtDue(due)}
            </span>
        );
    }
    return (
        <span className="inline-flex items-center gap-1 font-mono text-[10.5px] text-slate-400 tabular-nums">
            <CalendarClockIcon className="w-2.5 h-2.5" />
            {fmtDue(due)}
        </span>
    );
}

// ── Toolbar pieces ─────────────────────────────────────────────────────────

function ViewToggle({
    view,
    onChange,
}: {
    view: "flat" | "grouped";
    onChange: (v: "flat" | "grouped") => void;
}) {
    return (
        <div className="inline-flex rounded-md border border-slate-200 bg-white p-0.5">
            <button
                type="button"
                onClick={() => onChange("grouped")}
                aria-label="Grouped by due date"
                className={`h-6 px-2 rounded text-[11px] font-medium inline-flex items-center gap-1.5 transition-colors ${
                    view === "grouped" ? "bg-white text-slate-900 shadow-sm" : "text-slate-500 hover:text-slate-900"
                }`}
            >
                <ListTreeIcon className="w-3 h-3" />
                Grouped
            </button>
            <button
                type="button"
                onClick={() => onChange("flat")}
                aria-label="Flat list"
                className={`h-6 px-2 rounded text-[11px] font-medium inline-flex items-center gap-1.5 transition-colors ${
                    view === "flat" ? "bg-white text-slate-900 shadow-sm" : "text-slate-500 hover:text-slate-900"
                }`}
            >
                <LayoutListIcon className="w-3 h-3" />
                Table
            </button>
        </div>
    );
}

function AssigneeFacet({
    members,
    selected,
    onChange,
}: {
    members: OrganizationMember[];
    selected: string[];
    onChange: (ids: string[]) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const label =
        selected.length === 0
            ? "Anyone"
            : selected.length === 1
              ? memberLabel(members.find((m) => m.user_id === selected[0]), selected[0])
              : `${selected.length} assignees`;

    function toggle(id: string) {
        onChange(selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id]);
    }

    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="end">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className={`h-7 px-2.5 rounded-md border text-[12px] inline-flex items-center gap-1.5 transition-colors ${
                        selected.length
                            ? "border-sky-300 bg-sky-50 text-sky-700"
                            : "border-slate-200 text-slate-600 hover:text-slate-900 hover:border-slate-300"
                    }`}
                >
                    <UsersIcon className="w-3 h-3" />
                    <span className="truncate max-w-[100px]">{label}</span>
                    <span className="text-slate-400">▾</span>
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={220} className="max-h-64 overflow-y-auto">
                {members.length === 0 ? (
                    <div className="px-2 py-1.5 text-[11.5px] text-slate-400">No members</div>
                ) : (
                    members.map((m) => (
                        <PopoverMenuItem
                            key={m.user_id}
                            onSelect={() => toggle(m.user_id)}
                            selected={selected.includes(m.user_id)}
                            closeOnSelect={false}
                            icon={
                                <span className="size-5 rounded-full bg-sky-50 border border-sky-200 text-sky-700 text-[9px] font-semibold inline-flex items-center justify-center uppercase">
                                    {memberInitials(m)}
                                </span>
                            }
                        >
                            <span className="truncate">{memberLabel(m)}</span>
                        </PopoverMenuItem>
                    ))
                )}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

function TeamFacet({
    teams,
    selected,
    onChange,
}: {
    teams: Team[];
    selected: string[];
    onChange: (ids: string[]) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const label =
        selected.length === 0
            ? "Any team"
            : selected.length === 1
              ? teamLabel(teams.find((t) => t.id === selected[0]), selected[0])
              : `${selected.length} teams`;

    function toggle(id: string) {
        onChange(selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id]);
    }

    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="end">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className={`h-7 px-2.5 rounded-md border text-[12px] inline-flex items-center gap-1.5 transition-colors ${
                        selected.length
                            ? "border-sky-300 bg-sky-50 text-sky-700"
                            : "border-slate-200 text-slate-600 hover:text-slate-900 hover:border-slate-300"
                    }`}
                >
                    <UsersRoundIcon className="w-3 h-3" />
                    <span className="truncate max-w-[100px]">{label}</span>
                    <span className="text-slate-400">▾</span>
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={220} className="max-h-64 overflow-y-auto">
                {teams.length === 0 ? (
                    <Link
                        to="/app/settings/teams"
                        onClick={() => setOpen(false)}
                        className="flex items-center gap-2 px-3 h-8 text-[11.5px] text-slate-500 hover:text-sky-700 hover:bg-slate-50 transition-colors"
                    >
                        <UsersRoundIcon className="w-3.5 h-3.5 shrink-0" />
                        No teams yet, create one in Settings
                    </Link>
                ) : (
                    teams.map((t) => (
                        <PopoverMenuItem
                            key={t.id}
                            onSelect={() => toggle(t.id)}
                            selected={selected.includes(t.id)}
                            closeOnSelect={false}
                            icon={
                                <span
                                    className="size-2.5 rounded-full"
                                    style={{ backgroundColor: t.color || "#94a3b8" }}
                                />
                            }
                        >
                            <span className="truncate">{t.name}</span>
                        </PopoverMenuItem>
                    ))
                )}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

function TypeFacet({
    types,
    selected,
    onChange,
}: {
    types: { name: string; color: string }[];
    selected: string[];
    onChange: (names: string[]) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const label = selected.length === 0 ? "All types" : `${selected.length} type${selected.length === 1 ? "" : "s"}`;

    function toggle(name: string) {
        onChange(selected.includes(name) ? selected.filter((x) => x !== name) : [...selected, name]);
    }

    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="end">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className={`h-7 px-2.5 rounded-md border text-[12px] inline-flex items-center gap-1.5 transition-colors ${
                        selected.length
                            ? "border-sky-300 bg-sky-50 text-sky-700"
                            : "border-slate-200 text-slate-600 hover:text-slate-900 hover:border-slate-300"
                    }`}
                >
                    <TagIcon className="w-3 h-3" />
                    {label}
                    <span className="text-slate-400">▾</span>
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={200} className="max-h-64 overflow-y-auto">
                {types.length === 0 ? (
                    <div className="px-2 py-1.5 text-[11.5px] text-slate-400">No types yet</div>
                ) : (
                    types.map((t) => (
                        <PopoverMenuItem
                            key={t.name}
                            onSelect={() => toggle(t.name)}
                            selected={selected.includes(t.name)}
                            closeOnSelect={false}
                            icon={
                                <span
                                    className="w-2.5 h-2.5 rounded-full ring-1 ring-black/5 shrink-0"
                                    style={{ backgroundColor: t.color || "#94a3b8" }}
                                />
                            }
                        >
                            <span style={{ color: t.color || "#94a3b8" }}>
                                {t.name}
                            </span>
                        </PopoverMenuItem>
                    ))
                )}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

function SortPopover({
    active,
    onChange,
}: {
    active: string;
    onChange: (s: (typeof SORTS)[number]) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const cur = SORTS.find((s) => s.id === active) ?? SORTS[0];
    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="end">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-600 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors"
                >
                    <ArrowUpDownIcon className="w-3 h-3" />
                    {cur.label}
                    <span className="text-slate-400">▾</span>
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={200}>
                {SORTS.map((s) => (
                    <PopoverMenuItem key={s.id} onSelect={() => onChange(s)} selected={s.id === active}>
                        {s.label}
                    </PopoverMenuItem>
                ))}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

function FilterPopover({
    filters,
    onChange,
    activeCount,
}: {
    filters: SearchTasks;
    onChange: React.Dispatch<React.SetStateAction<SearchTasks>>;
    activeCount: number;
}) {
    const [open, setOpen] = React.useState(false);

    function togglePriority(p: CRMTaskPriority) {
        onChange((f) => ({
            ...f,
            priorities: f.priorities.includes(p)
                ? f.priorities.filter((x) => x !== p)
                : [...f.priorities, p],
        }));
    }

    const setDate = (key: "due_after" | "due_before", v: string) =>
        onChange((f) => ({ ...f, [key]: v ? new Date(v).toISOString() : undefined }));

    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="end">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className={`h-7 px-2.5 rounded-md border text-[12px] inline-flex items-center gap-1.5 transition-colors ${
                        activeCount
                            ? "border-sky-300 bg-sky-50 text-sky-700"
                            : "border-slate-200 text-slate-600 hover:text-slate-900 hover:border-slate-300"
                    }`}
                >
                    <AlertTriangleIcon className="w-3 h-3" />
                    Filters
                    {activeCount > 0 && (
                        <span className="size-4 rounded-full bg-sky-600 text-white text-[9.5px] inline-flex items-center justify-center tabular-nums">
                            {activeCount}
                        </span>
                    )}
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={260} className="p-3">
                <div className="space-y-3">
                    <div>
                        <Label>Priority</Label>
                        <div className="flex flex-wrap gap-1.5">
                            {PRIORITIES.map((p) => {
                                const on = filters.priorities.includes(p.id);
                                return (
                                    <button
                                        key={p.id}
                                        type="button"
                                        onClick={() => togglePriority(p.id)}
                                        className={`h-7 px-2 rounded-md border text-[11.5px] inline-flex items-center gap-1.5 transition-colors ${
                                            on
                                                ? "border-sky-300 bg-sky-50 text-sky-700"
                                                : "border-slate-200 text-slate-600 hover:border-slate-300"
                                        }`}
                                    >
                                        <span className={`size-1.5 rounded-full ${p.dot}`} />
                                        {p.label}
                                    </button>
                                );
                            })}
                        </div>
                    </div>
                    <div>
                        <Label>Due date</Label>
                        <div className="flex items-center gap-1.5">
                            <TextInput
                                value={filters.due_after ? String(filters.due_after).split("T")[0] : ""}
                                onChange={(v) => setDate("due_after", v)}
                                type="date"
                                className="w-full"
                            />
                            <span className="text-slate-300">–</span>
                            <TextInput
                                value={filters.due_before ? String(filters.due_before).split("T")[0] : ""}
                                onChange={(v) => setDate("due_before", v)}
                                type="date"
                                className="w-full"
                            />
                        </div>
                    </div>
                    <label className="flex items-center gap-2 cursor-pointer select-none">
                        <button
                            type="button"
                            onClick={() => onChange((f) => ({ ...f, overdue: f.overdue ? undefined : true }))}
                            className={`size-4 rounded border inline-flex items-center justify-center transition-colors ${
                                filters.overdue
                                    ? "bg-sky-600 border-sky-600 text-white"
                                    : "border-slate-300 bg-white"
                            }`}
                        >
                            {filters.overdue && <CheckSquareIcon className="w-3 h-3" />}
                        </button>
                        <span className="text-[12px] text-slate-700">Only overdue + actionable</span>
                    </label>
                    <div className="flex items-center justify-between pt-1 border-t border-slate-100">
                        <button
                            type="button"
                            onClick={() =>
                                onChange((f) => ({
                                    ...f,
                                    priorities: [],
                                    due_after: undefined,
                                    due_before: undefined,
                                    overdue: undefined,
                                }))
                            }
                            className="text-[11.5px] text-slate-500 hover:text-slate-900 inline-flex items-center gap-1"
                        >
                            <XIcon className="w-3 h-3" />
                            Clear
                        </button>
                        <button
                            type="button"
                            onClick={() => setOpen(false)}
                            className="h-6 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[11.5px] font-medium transition-colors"
                        >
                            Done
                        </button>
                    </div>
                </div>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

function LoadMore({
    hasNextPage,
    isFetchingNextPage,
    onLoadMore,
    loaded,
    total,
}: {
    hasNextPage: boolean;
    isFetchingNextPage: boolean;
    onLoadMore: () => void;
    loaded: number;
    total: number;
}) {
    return (
        <div className="px-5 py-3 flex items-center justify-center gap-3 border-t border-slate-200/60">
            {hasNextPage ? (
                <button
                    type="button"
                    onClick={onLoadMore}
                    disabled={isFetchingNextPage}
                    className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                >
                    {isFetchingNextPage ? (
                        <>
                            <Loader2Icon className="w-3 h-3 animate-spin" />
                            Loading…
                        </>
                    ) : (
                        <>
                            <PlusIcon className="w-3 h-3" />
                            Load more
                        </>
                    )}
                </button>
            ) : null}
            <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">
                {hasNextPage ? `${loaded} of ${total} loaded` : `${total} ${total === 1 ? "task" : "tasks"}`}
            </span>
        </div>
    );
}

// ── Empty / skeleton ───────────────────────────────────────────────────────

function EmptyState({
    hasFilters,
    onCreate,
    onClear,
}: {
    hasFilters: boolean;
    onCreate: () => void;
    onClear: () => void;
}) {
    return (
        <div className="px-5 py-16 text-center">
            <div className="mx-auto size-9 rounded-md bg-slate-50 border border-slate-200 flex items-center justify-center mb-3">
                <CheckSquareIcon className="w-4 h-4 text-slate-400" />
            </div>
            <p className="text-[12.5px] text-slate-700 font-medium mb-1">
                {hasFilters ? "No tasks match these filters" : "No tasks yet"}
            </p>
            <p className="text-[11.5px] text-slate-400 mb-4 max-w-[42ch] mx-auto leading-relaxed">
                {hasFilters
                    ? "Try widening or clearing the filters to see more."
                    : "Tasks attach to a contact or a deal. Create one and it shows up here: searchable, filterable, and grouped by due-date."}
            </p>
            {hasFilters ? (
                <button
                    type="button"
                    onClick={onClear}
                    className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors"
                >
                    <XIcon className="w-3 h-3" />
                    Clear filters
                </button>
            ) : (
                <button
                    type="button"
                    onClick={onCreate}
                    className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                >
                    <PlusIcon className="w-3 h-3" />
                    Create task
                </button>
            )}
        </div>
    );
}

function GroupedSkeleton() {
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

function FlatSkeleton() {
    return (
        <table className="w-full border-collapse">
            <tbody>
                {Array.from({ length: 8 }).map((_, i) => (
                    <tr key={i} className="h-11 border-b border-slate-200/60">
                        {Array.from({ length: 6 }).map((__, j) => (
                            <td key={j} className="px-3">
                                <div
                                    className="h-3 bg-slate-100 rounded animate-pulse"
                                    style={{ width: `${50 + ((j * 13) % 40)}%` }}
                                />
                            </td>
                        ))}
                    </tr>
                ))}
            </tbody>
        </table>
    );
}

function Th({ children, className }: { children: React.ReactNode; className?: string }) {
    return (
        <th
            className={`px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] ${className ?? ""}`}
        >
            {children}
        </th>
    );
}

// ── Dialog ─────────────────────────────────────────────────────────────────

function TaskDialog({
    open,
    onClose,
    editing,
    members,
    teams,
}: {
    open: boolean;
    onClose: () => void;
    editing?: CRMTask;
    members: OrganizationMember[];
    teams: Team[];
}) {
    const create = useCreateCRMTask();
    const update = useUpdateCRMTask();
    const del = useDeleteCRMTask();
    const confirm = useConfirm();

    const [title, setTitle] = React.useState("");
    const [description, setDescription] = React.useState("");
    const [dueDays, setDueDays] = React.useState<number | null>(null);
    const [priority, setPriority] = React.useState<CRMTaskPriority>("medium");
    const [type, setType] = React.useState<string>("");
    const [status, setStatus] = React.useState<CRMTaskStatus>("pending");
    const [assignedTo, setAssignedTo] = React.useState<string>("");
    const [assignedTeamId, setAssignedTeamId] = React.useState<string>("");

    React.useEffect(() => {
        if (!open) return;
        if (editing) {
            setTitle(editing.title);
            setDescription(editing.description ?? "");
            setDueDays(isoToDueInDays(editing.due_date));
            setPriority(editing.priority);
            setType(editing.type ?? "");
            setStatus(editing.status);
            setAssignedTo(editing.assigned_to ?? "");
            setAssignedTeamId(editing.assigned_team_id ?? "");
        } else {
            setTitle("");
            setDescription("");
            setDueDays(null);
            setPriority("medium");
            setType("");
            setStatus("pending");
            setAssignedTo("");
            setAssignedTeamId("");
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
            type,
        };
        if (description.trim()) data.description = description.trim();
        if (dueDays !== null) data.due_date = dueInDaysToISO(dueDays);
        if (assignedTo) data.assigned_to = assignedTo;
        if (assignedTeamId) data.assigned_team_id = assignedTeamId;
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
                        className="w-full max-w-[480px] max-h-[calc(100dvh-2rem)] flex flex-col rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18)] overflow-hidden"
                    >
                        <div className="h-12 shrink-0 px-4 border-b border-slate-200 flex items-center gap-2.5">
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

                        <div className="px-4 py-4 space-y-3 overflow-y-auto min-h-0 flex-1">
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
                            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                                <div>
                                    <Label>Task type</Label>
                                    <TaskTypePicker value={type} onChange={setType} />
                                </div>
                                <div>
                                    <Label>Assignee</Label>
                                    <AssigneePicker
                                        value={assignedTo}
                                        members={members}
                                        onChange={setAssignedTo}
                                        teams={teams}
                                        teamValue={assignedTeamId}
                                        onTeamChange={setAssignedTeamId}
                                    />
                                </div>
                            </div>
                            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                                <div>
                                    <Label>Due date</Label>
                                    <DueInDays value={dueDays} onChange={setDueDays} />
                                </div>
                                <div>
                                    <Label>Priority</Label>
                                    <PriorityPill value={priority} onChange={setPriority} />
                                </div>
                            </div>
                            {editing && (
                                <div>
                                    <Label>Status</Label>
                                    <div className="inline-flex rounded-md bg-slate-100 p-0.5 w-full gap-0.5">
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
                                                        ? "bg-white text-slate-900 shadow-sm"
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

                        <div className="px-3 h-12 shrink-0 border-t border-slate-200 flex items-center gap-1.5">
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

function AssigneePicker({
    value,
    members,
    onChange,
    teams,
    teamValue,
    onTeamChange,
}: {
    value: string;
    members: OrganizationMember[];
    onChange: (id: string) => void;
    teams: Team[];
    teamValue: string;
    onTeamChange: (id: string) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const cur = members.find((m) => m.user_id === value);
    const curTeam = teams.find((t) => t.id === teamValue);

    // Person and team are independent: a task can set one, both, or neither.
    // The trigger summarizes whichever are selected.
    const triggerLabel = cur
        ? curTeam
            ? `${memberLabel(cur)} + ${curTeam.name}`
            : memberLabel(cur)
        : curTeam
          ? curTeam.name
          : "Unassigned";

    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="start">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className="h-7 w-full px-2.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors"
                >
                    {cur ? (
                        <span className="size-4 shrink-0 rounded-full bg-sky-50 border border-sky-200 text-sky-700 text-[8px] font-semibold inline-flex items-center justify-center uppercase">
                            {memberInitials(cur)}
                        </span>
                    ) : curTeam ? (
                        <span
                            className="size-2 rounded-full shrink-0"
                            style={{ backgroundColor: curTeam.color || "#94a3b8" }}
                        />
                    ) : (
                        <span className="size-2 rounded-full bg-slate-300 shrink-0" />
                    )}
                    <span className="truncate flex-1 text-left">{triggerLabel}</span>
                    <span className="ml-auto text-slate-400">▾</span>
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={240} className="max-h-72 overflow-y-auto">
                <div className="px-3 pt-1.5 pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                    Person
                </div>
                <PopoverMenuItem
                    onSelect={() => onChange("")}
                    selected={value === ""}
                    closeOnSelect={false}
                    icon={<span className="size-2 rounded-full bg-slate-300 block" />}
                >
                    No person
                </PopoverMenuItem>
                {members.map((m) => (
                    <PopoverMenuItem
                        key={m.user_id}
                        onSelect={() => onChange(m.user_id)}
                        selected={m.user_id === value}
                        closeOnSelect={false}
                        icon={
                            <span className="size-5 rounded-full bg-sky-50 border border-sky-200 text-sky-700 text-[9px] font-semibold inline-flex items-center justify-center uppercase">
                                {memberInitials(m)}
                            </span>
                        }
                    >
                        <span className="truncate">{memberLabel(m)}</span>
                    </PopoverMenuItem>
                ))}
                <div className="my-1 h-px bg-slate-200" />
                <div className="px-3 pt-0.5 pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                    Team
                </div>
                {teams.length === 0 ? (
                    <Link
                        to="/app/settings/teams"
                        onClick={() => setOpen(false)}
                        className="flex items-center gap-2 px-3 h-8 text-[12px] text-slate-500 hover:text-sky-700 hover:bg-slate-50 transition-colors"
                    >
                        <UsersRoundIcon className="w-3.5 h-3.5 shrink-0" />
                        No teams yet, create one in Settings
                    </Link>
                ) : (
                    <>
                        <PopoverMenuItem
                            onSelect={() => onTeamChange("")}
                            selected={teamValue === ""}
                            closeOnSelect={false}
                            icon={<span className="size-2 rounded-full bg-slate-300 block" />}
                        >
                            No team
                        </PopoverMenuItem>
                        {teams.map((t) => (
                            <PopoverMenuItem
                                key={t.id}
                                onSelect={() => onTeamChange(t.id)}
                                selected={t.id === teamValue}
                                closeOnSelect={false}
                                icon={
                                    <span
                                        className="size-2.5 rounded-full"
                                        style={{ backgroundColor: t.color || "#94a3b8" }}
                                    />
                                }
                            >
                                <span className="truncate">{t.name}</span>
                            </PopoverMenuItem>
                        ))}
                    </>
                )}
            </PopoverMenuContent>
        </PopoverMenu>
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
    const cur = PRIORITIES.find((p) => p.id === value)!;

    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="start">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className="h-7 w-full px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1.5"
                >
                    <span className={`size-1.5 rounded-full ${cur.dot}`} />
                    <span className="truncate">{cur.label}</span>
                    <span className="ml-auto text-slate-400">▾</span>
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent>
                {PRIORITIES.map((p) => (
                    <PopoverMenuItem
                        key={p.id}
                        onSelect={() => onChange(p.id)}
                        selected={p.id === value}
                        icon={<span className={`size-1.5 rounded-full ${p.dot}`} />}
                    >
                        {p.label}
                    </PopoverMenuItem>
                ))}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

// ── Helpers ────────────────────────────────────────────────────────────────

function hasAnyFilter(f: SearchTasks): boolean {
    return (
        f.query.trim() !== "" ||
        f.statuses.length > 0 ||
        f.priorities.length > 0 ||
        f.types.length > 0 ||
        f.assigned_to.length > 0 ||
        f.team_ids.length > 0 ||
        !!f.contact_id ||
        !!f.deal_id ||
        !!f.due_after ||
        !!f.due_before ||
        !!f.overdue
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
