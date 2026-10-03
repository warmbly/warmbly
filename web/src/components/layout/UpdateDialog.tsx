// The instance update dialog, for platform admins on a self-hosted install.
//
// Four panes with directional slides, like the campaign wizard: overview
// (what runs, what is newest), confirm (what the update does), progress (the
// updater's steps and log, then the backend restart), and the result. The pill
// in the header keeps following the job when the dialog is closed, and a
// reload picks the job back up from the backend.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
    AlertTriangleIcon,
    ArrowRightIcon,
    ArrowUpCircleIcon,
    CheckIcon,
    ChevronDownIcon,
    ChevronLeftIcon,
    DatabaseIcon,
    ExternalLinkIcon,
    GitBranchIcon,
    HammerIcon,
    Loader2Icon,
    RefreshCwIcon,
    RotateCwIcon,
    SendIcon,
    ServerIcon,
    ShieldCheckIcon,
    XIcon,
} from "lucide-react";
import { applyInstanceUpdate, checkInstanceUpdate } from "@/lib/api/client/admin/updates";
import {
    INSTANCE_UPDATE_KEY,
    INSTANCE_UPDATE_LOG_KEY,
    isUpdateRunning,
    runningLabel,
    useInstanceUpdate,
    useInstanceUpdateLog,
} from "@/lib/api/hooks/auth/useInstanceUpdate";
import type InstanceUpdate from "@/lib/api/models/auth/InstanceUpdate";
import type { UpdateJob } from "@/lib/api/models/auth/InstanceUpdate";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { markUpdateStarted, readUpdateStarted, clearUpdateStarted } from "@/lib/updateSession";
import { cn } from "@/lib/utils";

const DOCS_UPDATES = "https://docs.warmbly.com/development/updates/";

interface Props {
    open: boolean;
    onClose: () => void;
}

type Pane = "overview" | "confirm" | "progress" | "done" | "failed";

interface StepDef {
    id: string;
    label: string;
    hint: string;
    icon: React.ComponentType<{ className?: string }>;
}

const COMPOSE_STEPS: StepDef[] = [
    { id: "fetch", label: "Fetch", hint: "Reading the remote repository", icon: GitBranchIcon },
    { id: "checkout", label: "Pull", hint: "Moving the checkout to the new version", icon: ArrowUpCircleIcon },
    { id: "build", label: "Build", hint: "Rebuilding every image, usually the longest step", icon: HammerIcon },
    { id: "restart", label: "Restart", hint: "Recreating the services that changed", icon: RotateCwIcon },
    { id: "prune", label: "Clean up", hint: "Removing images nothing uses any more", icon: DatabaseIcon },
    { id: "wait", label: "Reconnect", hint: "Waiting for the backend to answer again", icon: ServerIcon },
];

const COMMAND_STEPS: StepDef[] = [
    { id: "fetch", label: "Fetch", hint: "Reading the remote repository", icon: GitBranchIcon },
    { id: "checkout", label: "Pull", hint: "Moving the checkout to the new version", icon: ArrowUpCircleIcon },
    { id: "command", label: "Build and restart", hint: "Running the upgrade script", icon: HammerIcon },
    { id: "wait", label: "Reconnect", hint: "Waiting for the backend to answer again", icon: ServerIcon },
];

export default function UpdateDialog({ open, onClose }: Props) {
    const qc = useQueryClient();
    const stateQ = useInstanceUpdate(open);
    const logQ = useInstanceUpdateLog(open);
    const state: InstanceUpdate | undefined = logQ.data ?? stateQ.data;
    const started = readUpdateStarted();

    const [pane, setPane] = React.useState<Pane>("overview");
    const [direction, setDirection] = React.useState<1 | -1>(1);
    const goTo = React.useCallback(
        (next: Pane) => {
            const order: Pane[] = ["overview", "confirm", "progress", "done", "failed"];
            setDirection(order.indexOf(next) >= order.indexOf(pane) ? 1 : -1);
            setPane(next);
        },
        [pane],
    );

    // The backend decides the pane while a job exists; the local state only
    // drives overview <-> confirm.
    const running = isUpdateRunning(state);
    const backendDown = !!started && (logQ.isError || stateQ.isError);
    const lastJob = state?.updater?.last_job;
    const finished = !!started && !running && !backendDown && lastJob && lastJob.status !== "running";

    React.useEffect(() => {
        if (!open) return;
        if (running || backendDown) {
            if (pane !== "progress") goTo("progress");
        } else if (finished) {
            const next: Pane = lastJob?.status === "succeeded" ? "done" : "failed";
            if (pane !== next) goTo(next);
        }
    }, [open, running, backendDown, finished, lastJob?.status, pane, goTo]);

    React.useEffect(() => {
        if (!open) setPane("overview");
    }, [open]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            e.preventDefault();
            onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, onClose]);

    const check = useMutation({
        mutationFn: checkInstanceUpdate,
        onSuccess: (data) => {
            qc.setQueryData(INSTANCE_UPDATE_KEY, data);
            qc.setQueryData(INSTANCE_UPDATE_LOG_KEY, data);
            toast.success(data.update_available ? "A newer version is available" : "This instance is up to date");
        },
        onError: (err: unknown) => toast.error(buildError(err as AppError)),
    });

    const apply = useMutation({
        mutationFn: () => applyInstanceUpdate("latest"),
        onSuccess: (job: UpdateJob) => {
            markUpdateStarted(state?.running.version ?? "", state?.running.commit ?? job.from_commit);
            goTo("progress");
            void qc.invalidateQueries({ queryKey: INSTANCE_UPDATE_KEY });
            void qc.invalidateQueries({ queryKey: INSTANCE_UPDATE_LOG_KEY });
        },
        onError: (err: unknown) => toast.error(buildError(err as AppError)),
    });

    const job = state?.updater?.job ?? state?.updater?.last_job;
    const steps = state?.updater?.mode === "command" ? COMMAND_STEPS : COMPOSE_STEPS;

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onMouseDown={onClose}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        role="dialog"
                        aria-modal="true"
                        aria-label="Update Warmbly"
                        initial={{ y: 8, opacity: 0, scale: 0.985 }}
                        animate={{ y: 0, opacity: 1, scale: 1 }}
                        exit={{ y: 8, opacity: 0, scale: 0.985 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[560px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[88dvh]"
                    >
                        <Header state={state} pane={pane} onClose={onClose} />

                        <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden">
                            <AnimatePresence mode="wait" initial={false} custom={direction}>
                                <motion.div
                                    key={pane}
                                    custom={direction}
                                    variants={paneVariants}
                                    initial="enter"
                                    animate="center"
                                    exit="exit"
                                    transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                                    className="px-5 py-5"
                                >
                                    {pane === "overview" && (
                                        <OverviewPane state={state} loading={stateQ.isLoading && !state} />
                                    )}
                                    {pane === "confirm" && <ConfirmPane state={state} />}
                                    {pane === "progress" && (
                                        <ProgressPane
                                            steps={steps}
                                            job={job}
                                            backendDown={backendDown}
                                        />
                                    )}
                                    {pane === "done" && (
                                        <DonePane
                                            state={state}
                                            job={job}
                                            onAcknowledge={() => clearUpdateStarted()}
                                        />
                                    )}
                                    {pane === "failed" && <FailedPane job={job} />}
                                </motion.div>
                            </AnimatePresence>
                        </div>

                        <Footer
                            pane={pane}
                            state={state}
                            checking={check.isPending}
                            applying={apply.isPending}
                            onCheck={() => check.mutate()}
                            onContinue={() => goTo("confirm")}
                            onBack={() => goTo("overview")}
                            onApply={() => apply.mutate()}
                            onRetry={() => {
                                clearUpdateStarted();
                                goTo("confirm");
                            }}
                            onClose={onClose}
                        />
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

const paneVariants = {
    enter: (dir: 1 | -1) => ({ x: dir * 28, opacity: 0 }),
    center: { x: 0, opacity: 1 },
    exit: (dir: 1 | -1) => ({ x: dir * -28, opacity: 0 }),
};

// header

function Header({ state, pane, onClose }: { state?: InstanceUpdate; pane: Pane; onClose: () => void }) {
    const latest = state?.latest?.tag;
    const subtitle =
        pane === "progress"
            ? "Updating this instance"
            : pane === "done"
              ? "Update finished"
              : pane === "failed"
                ? "Update failed"
                : state
                  ? `Running ${runningLabel(state)}${latest ? ` · latest ${latest}` : ""}`
                  : "Reading the running version";
    return (
        <div className="flex items-center gap-3 px-5 h-14 border-b border-slate-200 shrink-0">
            <span className="size-8 rounded-md bg-sky-50 text-sky-700 flex items-center justify-center shrink-0">
                <ArrowUpCircleIcon className="w-4 h-4" />
            </span>
            <div className="min-w-0 flex-1">
                <div className="text-[13.5px] font-semibold text-slate-900 leading-tight">Update Warmbly</div>
                <div className="text-[12px] text-slate-500 truncate">{subtitle}</div>
            </div>
            <button
                type="button"
                onClick={onClose}
                aria-label="Close"
                className="size-7 rounded-md flex items-center justify-center text-slate-400 hover:text-slate-700 hover:bg-slate-100 transition-colors"
            >
                <XIcon className="w-4 h-4" />
            </button>
        </div>
    );
}

// panes

function OverviewPane({ state, loading }: { state?: InstanceUpdate; loading: boolean }) {
    if (loading || !state) {
        return (
            <div className="space-y-3">
                <div className="h-20 rounded-md bg-slate-100 animate-pulse" />
                <div className="h-4 w-2/3 rounded bg-slate-100 animate-pulse" />
                <div className="h-4 w-1/2 rounded bg-slate-100 animate-pulse" />
            </div>
        );
    }
    const { latest, updater } = state;
    const checkout = updater?.checkout;
    const available = state.update_available;

    return (
        <div className="space-y-4">
            <div
                className={cn(
                    "rounded-md border p-4 flex items-center gap-4",
                    available ? "border-amber-200 bg-amber-50/60" : "border-emerald-200 bg-emerald-50/50",
                )}
            >
                <VersionBox label="Running" value={runningLabel(state)} muted={available} />
                <ArrowRightIcon className={cn("w-4 h-4 shrink-0", available ? "text-amber-500" : "text-emerald-500")} />
                <VersionBox
                    label={available ? "Available" : "Latest"}
                    value={latest?.tag ?? (checkout && !checkout.detached ? `${checkout.branch} head` : "unknown")}
                    accent={available}
                />
                <div className="ml-auto text-right">
                    <span
                        className={cn(
                            "inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[10.5px] font-semibold uppercase tracking-[0.1em]",
                            available
                                ? "border-amber-300 bg-white text-amber-700"
                                : "border-emerald-300 bg-white text-emerald-700",
                        )}
                    >
                        {available ? (
                            <>
                                <span className="size-1.5 rounded-full bg-amber-500 animate-pulse" />
                                Update
                            </>
                        ) : (
                            <>
                                <CheckIcon className="w-3 h-3" />
                                Current
                            </>
                        )}
                    </span>
                </div>
            </div>

            <dl className="text-[12.5px] divide-y divide-slate-100 border-y border-slate-100">
                <Row label="Release">
                    {latest ? (
                        <span className="inline-flex flex-wrap items-center gap-2">
                            <span className="font-medium text-slate-900">{latest.name || latest.tag}</span>
                            {latest.published_at && (
                                <span className="text-slate-500">published {relative(latest.published_at)}</span>
                            )}
                            {latest.html_url && (
                                <a
                                    href={latest.html_url}
                                    target="_blank"
                                    rel="noreferrer"
                                    className="inline-flex items-center gap-1 text-sky-700 hover:underline"
                                >
                                    Release notes
                                    <ExternalLinkIcon className="w-3 h-3" />
                                </a>
                            )}
                        </span>
                    ) : state.check_error ? (
                        <span className="text-amber-700">Could not read releases: {state.check_error}</span>
                    ) : (
                        <span className="text-slate-500">
                            {state.enabled ? `No release found for ${state.repo}` : "Release check is off"}
                        </span>
                    )}
                </Row>
                {checkout && (
                    <Row label="Checkout">
                        <span className="inline-flex flex-wrap items-center gap-2">
                            <span className="inline-flex items-center gap-1 font-mono text-[11.5px] text-slate-700">
                                <GitBranchIcon className="w-3 h-3 text-slate-400" />
                                {checkout.detached ? "pinned" : checkout.branch}@{checkout.commit.slice(0, 7)}
                            </span>
                            {!checkout.detached && (
                                <span className="text-slate-500">
                                    {checkout.behind > 0
                                        ? `${checkout.behind} commit${checkout.behind === 1 ? "" : "s"} behind`
                                        : "matches the remote"}
                                </span>
                            )}
                            {checkout.dirty && <span className="text-amber-700">local changes present</span>}
                        </span>
                    </Row>
                )}
                <Row label="Updater">
                    {updater?.status === "ok" && (
                        <span className="inline-flex items-center gap-1.5 text-slate-700">
                            <span className="size-1.5 rounded-full bg-emerald-500" />
                            ready
                            <span className="text-slate-400">({updater.mode} mode)</span>
                        </span>
                    )}
                    {updater?.status === "off" && <span className="text-slate-500">not configured</span>}
                    {updater?.status === "unreachable" && (
                        <span className="inline-flex items-center gap-1.5 text-amber-700">
                            <span className="size-1.5 rounded-full bg-amber-500" />
                            unreachable
                        </span>
                    )}
                </Row>
                <Row label="Checked">
                    <span className="text-slate-500">
                        {state.checked_at ? `${relative(state.checked_at)}, every ${state.interval}` : "not yet"}
                    </span>
                </Row>
            </dl>

            {updater?.status !== "ok" && (
                <Notice tone={updater?.status === "unreachable" ? "warning" : "info"}>
                    <div className="font-medium text-slate-900">
                        {updater?.status === "unreachable" ? "The updater is not answering" : "Updates run from a shell here"}
                    </div>
                    <div className="mt-0.5">
                        {updater?.status === "unreachable"
                            ? updater?.error
                            : "No updater is configured on this instance, so apply updates on the host:"}
                    </div>
                    <code className="mt-1.5 block rounded bg-white/80 border border-slate-200 px-2 py-1 font-mono text-[11.5px] text-slate-800">
                        git pull && make up
                    </code>
                    <a
                        href={DOCS_UPDATES}
                        target="_blank"
                        rel="noreferrer"
                        className="mt-1.5 inline-flex items-center gap-1 text-sky-700 hover:underline"
                    >
                        How to enable the updater
                        <ExternalLinkIcon className="w-3 h-3" />
                    </a>
                </Notice>
            )}
            {checkout?.dirty && updater?.status === "ok" && (
                <Notice tone="warning">
                    The checkout has local modifications. The updater refuses to move it until they are
                    committed or stashed.
                </Notice>
            )}
        </div>
    );
}

function ConfirmPane({ state }: { state?: InstanceUpdate }) {
    const target = state?.latest?.tag;
    const items = [
        {
            icon: GitBranchIcon,
            title: target ? `Pull ${target} from the repository` : "Pull the latest changes from the repository",
            text: "The checkout moves forward; nothing is overwritten that was committed.",
        },
        {
            icon: HammerIcon,
            title: "Rebuild and restart every service",
            text: "Takes a few minutes. Postgres, Redis and NATS stay up; only what changed is recreated.",
        },
        {
            icon: SendIcon,
            title: "Sending and syncing pause, then resume",
            text: "Nothing in flight is lost: campaigns and warmup pick up from the database when the workers are back.",
        },
        {
            icon: DatabaseIcon,
            title: "Migrations apply when the backend comes back",
            text: "They are forward-only and safe with data in place. A backup before an update is still the one you are glad to have.",
        },
        {
            icon: ShieldCheckIcon,
            title: "You stay signed in",
            text: "This tab reconnects on its own and reloads once the new version answers.",
        },
    ];
    return (
        <div className="space-y-4">
            <div className="text-[13px] text-slate-700">Here is what happens when you continue.</div>
            <ul className="space-y-2.5">
                {items.map((it, i) => (
                    <motion.li
                        key={it.title}
                        initial={{ opacity: 0, y: 6 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ delay: 0.04 * i, duration: 0.2 }}
                        className="flex items-start gap-3"
                    >
                        <span className="mt-0.5 size-7 rounded-md bg-slate-50 border border-slate-200 text-slate-600 flex items-center justify-center shrink-0">
                            <it.icon className="w-3.5 h-3.5" />
                        </span>
                        <div className="min-w-0">
                            <div className="text-[12.5px] font-medium text-slate-900">{it.title}</div>
                            <div className="text-[12px] text-slate-500 leading-relaxed">{it.text}</div>
                        </div>
                    </motion.li>
                ))}
            </ul>
        </div>
    );
}

function ProgressPane({
    steps,
    job,
    backendDown,
}: {
    steps: StepDef[];
    job?: UpdateJob;
    backendDown: boolean;
}) {
    const current = backendDown ? "wait" : (job?.step ?? "starting");
    const idx = Math.max(0, steps.findIndex((s) => s.id === current));
    const percent = backendDown
        ? Math.round(((steps.length - 0.5) / steps.length) * 100)
        : Math.round(((idx + 0.5) / steps.length) * 100);
    const [showLog, setShowLog] = React.useState(false);
    const lines = job?.log ?? [];

    return (
        <div className="space-y-4">
            <div>
                <div className="flex items-center justify-between text-[12px] mb-1.5">
                    <span className="font-medium text-slate-900 inline-flex items-center gap-2">
                        <Loader2Icon className="w-3.5 h-3.5 animate-spin text-sky-600" />
                        {backendDown ? "Reconnecting to the backend" : (steps[idx]?.label ?? "Starting")}
                    </span>
                    <span className="text-slate-500 tabular-nums">{percent}%</span>
                </div>
                <div className="h-1.5 rounded-full bg-slate-100 overflow-hidden">
                    <motion.div
                        className="h-full rounded-full bg-sky-500"
                        initial={{ width: 0 }}
                        animate={{ width: `${percent}%` }}
                        transition={{ duration: 0.5, ease: [0.22, 1, 0.36, 1] }}
                    />
                </div>
                <div className="mt-1.5 text-[12px] text-slate-500">
                    {backendDown
                        ? "The services are restarting. This can take a minute; keep the tab open or come back later, the result is kept."
                        : "You can close this and keep working. The pill in the header follows the update."}
                </div>
            </div>

            <ol className="space-y-1">
                {steps.map((s, i) => {
                    const done = i < idx || (backendDown && s.id !== "wait");
                    const active = i === idx && !done;
                    return (
                        <li
                            key={s.id}
                            className={cn(
                                "flex items-center gap-3 rounded-md px-2 py-1.5 transition-colors",
                                active && "bg-sky-50/70",
                            )}
                        >
                            <span
                                className={cn(
                                    "size-6 rounded-full flex items-center justify-center shrink-0 border transition-colors",
                                    done && "bg-emerald-500 border-emerald-500 text-white",
                                    active && "bg-white border-sky-400 text-sky-600",
                                    !done && !active && "bg-white border-slate-200 text-slate-300",
                                )}
                            >
                                <AnimatePresence mode="wait" initial={false}>
                                    {done ? (
                                        <motion.span
                                            key="done"
                                            initial={{ scale: 0.4, opacity: 0 }}
                                            animate={{ scale: 1, opacity: 1 }}
                                            exit={{ scale: 0.4, opacity: 0 }}
                                            transition={{ type: "spring", stiffness: 500, damping: 30 }}
                                        >
                                            <CheckIcon className="w-3.5 h-3.5" />
                                        </motion.span>
                                    ) : active ? (
                                        <motion.span key="active" initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
                                            <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                                        </motion.span>
                                    ) : (
                                        <motion.span key="idle" initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
                                            <s.icon className="w-3 h-3" />
                                        </motion.span>
                                    )}
                                </AnimatePresence>
                            </span>
                            <div className="min-w-0 flex-1">
                                <div
                                    className={cn(
                                        "text-[12.5px] leading-tight",
                                        done ? "text-slate-500" : active ? "text-slate-900 font-medium" : "text-slate-400",
                                    )}
                                >
                                    {s.label}
                                </div>
                                {active && (
                                    <motion.div
                                        initial={{ opacity: 0, height: 0 }}
                                        animate={{ opacity: 1, height: "auto" }}
                                        className="text-[11.5px] text-slate-500"
                                    >
                                        {backendDown && s.id === "wait" ? "Waiting for the new backend to answer" : s.hint}
                                    </motion.div>
                                )}
                            </div>
                        </li>
                    );
                })}
            </ol>

            <LogToggle open={showLog} onToggle={() => setShowLog((v) => !v)} count={lines.length} />
            <AnimatePresence initial={false}>
                {showLog && (
                    <motion.div
                        key="log"
                        initial={{ opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={{ opacity: 0, height: 0 }}
                        transition={{ duration: 0.18 }}
                        className="overflow-hidden"
                    >
                        <LogPanel lines={lines} />
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

function DonePane({
    state,
    job,
    onAcknowledge,
}: {
    state?: InstanceUpdate;
    job?: UpdateJob;
    onAcknowledge: () => void;
}) {
    const [seconds, setSeconds] = React.useState(8);
    const [showLog, setShowLog] = React.useState(false);
    React.useEffect(() => {
        if (seconds <= 0) {
            onAcknowledge();
            window.location.reload();
            return;
        }
        const t = setTimeout(() => setSeconds((s) => s - 1), 1000);
        return () => clearTimeout(t);
    }, [seconds, onAcknowledge]);

    return (
        <div className="space-y-4">
            <div className="flex flex-col items-center text-center py-2">
                <motion.span
                    initial={{ scale: 0.5, opacity: 0 }}
                    animate={{ scale: 1, opacity: 1 }}
                    transition={{ type: "spring", stiffness: 380, damping: 22 }}
                    className="size-14 rounded-full bg-emerald-50 border border-emerald-200 text-emerald-600 flex items-center justify-center"
                >
                    <motion.span initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ delay: 0.15 }}>
                        <CheckIcon className="w-7 h-7" strokeWidth={2.5} />
                    </motion.span>
                </motion.span>
                <div className="mt-3 text-[15px] font-semibold text-slate-900">
                    Updated to {state ? runningLabel(state) : "the new version"}
                </div>
                <div className="mt-1 text-[12.5px] text-slate-500 max-w-[36ch]">
                    Every service is back and sending has resumed. Reloading in {seconds}s to pick up the
                    new dashboard.
                </div>
                {job?.from_commit && job?.to_commit && (
                    <div className="mt-2 font-mono text-[11px] text-slate-400">
                        {job.from_commit.slice(0, 7)} <span className="text-slate-300">→</span> {job.to_commit.slice(0, 7)}
                    </div>
                )}
            </div>
            <LogToggle open={showLog} onToggle={() => setShowLog((v) => !v)} count={job?.log?.length ?? 0} />
            {showLog && <LogPanel lines={job?.log ?? []} />}
        </div>
    );
}

function FailedPane({ job }: { job?: UpdateJob }) {
    return (
        <div className="space-y-4">
            <div className="flex items-start gap-3 rounded-md border border-red-200 bg-red-50/60 p-3">
                <motion.span
                    initial={{ scale: 0.6, opacity: 0 }}
                    animate={{ scale: 1, opacity: 1 }}
                    transition={{ type: "spring", stiffness: 380, damping: 22 }}
                    className="size-8 rounded-full bg-white border border-red-200 text-red-600 flex items-center justify-center shrink-0"
                >
                    <XIcon className="w-4 h-4" />
                </motion.span>
                <div className="min-w-0 text-[12.5px] leading-relaxed text-red-900">
                    <div className="font-semibold">The update did not finish</div>
                    <div className="mt-0.5">{job?.error ?? "See the log below."}</div>
                    <div className="mt-1 text-red-800/80">
                        {job?.step && job.step !== "restart" && job.step !== "wait"
                            ? "It stopped before anything restarted, so the previous version is still running."
                            : "Check the services on the host before trying again."}
                    </div>
                </div>
            </div>
            <LogPanel lines={job?.log ?? []} />
        </div>
    );
}

// footer

function Footer({
    pane,
    state,
    checking,
    applying,
    onCheck,
    onContinue,
    onBack,
    onApply,
    onRetry,
    onClose,
}: {
    pane: Pane;
    state?: InstanceUpdate;
    checking: boolean;
    applying: boolean;
    onCheck: () => void;
    onContinue: () => void;
    onBack: () => void;
    onApply: () => void;
    onRetry: () => void;
    onClose: () => void;
}) {
    const canApply = !!state && state.updater?.status === "ok" && state.update_available && !state.updater?.checkout?.dirty;
    return (
        <div className="flex items-center gap-2 px-5 h-14 border-t border-slate-200 bg-slate-50/60 shrink-0">
            <a
                href={DOCS_UPDATES}
                target="_blank"
                rel="noreferrer"
                className="text-[12px] text-slate-500 hover:text-slate-900 inline-flex items-center gap-1"
            >
                How updates work
                <ExternalLinkIcon className="w-3 h-3" />
            </a>
            <div className="flex-1" />
            {pane === "overview" && (
                <>
                    <GhostButton onClick={onCheck} disabled={checking}>
                        <RefreshCwIcon className={cn("w-3.5 h-3.5", checking && "animate-spin")} />
                        {checking ? "Checking" : "Check now"}
                    </GhostButton>
                    {canApply ? (
                        <PrimaryButton onClick={onContinue}>
                            Update and restart
                            <ArrowRightIcon className="w-3.5 h-3.5" />
                        </PrimaryButton>
                    ) : (
                        <PrimaryButton onClick={onClose}>Done</PrimaryButton>
                    )}
                </>
            )}
            {pane === "confirm" && (
                <>
                    <GhostButton onClick={onBack} disabled={applying}>
                        <ChevronLeftIcon className="w-3.5 h-3.5" />
                        Back
                    </GhostButton>
                    <PrimaryButton onClick={onApply} disabled={applying} tone="amber">
                        {applying ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <RotateCwIcon className="w-3.5 h-3.5" />}
                        {applying ? "Starting" : "Update now"}
                    </PrimaryButton>
                </>
            )}
            {pane === "progress" && <GhostButton onClick={onClose}>Keep running in the background</GhostButton>}
            {pane === "done" && (
                <>
                    <GhostButton onClick={onClose}>Later</GhostButton>
                    <PrimaryButton onClick={() => window.location.reload()}>Reload now</PrimaryButton>
                </>
            )}
            {pane === "failed" && (
                <>
                    <GhostButton onClick={onClose}>Close</GhostButton>
                    <PrimaryButton onClick={onRetry}>
                        <RotateCwIcon className="w-3.5 h-3.5" />
                        Try again
                    </PrimaryButton>
                </>
            )}
        </div>
    );
}

// bits

function VersionBox({ label, value, accent, muted }: { label: string; value: string; accent?: boolean; muted?: boolean }) {
    return (
        <div className="min-w-0">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-500">{label}</div>
            <div
                className={cn(
                    "text-[15px] font-semibold tracking-tight truncate",
                    accent ? "text-amber-800" : muted ? "text-slate-500" : "text-slate-900",
                )}
            >
                {value}
            </div>
        </div>
    );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div className="grid grid-cols-[6rem_1fr] gap-3 py-2">
            <dt className="text-slate-500">{label}</dt>
            <dd className="min-w-0 text-slate-800">{children}</dd>
        </div>
    );
}

function Notice({ tone, children }: { tone: "info" | "warning"; children: React.ReactNode }) {
    return (
        <div
            className={cn(
                "flex items-start gap-3 rounded-md border p-3 text-[12.5px] leading-relaxed",
                tone === "warning" ? "border-amber-200 bg-amber-50/60 text-amber-900" : "border-sky-200 bg-sky-50/60 text-sky-900",
            )}
        >
            <AlertTriangleIcon className="mt-0.5 w-4 h-4 shrink-0" />
            <div className="min-w-0 flex-1">{children}</div>
        </div>
    );
}

function LogToggle({ open, onToggle, count }: { open: boolean; onToggle: () => void; count: number }) {
    return (
        <button
            type="button"
            onClick={onToggle}
            className="inline-flex items-center gap-1 text-[12px] text-slate-500 hover:text-slate-900 transition-colors"
        >
            <ChevronDownIcon className={cn("w-3.5 h-3.5 transition-transform", open && "rotate-180")} />
            {open ? "Hide log" : "Show log"}
            {count > 0 && <span className="text-slate-400 tabular-nums">({count})</span>}
        </button>
    );
}

function LogPanel({ lines }: { lines: string[] }) {
    const ref = React.useRef<HTMLPreElement>(null);
    React.useEffect(() => {
        const el = ref.current;
        if (el) el.scrollTop = el.scrollHeight;
    }, [lines.length]);
    return (
        <pre
            ref={ref}
            className="max-h-56 overflow-auto rounded-md border border-slate-200 bg-slate-950 p-3 text-[11px] leading-relaxed text-slate-200"
        >
            {lines.length > 0 ? lines.join("\n") : "Waiting for output"}
        </pre>
    );
}

function GhostButton({ children, onClick, disabled }: { children: React.ReactNode; onClick: () => void; disabled?: boolean }) {
    return (
        <button
            type="button"
            onClick={onClick}
            disabled={disabled}
            className="inline-flex items-center gap-1.5 h-8 px-3 rounded-md text-[12.5px] font-medium text-slate-600 hover:text-slate-900 hover:bg-slate-200/60 transition-colors disabled:opacity-50 disabled:pointer-events-none"
        >
            {children}
        </button>
    );
}

function PrimaryButton({
    children,
    onClick,
    disabled,
    tone = "dark",
}: {
    children: React.ReactNode;
    onClick: () => void;
    disabled?: boolean;
    tone?: "dark" | "amber";
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            disabled={disabled}
            className={cn(
                "inline-flex items-center gap-1.5 h-8 px-3.5 rounded-md text-[12.5px] font-medium text-white transition-colors disabled:opacity-60 disabled:pointer-events-none",
                tone === "amber" ? "bg-amber-600 hover:bg-amber-700" : "bg-slate-900 hover:bg-slate-800",
            )}
        >
            {children}
        </button>
    );
}

function relative(at: Date | string): string {
    const diff = Date.now() - new Date(at).getTime();
    const min = Math.round(diff / 60_000);
    if (min < 1) return "just now";
    if (min < 60) return `${min} min ago`;
    const h = Math.round(min / 60);
    if (h < 24) return `${h} hour${h === 1 ? "" : "s"} ago`;
    const d = Math.round(h / 24);
    if (d < 30) return `${d} day${d === 1 ? "" : "s"} ago`;
    return new Date(at).toLocaleDateString();
}
