// The update dialog behind the version pill: what is running, what is newest,
// and the one button that pulls, rebuilds and restarts through the updater.
// While a job runs it shows the steps and the live log; when the backend goes
// away for the restart it says so and keeps polling until it is back.

import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
    AlertTriangle,
    Check,
    CheckCircle2,
    ExternalLink,
    GitBranch,
    Loader2,
    Package,
    RefreshCw,
    RotateCw,
    XCircle,
} from "lucide-react";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { docsUrl } from "@/lib/docs";
import { cn } from "@/lib/utils";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import {
    UPDATE_JOB_KEY,
    UPDATE_STATE_KEY,
    buildLabel,
    isUpdating,
    useUpdateJob,
    useUpdateState,
} from "@/hooks/useUpdateState";
import {
    applyUpdate,
    checkForUpdates,
    type UpdateJob,
    type UpdateState,
} from "@/lib/api/client/admin/updates";
import { markUpdateStarted, readUpdateStarted } from "@/lib/updateSession";

const DOCS_UPDATES = "/development/updates/";

const STEPS: Record<string, string[]> = {
    compose: ["fetch", "checkout", "build", "restart", "prune", "wait"],
    image: ["resolve", "pull", "restart", "prune", "wait"],
    command: ["fetch", "checkout", "command", "wait"],
};

const STEP_LABELS: Record<string, string> = {
    fetch: "Fetch",
    checkout: "Pull",
    build: "Build",
    resolve: "Pin release",
    pull: "Pull images",
    restart: "Restart",
    prune: "Clean up",
    command: "Run script",
    wait: "Wait for backend",
    starting: "Starting",
};

type Phase = "idle" | "running" | "restarting" | "done" | "failed";

interface Props {
    open: boolean;
    onOpenChange: (open: boolean) => void;
}

export function UpdateDialog({ open, onOpenChange }: Props) {
    const qc = useQueryClient();
    const canManage = useAdminPerm(AdminPerm.ManageSettings);
    const stateQ = useUpdateState();
    const jobQ = useUpdateJob(open);
    const state: UpdateState | undefined = jobQ.data ?? stateQ.data;
    const [confirming, setConfirming] = useState(false);
    const started = readUpdateStarted();

    const phase: Phase = useMemo(() => {
        if (isUpdating(state)) return "running";
        if (started && (jobQ.isError || stateQ.isError)) return "restarting";
        const last = state?.updater?.last_job;
        if (started && last && last.status !== "running") {
            return last.status === "succeeded" ? "done" : "failed";
        }
        return "idle";
    }, [state, started, jobQ.isError, stateQ.isError]);

    const canApply =
        canManage &&
        state?.updater?.status === "ok" &&
        !!state?.update_available &&
        !state?.updater?.checkout?.dirty &&
        phase === "idle";

    // Leaving the confirmation open once the update can no longer start
    // (a phase change, or the checkout turning dirty) would let a stale
    // click start a job that fails.
    useEffect(() => {
        if (!canApply) setConfirming(false);
    }, [canApply]);

    const checkMut = useMutation({
        mutationFn: checkForUpdates,
        onSuccess: (data) => {
            qc.setQueryData(UPDATE_STATE_KEY, data);
            qc.setQueryData(UPDATE_JOB_KEY, data);
            toast.success(
                data.update_available ? "A newer version is available" : "This instance is up to date",
            );
        },
        onError: (err: Error) => toast.error(err.message || "Could not check for updates"),
    });

    const applyMut = useMutation({
        mutationFn: () => {
            // The state keeps refreshing while the confirmation is open; a
            // checkout that turned dirty meanwhile must not start a job.
            if (!canApply) return Promise.reject(new Error("The update can no longer start; check the status above."));
            return applyUpdate("latest");
        },
        onSuccess: (job: UpdateJob) => {
            markUpdateStarted(state?.running.version ?? "", state?.running.commit ?? job.from_commit);
            setConfirming(false);
            toast.success("Update started");
            void qc.invalidateQueries({ queryKey: UPDATE_STATE_KEY });
            void qc.invalidateQueries({ queryKey: UPDATE_JOB_KEY });
        },
        onError: (err: Error) => toast.error(err.message || "Could not start the update"),
    });

    const updater = state?.updater;
    const checkout = updater?.checkout;
    const job = updater?.job ?? updater?.last_job;
    // An image install never builds, so the confirmation must not promise a
    // rebuild it will not do.
    const imageMode = updater?.mode === "image";

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-xl">
                <DialogHeader>
                    <DialogTitle>Updates</DialogTitle>
                    <DialogDescription>
                        {state
                            ? `Running ${buildLabel(state)}${state.running.commit ? ` (${state.running.commit.slice(0, 7)})` : ""}.`
                            : "Reading the running version."}
                    </DialogDescription>
                </DialogHeader>

                {phase === "idle" && state && (
                    <div className="space-y-3">
                        <Overview state={state} />
                        {updater?.status !== "ok" && <UpdaterNotice state={state} />}
                        {checkout?.dirty && (
                            <Notice tone="warning">
                                The checkout has local modifications. The updater refuses to move it
                                until they are committed or stashed, or `UPDATER_ALLOW_DIRTY=true`.
                            </Notice>
                        )}
                        {job && job.status === "failed" && !started && (
                            <Notice tone="error">
                                The last update failed at step {STEP_LABELS[job.step] ?? job.step}:{" "}
                                {job.error}
                            </Notice>
                        )}
                        {confirming && (
                            <div className="animate-in fade-in slide-in-from-bottom-1 duration-200">
                            <Notice tone="warning">
                                <div className="font-medium text-foreground">
                                    {imageMode
                                        ? "This pulls the release images and restarts every service."
                                        : "This pulls the checkout, rebuilds the images and restarts every service."}
                                </div>
                                <div className="mt-1">
                                    Sending and syncing pause for a few minutes and resume on their own;
                                    nothing in flight is lost. Migrations apply when the backend comes
                                    back. This panel reconnects by itself.
                                </div>
                            </Notice>
                            </div>
                        )}
                    </div>
                )}

                {(phase === "running" || phase === "restarting") && (
                    <Progress state={state} job={job} restarting={phase === "restarting"} />
                )}

                {phase === "done" && state && (
                    <div className="space-y-3 animate-in fade-in zoom-in-95 duration-300">
                        <div className="flex items-start gap-3 rounded-lg border border-emerald-200 bg-emerald-50 p-3">
                            <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-emerald-600 animate-in zoom-in-50 duration-300" />
                            <div className="text-[13px] leading-relaxed text-emerald-800">
                                <div className="font-semibold">Updated to {buildLabel(state)}</div>
                                Every service is back and sending has resumed. Reload to pick up the
                                new admin panel.
                            </div>
                        </div>
                        {job?.log && <LogPanel lines={job.log} />}
                    </div>
                )}

                {phase === "failed" && (
                    <div className="space-y-3 animate-in fade-in duration-200">
                        <Notice tone="error">
                            <div className="font-semibold text-foreground">The update failed</div>
                            {job?.error ?? "See the log below."} The previous version is still running
                            unless the restart step had already begun.
                        </Notice>
                        {job?.log && <LogPanel lines={job.log} />}
                    </div>
                )}

                <DialogFooter className="gap-2 sm:justify-between">
                    <a
                        href={docsUrl(DOCS_UPDATES)}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1 text-xs font-medium text-[var(--admin-accent-strong)] hover:underline"
                    >
                        How updates work
                        <ExternalLink className="size-3" />
                    </a>
                    <div className="flex flex-wrap items-center gap-2">
                        {phase === "idle" && canManage && (
                            <Button
                                size="sm"
                                variant="outline"
                                onClick={() => checkMut.mutate()}
                                disabled={checkMut.isPending}
                            >
                                <RefreshCw className={cn("size-4", checkMut.isPending && "animate-spin")} />
                                {checkMut.isPending ? "Checking..." : "Check now"}
                            </Button>
                        )}
                        {phase === "idle" && canApply && !confirming && (
                            <Button size="sm" onClick={() => setConfirming(true)}>
                                <RotateCw className="size-4" />
                                Update and restart
                            </Button>
                        )}
                        {phase === "idle" && confirming && (
                            <>
                                <Button size="sm" variant="ghost" onClick={() => setConfirming(false)}>
                                    Cancel
                                </Button>
                                <Button
                                    size="sm"
                                    onClick={() => applyMut.mutate()}
                                    disabled={applyMut.isPending}
                                >
                                    {applyMut.isPending ? (
                                        <Loader2 className="size-4 animate-spin" />
                                    ) : (
                                        <RotateCw className="size-4" />
                                    )}
                                    Update now
                                </Button>
                            </>
                        )}
                        {(phase === "done" || phase === "failed") && (
                            <Button size="sm" onClick={() => window.location.reload()}>
                                Reload
                            </Button>
                        )}
                        {(phase === "running" || phase === "restarting") && (
                            <Button size="sm" variant="ghost" onClick={() => onOpenChange(false)}>
                                Keep running in the background
                            </Button>
                        )}
                    </div>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

function Overview({ state }: { state: UpdateState }) {
    const { latest, updater } = state;
    const checkout = updater?.checkout;
    const release = updater?.release;
    return (
        <dl className="grid grid-cols-[8rem_1fr] gap-x-3 gap-y-2 text-[13px]">
            <dt className="text-muted-foreground">Latest release</dt>
            <dd>
                {latest ? (
                    <span className="inline-flex flex-wrap items-center gap-2">
                        <span className="font-medium">{latest.tag}</span>
                        {latest.published_at && (
                            <span className="text-muted-foreground">
                                {new Date(latest.published_at).toLocaleDateString()}
                            </span>
                        )}
                        {latest.html_url && (
                            <a
                                href={latest.html_url}
                                target="_blank"
                                rel="noreferrer"
                                className="inline-flex items-center gap-1 text-xs font-medium text-[var(--admin-accent-strong)] hover:underline"
                            >
                                Release notes
                                <ExternalLink className="size-3" />
                            </a>
                        )}
                    </span>
                ) : state.check_error ? (
                    <span className="text-amber-700">Could not read releases: {state.check_error}</span>
                ) : state.enabled ? (
                    <span className="text-muted-foreground">No release found for {state.repo}</span>
                ) : (
                    <span className="text-muted-foreground">Release check is off</span>
                )}
            </dd>

            <dt className="text-muted-foreground">Status</dt>
            <dd>
                {state.update_available ? (
                    <Badge variant="outline" className="border-amber-300 bg-amber-50 text-amber-700">
                        Update available
                    </Badge>
                ) : (
                    <Badge variant="outline" className="border-emerald-300 bg-emerald-50 text-emerald-700">
                        Up to date
                    </Badge>
                )}
                {state.checked_at && (
                    <span className="ml-2 text-xs text-muted-foreground">
                        checked {new Date(state.checked_at).toLocaleTimeString()}, every{" "}
                        {state.interval}
                    </span>
                )}
            </dd>

            {release && (
                <>
                    <dt className="text-muted-foreground">Installed</dt>
                    <dd className="flex flex-wrap items-center gap-2">
                        <span className="inline-flex items-center gap-1 font-mono text-xs">
                            <Package className="size-3.5 text-muted-foreground" />
                            {release.prefix}/*:{release.tag}
                        </span>
                        <span className="text-muted-foreground">
                            {release.pinned
                                ? "pinned to this release"
                                : "following the channel tag"}
                        </span>
                    </dd>
                </>
            )}

            {checkout && (
                <>
                    <dt className="text-muted-foreground">Checkout</dt>
                    <dd className="flex flex-wrap items-center gap-2">
                        <span className="inline-flex items-center gap-1 font-mono text-xs">
                            <GitBranch className="size-3.5 text-muted-foreground" />
                            {checkout.detached ? "pinned" : checkout.branch}@{checkout.commit.slice(0, 7)}
                        </span>
                        {!checkout.detached && (
                            <span className="text-muted-foreground">
                                {checkout.behind > 0
                                    ? `${checkout.behind} commit${checkout.behind === 1 ? "" : "s"} behind`
                                    : "matches the remote"}
                            </span>
                        )}
                        {checkout.fetch_error && (
                            <span className="text-amber-700">fetch failed: {checkout.fetch_error}</span>
                        )}
                    </dd>
                </>
            )}

            <dt className="text-muted-foreground">Updater</dt>
            <dd>
                {updater?.status === "ok" && (
                    <span>
                        ready
                        <span className="text-muted-foreground"> ({updater.mode} mode)</span>
                    </span>
                )}
                {updater?.status === "off" && <span className="text-muted-foreground">not configured</span>}
                {updater?.status === "unreachable" && <span className="text-amber-700">unreachable</span>}
            </dd>
        </dl>
    );
}

function UpdaterNotice({ state }: { state: UpdateState }) {
    const u = state.updater;
    // A clone-free install has no checkout to pull, so the by-hand command is
    // the compose one. Which install this is comes from the release block, and
    // an unreachable updater reports neither block, so that case names both
    // rather than printing one command that may not work.
    //
    // A pinned install needs the tag edited first: pulling again resolves the
    // same fixed version and updates nothing, which is the kind of instruction
    // that looks like it worked.
    const pinnedTag = u?.release?.pinned ? (state.latest?.tag ?? "vX.Y.Z") : null;
    const byHand = u?.release
        ? "docker compose pull && docker compose up -d"
        : u?.checkout
          ? "git pull && make up"
          : null;
    if (u?.status === "unreachable") {
        return (
            <Notice tone="warning">
                <div className="font-medium text-foreground">The updater is not answering</div>
                {u.error} Until it does, update by hand from the install directory:
                {pinnedTag && <SetTagFirst tag={pinnedTag} />}
                {byHand ? <Cmd>{byHand}</Cmd> : <BothCommands />}
            </Notice>
        );
    }
    return (
        <Notice tone="info">
            <div className="font-medium text-foreground">This panel can only report</div>
            No updater is configured, so apply updates from a shell on the host:
            {pinnedTag && <SetTagFirst tag={pinnedTag} />}
            {byHand ? <Cmd>{byHand}</Cmd> : <BothCommands />}
            To get the button, enable the updater compose profile (`make up` and the installer
            both do) or run the updater unit on a bare-metal host.
        </Notice>
    );
}

// An image install pinned to a fixed version resolves the same images on every
// pull, so moving it is an edit to .env and then the pull. Without this the
// command below reports success and changes nothing.
function SetTagFirst({ tag }: { tag: string }) {
    return (
        <>
            <div className="mt-1.5 text-xs">
                This install is pinned, so set the release in <code>.env</code> first:
            </div>
            <Cmd>{`WARMBLY_TAG=${tag}`}</Cmd>
            <div className="text-xs">then:</div>
        </>
    );
}

// Shown when the updater is unreachable, so nothing says which shape of install
// this is. Naming both beats guessing: the wrong one fails confusingly on an
// install that has no checkout, or no images to pull.
function BothCommands() {
    return (
        <>
            <div className="mt-1.5 text-xs">From an install.sh install:</div>
            <Cmd>docker compose pull && docker compose up -d</Cmd>
            <div className="text-xs">From a git checkout:</div>
            <Cmd>git pull && make up</Cmd>
        </>
    );
}

function Progress({
    state,
    job,
    restarting,
}: {
    state: UpdateState | undefined;
    job: UpdateJob | undefined;
    restarting: boolean;
}) {
    const mode = state?.updater?.mode ?? "compose";
    const steps = STEPS[mode] ?? STEPS.compose;
    const current = restarting ? "wait" : (job?.step ?? "starting");
    const currentIdx = Math.max(0, steps.indexOf(current));
    const percent = Math.round(((currentIdx + 0.5) / steps.length) * 100);
    return (
        <div className="space-y-3 animate-in fade-in duration-200">
            <div className="flex items-start gap-3 rounded-lg border border-sky-200 bg-sky-50/60 p-3">
                <Loader2 className="mt-0.5 size-4 shrink-0 animate-spin text-sky-600" />
                <div className="min-w-0 flex-1 text-[13px] leading-relaxed text-sky-900">
                    <div className="flex items-center justify-between gap-3">
                        <span className="font-semibold">
                            {restarting ? "Restarting services" : `Updating: ${STEP_LABELS[current] ?? current}`}
                        </span>
                        <span className="text-xs tabular-nums text-sky-700">{percent}%</span>
                    </div>
                    <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-sky-100">
                        <div
                            className="h-full rounded-full bg-sky-500 transition-[width] duration-500 ease-out"
                            style={{ width: `${percent}%` }}
                        />
                    </div>
                    <div className="mt-1.5 text-xs text-sky-800/80">
                        {restarting
                            ? "The backend is coming back up. This panel reconnects on its own; keep the tab open or come back later, the result is kept."
                            : "You can close this dialog; the pill in the top bar keeps following the job."}
                    </div>
                </div>
            </div>
            <ol className="flex flex-wrap gap-1.5">
                {steps.map((s, i) => {
                    const done = currentIdx > i || (restarting && s !== "wait");
                    const active = s === current;
                    return (
                        <li
                            key={s}
                            className={cn(
                                "inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px]",
                                done && "border-emerald-200 bg-emerald-50 text-emerald-700",
                                active && "border-sky-300 bg-sky-50 text-sky-700",
                                !done && !active && "border-border text-muted-foreground",
                            )}
                        >
                            {done ? (
                                <Check className="size-3" />
                            ) : active ? (
                                <Loader2 className="size-3 animate-spin" />
                            ) : null}
                            {STEP_LABELS[s] ?? s}
                        </li>
                    );
                })}
            </ol>
            {job?.log && job.log.length > 0 && <LogPanel lines={job.log} />}
        </div>
    );
}

function LogPanel({ lines }: { lines: string[] }) {
    const ref = useRef<HTMLPreElement>(null);
    useEffect(() => {
        const el = ref.current;
        if (el) el.scrollTop = el.scrollHeight;
    }, [lines.length]);
    return (
        <pre
            ref={ref}
            className="max-h-64 overflow-auto rounded-md border border-border bg-zinc-950 p-3 text-[11px] leading-relaxed text-zinc-200"
        >
            {lines.join("\n")}
        </pre>
    );
}

function Notice({ tone, children }: { tone: "info" | "warning" | "error"; children: React.ReactNode }) {
    const styles = {
        info: "border-sky-200 bg-sky-50/60 text-sky-900",
        warning: "border-amber-200 bg-amber-50/60 text-amber-900",
        error: "border-red-200 bg-red-50/60 text-red-900",
    }[tone];
    const Icon = tone === "error" ? XCircle : AlertTriangle;
    return (
        <div className={cn("flex items-start gap-3 rounded-lg border p-3 text-[13px] leading-relaxed", styles)}>
            <Icon className="mt-0.5 size-4 shrink-0" />
            <div className="min-w-0 flex-1">{children}</div>
        </div>
    );
}

function Cmd({ children }: { children: string }) {
    return (
        <code className="mt-1.5 mb-1.5 block rounded bg-white/70 px-2 py-1 font-mono text-[12px] text-foreground">
            {children}
        </code>
    );
}
