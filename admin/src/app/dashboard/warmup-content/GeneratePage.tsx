// /warmup-content/generate — enqueue warmup-content generation. Two modes in
// one full-width page:
//
//   Sync   — the existing immediate-enqueue path (one background job).
//   Batch  — OpenAI Batch API: cheaper, async, high volume. Exposes every
//            knob (pool, segment, model, count up to 2000, max messages,
//            completion window) plus a theme multi-input that fans out one
//            batch job per theme. Returned job ids are surfaced in a live
//            "Recent batch jobs" view that polls while anything is in flight,
//            with an inline Cancel per job.
//
// Neither mode runs inline: submitting returns job id(s); progress is watched
// here and on the Jobs tab.

import { useMemo, useState, type KeyboardEvent } from "react";
import {
    keepPreviousData,
    useMutation,
    useQuery,
    useQueryClient,
} from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { AlertTriangle, Ban, Layers, Sparkles, X, Zap } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { ErrorState } from "@/components/ErrorState";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { APIError } from "@/lib/api/client";
import {
    cancelWarmupBatch,
    generateWarmupContent,
    isJobActive,
    isJobCancellable,
    listWarmupGenerationJobs,
    submitWarmupBatch,
    type GenerateBatchRequest,
    type WarmupGenerationJob,
} from "@/lib/api/client/admin/warmupContent";
import { cn } from "@/lib/utils";
import { PoolBadge } from "./components";
import { batchTone, fmtDate, jobTone } from "./shared";

const COMPLETION_WINDOWS = ["24h"];

// A 400 whose code marks generation as un-runnable. We disable submit and show
// a clear banner instead of letting the user fire a doomed request.
const NOT_CONFIGURED_CODES = new Set([
    "not_configured",
    "daily_cap_reached",
]);

function notConfiguredMessage(err: unknown): string | null {
    if (err instanceof APIError && err.status === 400) {
        if (!err.code || NOT_CONFIGURED_CODES.has(err.code)) {
            return err.message || "Generation is not configured right now.";
        }
    }
    return null;
}

type Mode = "sync" | "batch";

export default function GeneratePage() {
    const navigate = useNavigate();
    const [mode, setMode] = useState<Mode>("sync");

    return (
        <div className="w-full space-y-6">
            <div className="inline-flex rounded-lg border border-border bg-muted/40 p-1">
                <button
                    type="button"
                    onClick={() => setMode("sync")}
                    className={cn(
                        "inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
                        mode === "sync"
                            ? "bg-background text-foreground shadow-sm"
                            : "text-foreground/60 hover:text-foreground",
                    )}
                >
                    <Zap className="size-4" /> Instant
                </button>
                <button
                    type="button"
                    onClick={() => setMode("batch")}
                    className={cn(
                        "inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
                        mode === "batch"
                            ? "bg-background text-foreground shadow-sm"
                            : "text-foreground/60 hover:text-foreground",
                    )}
                >
                    <Layers className="size-4" /> Batch
                </button>
            </div>
            <p className="max-w-3xl text-[12.5px] text-muted-foreground">
                Same result either way — both produce warmup threads and add the passing ones
                to the library. They only differ in <em>how</em> the AI runs them:{" "}
                <strong className="font-medium text-foreground">Instant</strong> generates
                immediately (best for a handful while you watch).{" "}
                <strong className="font-medium text-foreground">Batch</strong> hands the work
                to OpenAI's Batch API — about 50% cheaper and processed in the background (up
                to ~24h) — best for generating a lot at once.
            </p>

            {mode === "sync" ? (
                <SyncGenerate onQueued={() => navigate("/warmup-content/jobs")} />
            ) : (
                <BatchGenerate />
            )}
        </div>
    );
}

// ====================================================================
// Sync mode — the existing immediate-enqueue path.
// ====================================================================

function SyncGenerate({ onQueued }: { onQueued: () => void }) {
    const qc = useQueryClient();
    const [count, setCount] = useState(10);
    const [segment, setSegment] = useState("");
    const [theme, setTheme] = useState("");
    const [model, setModel] = useState("");

    const generate = useMutation({
        mutationFn: () =>
            generateWarmupContent({
                count,
                segment: segment.trim() || undefined,
                theme: theme.trim() || undefined,
                model: model.trim() || undefined,
            }),
        onSuccess: (res) => {
            toast.success("Generation job queued", {
                description: `Job ${res.job_id} is running offline. Watch the Jobs tab for progress.`,
            });
            qc.invalidateQueries({ queryKey: ["admin", "warmup-content", "jobs"] });
            onQueued();
        },
        onError: (err: Error) => toast.error(err.message || "Failed to queue job"),
    });

    const blocked = notConfiguredMessage(generate.error);

    return (
        <div className="space-y-5">
            <div className="rounded-lg border border-[var(--admin-accent)]/30 bg-[var(--admin-accent-soft)]/40 p-3 text-[12.5px] text-muted-foreground">
                <div className="flex items-center gap-2 font-medium text-foreground">
                    <Sparkles className="size-4 text-[var(--admin-accent-strong)]" />
                    Runs offline
                </div>
                <p className="mt-1">
                    Generation does not run inline. Submitting enqueues a background job
                    that produces threads, lint-checks them, and adds the passing ones to
                    the library. Track progress on the Jobs tab.
                </p>
            </div>

            {blocked && <NotConfiguredBanner message={blocked} />}

            <div className="grid gap-4 rounded-lg border border-border bg-card p-4 sm:grid-cols-2 lg:grid-cols-3">
                <div>
                    <Label htmlFor="gen-count" className="mb-1 text-xs">
                        Count
                    </Label>
                    <Input
                        id="gen-count"
                        type="number"
                        min={1}
                        max={500}
                        value={count}
                        onChange={(e) =>
                            setCount(Math.max(1, Math.min(500, Number(e.target.value) || 0)))
                        }
                    />
                    <p className="mt-1 text-[11px] text-muted-foreground">
                        Number of conversation threads to generate.
                    </p>
                </div>

                <div>
                    <Label htmlFor="gen-segment" className="mb-1 text-xs">
                        Segment <span className="text-muted-foreground">(optional)</span>
                    </Label>
                    <Input
                        id="gen-segment"
                        placeholder="e.g. saas, agency, ecommerce"
                        value={segment}
                        onChange={(e) => setSegment(e.target.value)}
                    />
                </div>

                <div className="sm:col-span-2 lg:col-span-3">
                    <Label htmlFor="gen-theme" className="mb-1 text-xs">
                        Theme <span className="text-muted-foreground">(optional)</span>
                    </Label>
                    <Textarea
                        id="gen-theme"
                        placeholder="Steer the topic, e.g. 'casual product follow-ups between colleagues'"
                        value={theme}
                        onChange={(e) => setTheme(e.target.value)}
                    />
                </div>

                <div>
                    <Label htmlFor="gen-model" className="mb-1 text-xs">
                        Model override{" "}
                        <span className="text-muted-foreground">(optional)</span>
                    </Label>
                    <Input
                        id="gen-model"
                        placeholder="Defaults to the configured model"
                        value={model}
                        onChange={(e) => setModel(e.target.value)}
                    />
                </div>

                <div className="flex justify-end sm:col-span-2 lg:col-span-3">
                    <Button
                        onClick={() => generate.mutate()}
                        disabled={generate.isPending || count < 1 || !!blocked}
                    >
                        <Sparkles className="size-4" />
                        {generate.isPending ? "Queuing…" : "Queue generation job"}
                    </Button>
                </div>
            </div>
        </div>
    );
}

// ====================================================================
// Batch mode — OpenAI Batch API, every knob exposed + live monitor.
// ====================================================================

function BatchGenerate() {
    const qc = useQueryClient();

    const [segment, setSegment] = useState("");
    const [model, setModel] = useState("");
    const [count, setCount] = useState(100);
    const [maxMessages, setMaxMessages] = useState(6);
    const [completionWindow, setCompletionWindow] = useState("24h");
    const [themes, setThemes] = useState<string[]>([]);
    const [themeDraft, setThemeDraft] = useState("");
    const [lastJobIds, setLastJobIds] = useState<string[]>([]);

    function commitTheme() {
        const t = themeDraft.trim();
        if (!t) return;
        setThemes((prev) => (prev.includes(t) ? prev : [...prev, t]));
        setThemeDraft("");
    }
    function onThemeKeyDown(e: KeyboardEvent<HTMLInputElement>) {
        if (e.key === "Enter" || e.key === ",") {
            e.preventDefault();
            commitTheme();
        } else if (e.key === "Backspace" && !themeDraft && themes.length) {
            setThemes((prev) => prev.slice(0, -1));
        }
    }
    function removeTheme(t: string) {
        setThemes((prev) => prev.filter((x) => x !== t));
    }

    const submit = useMutation({
        mutationFn: () => {
            const body: GenerateBatchRequest = {
                segment: segment.trim() || undefined,
                model: model.trim() || undefined,
                count,
                max_messages: maxMessages,
                completion_window: completionWindow,
                themes: themes.length ? themes : undefined,
            };
            return submitWarmupBatch(body);
        },
        onSuccess: (res) => {
            const ids = res.job_ids ?? [];
            setLastJobIds(ids);
            toast.success(
                ids.length === 1
                    ? "Batch job submitted"
                    : `${ids.length} batch jobs submitted`,
                {
                    description: "Watch progress below or on the Jobs tab.",
                },
            );
            qc.invalidateQueries({ queryKey: ["admin", "warmup-content", "jobs"] });
        },
        onError: (err: Error) => {
            if (!notConfiguredMessage(err)) {
                toast.error(err.message || "Failed to submit batch");
            }
        },
    });

    const blocked = notConfiguredMessage(submit.error);
    const jobCount = themes.length ? themes.length : 1;

    return (
        <div className="grid gap-6 lg:grid-cols-[minmax(0,28rem)_minmax(0,1fr)]">
            <div className="space-y-5">
                <div className="rounded-lg border border-sky-300/40 bg-sky-50/50 p-3 text-[12.5px] text-muted-foreground">
                    <div className="flex items-center gap-2 font-medium text-foreground">
                        <Layers className="size-4 text-sky-600" />
                        OpenAI Batch — async &amp; cheaper
                    </div>
                    <p className="mt-1">
                        Batch generation runs through the OpenAI Batch API: lower cost, much
                        higher volume, and completion within the chosen window (typically up
                        to {completionWindow}). Add multiple themes to fan out one batch job
                        per theme; leave themes empty to rotate the configured defaults.
                    </p>
                </div>

                {blocked && <NotConfiguredBanner message={blocked} />}

                <div className="space-y-4 rounded-lg border border-border bg-card p-4">
                    <div className="grid gap-4 sm:grid-cols-2">
                        <div>
                            <Label className="mb-1 text-xs">Completion window</Label>
                            <Select
                                value={completionWindow}
                                onValueChange={setCompletionWindow}
                            >
                                <SelectTrigger>
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    {COMPLETION_WINDOWS.map((w) => (
                                        <SelectItem key={w} value={w}>
                                            {w}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>

                        <div>
                            <Label htmlFor="batch-count" className="mb-1 text-xs">
                                Count{" "}
                                <span className="text-muted-foreground">
                                    {themes.length ? "(per theme)" : "(threads)"}
                                </span>
                            </Label>
                            <Input
                                id="batch-count"
                                type="number"
                                min={1}
                                max={2000}
                                value={count}
                                onChange={(e) =>
                                    setCount(
                                        Math.max(1, Math.min(2000, Number(e.target.value) || 0)),
                                    )
                                }
                            />
                            <p className="mt-1 text-[11px] text-muted-foreground">
                                Up to 2000; the server also clamps to the daily cap.
                            </p>
                        </div>
                        <div>
                            <Label htmlFor="batch-max-msgs" className="mb-1 text-xs">
                                Max messages / thread
                            </Label>
                            <Input
                                id="batch-max-msgs"
                                type="number"
                                min={1}
                                max={50}
                                value={maxMessages}
                                onChange={(e) =>
                                    setMaxMessages(
                                        Math.max(1, Math.min(50, Number(e.target.value) || 0)),
                                    )
                                }
                            />
                        </div>
                    </div>

                    <div>
                        <Label htmlFor="batch-segment" className="mb-1 text-xs">
                            Segment <span className="text-muted-foreground">(optional)</span>
                        </Label>
                        <Input
                            id="batch-segment"
                            placeholder="e.g. saas, agency, ecommerce"
                            value={segment}
                            onChange={(e) => setSegment(e.target.value)}
                        />
                    </div>

                    <div>
                        <Label htmlFor="batch-model" className="mb-1 text-xs">
                            Model override{" "}
                            <span className="text-muted-foreground">(optional)</span>
                        </Label>
                        <Input
                            id="batch-model"
                            placeholder="Defaults to the configured model"
                            value={model}
                            onChange={(e) => setModel(e.target.value)}
                        />
                    </div>

                    <div>
                        <Label htmlFor="batch-theme" className="mb-1 text-xs">
                            Themes{" "}
                            <span className="text-muted-foreground">
                                (optional — one job per theme)
                            </span>
                        </Label>
                        <div className="flex flex-wrap items-center gap-1.5 rounded-md border border-input bg-transparent p-1.5 focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/30">
                            {themes.map((t) => (
                                <span
                                    key={t}
                                    className="inline-flex items-center gap-1 rounded bg-sky-50 px-2 py-0.5 text-xs text-sky-700 ring-1 ring-inset ring-sky-200"
                                >
                                    {t}
                                    <button
                                        type="button"
                                        className="rounded-full p-0.5 hover:bg-sky-100"
                                        onClick={() => removeTheme(t)}
                                        aria-label={`Remove ${t}`}
                                    >
                                        <X className="size-3" />
                                    </button>
                                </span>
                            ))}
                            <input
                                id="batch-theme"
                                value={themeDraft}
                                onChange={(e) => setThemeDraft(e.target.value)}
                                onKeyDown={onThemeKeyDown}
                                onBlur={commitTheme}
                                placeholder={
                                    themes.length
                                        ? "Add another…"
                                        : "Type a theme and press Enter"
                                }
                                className="min-w-[8rem] flex-1 bg-transparent px-1 py-0.5 text-sm outline-none placeholder:text-muted-foreground"
                            />
                        </div>
                        <p className="mt-1 text-[11px] text-muted-foreground">
                            Press Enter or comma to add. With themes set, the count above is
                            per theme.
                        </p>
                    </div>

                    <div className="flex items-center justify-between gap-3 border-t border-border pt-3">
                        <p className="text-[11px] text-muted-foreground">
                            Will submit{" "}
                            <span className="font-medium text-foreground">{jobCount}</span>{" "}
                            batch job{jobCount === 1 ? "" : "s"} ·{" "}
                            <span className="font-medium text-foreground">{count}</span>{" "}
                            thread{count === 1 ? "" : "s"} each
                        </p>
                        <Button
                            onClick={() => submit.mutate()}
                            disabled={submit.isPending || count < 1 || !!blocked}
                        >
                            <Layers className="size-4" />
                            {submit.isPending ? "Submitting…" : "Submit batch"}
                        </Button>
                    </div>
                </div>

                {lastJobIds.length > 0 && (
                    <div className="rounded-lg border border-emerald-300/50 bg-emerald-50/50 p-3 text-[12.5px]">
                        <div className="font-medium text-emerald-800">
                            Submitted {lastJobIds.length} job
                            {lastJobIds.length === 1 ? "" : "s"}
                        </div>
                        <div className="mt-1 flex flex-wrap gap-1.5">
                            {lastJobIds.map((id) => (
                                <Badge
                                    key={id}
                                    variant="outline"
                                    className="font-mono text-[10px]"
                                >
                                    {id}
                                </Badge>
                            ))}
                        </div>
                    </div>
                )}
            </div>

            <BatchMonitor highlightIds={lastJobIds} />
        </div>
    );
}

// ====================================================================
// Live batch monitor — polls jobs while any is in flight, cancel inline.
// ====================================================================

function BatchMonitor({ highlightIds }: { highlightIds: string[] }) {
    const qc = useQueryClient();
    const highlight = useMemo(() => new Set(highlightIds), [highlightIds]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup-content", "jobs", "batch-monitor"],
        queryFn: () => listWarmupGenerationJobs({ limit: 25 }),
        placeholderData: keepPreviousData,
        refetchInterval: (query) => {
            const rows = query.state.data?.data ?? [];
            return rows.some(isJobActive) ? 10_000 : false;
        },
    });

    const cancel = useMutation({
        mutationFn: (id: string) => cancelWarmupBatch(id),
        onSuccess: () => {
            toast.success("Batch cancellation requested");
            qc.invalidateQueries({ queryKey: ["admin", "warmup-content", "jobs"] });
        },
        onError: (err: Error) => toast.error(err.message || "Failed to cancel job"),
    });

    const batchJobs = (data?.data ?? []).filter((j) => j.mode === "batch");

    return (
        <div className="space-y-3">
            <div className="flex items-center justify-between">
                <h2 className="text-sm font-semibold">Recent batch jobs</h2>
                <span className="text-[11px] text-muted-foreground">
                    auto-refreshes while running
                </span>
            </div>

            {error ? (
                <ErrorState
                    error={error}
                    title="Failed to load batch jobs"
                    onRetry={() => refetch()}
                />
            ) : isLoading ? (
                <div className="space-y-2">
                    <Skeleton className="h-20" />
                    <Skeleton className="h-20" />
                </div>
            ) : batchJobs.length === 0 ? (
                <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
                    No batch jobs yet. Submit one to watch it here.
                </div>
            ) : (
                <div className="space-y-2">
                    {batchJobs.map((j) => (
                        <BatchJobCard
                            key={j.id}
                            job={j}
                            highlighted={highlight.has(j.id)}
                            onCancel={() => cancel.mutate(j.id)}
                            cancelling={cancel.isPending}
                        />
                    ))}
                </div>
            )}
        </div>
    );
}

function BatchJobCard({
    job,
    highlighted,
    onCancel,
    cancelling,
}: {
    job: WarmupGenerationJob;
    highlighted: boolean;
    onCancel: () => void;
    cancelling: boolean;
}) {
    const requested = job.requested_count || 0;
    const generated = job.generated_count || 0;
    const pct =
        requested > 0 ? Math.min(100, Math.round((generated / requested) * 100)) : 0;

    return (
        <div
            className={cn(
                "rounded-lg border bg-card p-3",
                highlighted ? "border-sky-400 ring-1 ring-sky-200" : "border-border",
            )}
        >
            <div className="flex items-start justify-between gap-3">
                <div className="min-w-0 space-y-1">
                    <div className="flex flex-wrap items-center gap-1.5">
                        <PoolBadge pool={job.pool_type} />
                        <Badge
                            variant="outline"
                            className={`text-[10px] ${jobTone(job.status)}`}
                        >
                            {job.status}
                        </Badge>
                        {job.batch_status && (
                            <Badge
                                variant="outline"
                                className={`text-[10px] ${batchTone(job.batch_status)}`}
                            >
                                {job.batch_status}
                            </Badge>
                        )}
                        {job.completion_window && (
                            <span className="text-[10px] text-muted-foreground">
                                {job.completion_window}
                            </span>
                        )}
                    </div>
                    <div className="truncate font-mono text-[11px] text-muted-foreground">
                        {job.id}
                    </div>
                    {job.theme && (
                        <div className="truncate text-[11px] text-muted-foreground">
                            theme: {job.theme}
                        </div>
                    )}
                </div>
                {isJobCancellable(job) && (
                    <Button
                        size="xs"
                        variant="outline"
                        className="shrink-0 text-red-700 hover:bg-red-50"
                        onClick={onCancel}
                        disabled={cancelling}
                    >
                        <Ban className="size-3" /> Cancel
                    </Button>
                )}
            </div>

            <div className="mt-2.5">
                <div className="mb-1 flex items-center justify-between text-[11px] text-muted-foreground">
                    <span>
                        {generated.toLocaleString()} / {requested.toLocaleString()} generated
                    </span>
                    <span className="tabular-nums">{pct}%</span>
                </div>
                <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
                    <div
                        className="h-full rounded-full bg-sky-500 transition-all"
                        style={{ width: `${pct}%` }}
                    />
                </div>
            </div>

            <div className="mt-2 grid grid-cols-2 gap-x-4 gap-y-0.5 text-[11px] text-muted-foreground">
                <span>Lint rejected: {job.lint_rejected_count}</span>
                <span>Failed: {job.failed_count}</span>
                <span>Started: {fmtDate(job.started_at)}</span>
                <span>Finished: {fmtDate(job.finished_at)}</span>
            </div>

            {job.error && (
                <div className="mt-2 rounded border border-red-200 bg-red-50 px-2 py-1 text-[11px] text-red-700">
                    {job.error}
                </div>
            )}
        </div>
    );
}

// ====================================================================
// Shared "not configured" banner.
// ====================================================================

function NotConfiguredBanner({ message }: { message: string }) {
    return (
        <div className="flex items-start gap-2 rounded-lg border border-amber-300 bg-amber-50 p-3 text-[12.5px] text-amber-800">
            <AlertTriangle className="mt-0.5 size-4 shrink-0" />
            <div>
                <div className="font-medium">Generation unavailable</div>
                <p className="mt-0.5">{message}</p>
            </div>
        </div>
    );
}
