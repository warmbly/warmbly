// /warmup-content/overview: the automation status panel for the warmup
// content library: pipeline readiness (configured, enabled, scheduled),
// library stock vs the scheduler's targets, today's generation budget,
// headline counts, and the content-source vs spam-placement A/B comparison.

import { useMemo, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import {
    Archive,
    CheckCircle2,
    CircleAlert,
    CircleDashed,
    Inbox,
    Play,
} from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import {
    EmptyState,
    Panel,
    Property,
    PropertyList,
    Section,
    Stat,
    StatGrid,
    StatusBadge,
} from "@/components/ui/kit";
import { ErrorState } from "@/components/ErrorState";
import { TONE_DOT, TONE_TEXT, type Tone } from "@/lib/tones";
import { cn } from "@/lib/utils";
import {
    getWarmupContentAb,
    getWarmupContentOverview,
    putWarmupGenerationSettings,
    type WarmupGenerationSettings,
    type WarmupContentOverview,
} from "@/lib/api/client/admin/warmupContent";
import { Td, Th } from "./components";
import { fmtDate } from "./shared";

interface PipelineStep {
    label: string;
    ok: boolean;
    detail: string;
}

// Cached-content selection and new generation have independent controls.
function pipelineSteps(d: WarmupContentOverview): PipelineStep[] {
    const totalTarget = d.stock.reduce((n, s) => n + s.target, 0);
    const totalStock = d.stock.reduce((n, s) => n + Math.min(s.active, s.target), 0);
    const stocked = totalTarget > 0 && totalStock >= totalTarget;
    return [
        {
            label: "Generation enabled",
            ok: d.ai_configured && d.generation_enabled !== false,
            detail: !d.ai_configured ? "No compatible generation client configured" : d.generation_enabled === false ? "New jobs stopped; existing batches drain" : "Configured model checked before submission",
        },
        {
            label: "AI content enabled",
            ok: d.ai_enabled,
            detail: d.ai_enabled
                ? `${d.ai_selection_share}% of warmup sends draw AI content`
                : "Static fallback is active",
        },
        {
            label: "Scheduled top-up",
            ok: d.schedule_enabled,
            detail: d.schedule_enabled
                ? `Tops the library up every ${d.cadence_hours}h`
                : "Static fallback is active",
        },
        {
            label: "Library stocked",
            ok: stocked,
            detail:
                totalTarget > 0
                    ? `${totalStock.toLocaleString()} of ${totalTarget.toLocaleString()} target threads active`
                    : "Waiting for demand history",
        },
    ];
}

function StepIcon({ ok, blocked }: { ok: boolean; blocked: boolean }) {
    if (ok) return <CheckCircle2 className={cn("size-4 shrink-0", TONE_TEXT.success)} />;
    if (blocked) return <CircleDashed className="size-4 shrink-0 text-subtle-foreground" />;
    return <CircleAlert className={cn("size-4 shrink-0", TONE_TEXT.warning)} />;
}

function Meter({ value, tone, className }: { value: number; tone: Tone; className?: string }) {
    return (
        <span className={cn("inline-block h-1.5 overflow-hidden rounded-full bg-muted", className)}>
            <span className={cn("block h-full rounded-full", TONE_DOT[tone])} style={{ width: `${value}%` }} />
        </span>
    );
}

function AutomationPanel({ data }: { data: WarmupContentOverview }) {
    const steps = pipelineSteps(data);
    const allOk = steps.every((s) => s.ok);
    const firstGap = steps.findIndex((s) => !s.ok);
    const capped = data.daily_generation_cap > 0;
    const budgetUsed = capped
        ? Math.min(100, Math.round(((data.reserved_today ?? data.generated_today) / data.daily_generation_cap) * 100))
        : 0;

    return (
        <Panel
            title="Automatic extension"
            actions={
                <StatusBadge tone="success" dot>
                    {data.generation_enabled === false ? "Generation stopped" : "Generation control"}
                </StatusBadge>
            }
            bodyClassName="p-0"
        >
            <p className="px-4 pt-3 text-[12.5px] leading-relaxed text-muted-foreground">
                {allOk
                    ? data.refresh_enabled
                        ? "Scheduled generation preserves canonical content and reviews complete rendered diagnostic threads before activating them."
                        : "The library tops itself up to the target. Continuous refresh is off, so generation pauses once the target is reached."
                    : "Not fully automatic yet. Fix the first amber step below and the library will keep itself stocked without manual runs."}
            </p>

            <ol className="mx-4 mt-3 grid gap-px overflow-hidden rounded-md border border-border bg-border sm:grid-cols-2 xl:grid-cols-4">
                {steps.map((s, i) => {
                    const isGap = !s.ok && i === firstGap;
                    return (
                        <li
                            key={s.label}
                            className="relative flex items-start gap-2.5 bg-card px-3 py-2.5"
                        >
                            {isGap && <span aria-hidden className="pointer-events-none absolute inset-0 bg-amber-500/[0.08]" />}
                            <StepIcon ok={s.ok} blocked={!s.ok && !isGap} />
                            <div className={cn("relative min-w-0", !s.ok && !isGap && "opacity-70")}>
                                <div className="text-[13px] font-medium text-foreground">
                                    <span className="mr-1 tabular-nums text-subtle-foreground">{i + 1}</span>
                                    {s.label}
                                </div>
                                <div className="mt-0.5 text-xs leading-relaxed text-muted-foreground">{s.detail}</div>
                            </div>
                        </li>
                    );
                })}
            </ol>

            <PropertyList className="mt-2 px-4">
                <Property label="Today's budget">
                    {capped ? (
                        <span className="inline-flex flex-wrap items-center gap-2.5">
                            <span className="tabular-nums">
                                {(data.reserved_today ?? data.generated_today).toLocaleString()} / {data.daily_generation_cap.toLocaleString()}{" "}
                                requests reserved today
                            </span>
                            <Meter value={budgetUsed} tone={budgetUsed >= 100 ? "warning" : "success"} className="w-24" />
                        </span>
                    ) : (
                        <span className="text-muted-foreground">
                            uncapped ({data.generated_today.toLocaleString()} generated today)
                        </span>
                    )}
                </Property>
                <Property label="Continuous refresh">
                    {data.refresh_enabled ? (
                        <span>
                            On, recycles the <span className="tabular-nums">{data.refresh_per_run}</span> most-used threads
                            each run
                        </span>
                    ) : (
                        <span className="text-muted-foreground">Static fallback active</span>
                    )}
                </Property>
            </PropertyList>

            <p className="border-t border-border px-4 py-3 text-xs leading-relaxed text-muted-foreground">
                Generated threads are humanized, lint-gated, and any send that fails the gate falls back to the static
                library. Threads with a meaningful sample and unsafe spam placement are archived automatically.
            </p>
        </Panel>
    );
}

function GenerationSettingsPanel({ data }: { data: WarmupContentOverview }) {
    const canManage = useAdminPerm(AdminPerm.ManageSettings);
    const qc = useQueryClient();
    const [draft, setDraft] = useState<string | null>(null);
    const save = useMutation({
        mutationFn: putWarmupGenerationSettings,
        onSuccess: () => {
            setDraft(null);
            void qc.invalidateQueries({ queryKey: ["admin", "warmup-content"] });
            toast.success("Generation settings saved");
        },
        onError: (err: Error) => toast.error(err.message || "Failed to save generation settings"),
    });
    if (!data.effective_settings) return <Panel title="Effective generation settings">This backend does not expose generation controls. Upgrade the backend before changing settings.</Panel>;
    const text = draft ?? JSON.stringify(data.effective_settings, null, 2);
    const submit = () => {
        try {
            const settings: WarmupGenerationSettings = JSON.parse(text);
            if (!settings || typeof settings !== "object" || Array.isArray(settings)) throw new Error("Expected a settings object");
            save.mutate(settings);
        } catch {
            toast.error("Enter a valid JSON settings object");
        }
    };
    return (
        <Panel title="Effective generation settings">
            <p className="mb-3 text-sm text-muted-foreground">Stopping generation blocks new jobs without deleting credentials. Already submitted batches continue polling and ingest safely; cancel them explicitly in Jobs. Cached-content use is controlled separately by enabled. A stop is effective only after all backends are upgraded.</p>
            <p className="mb-3 text-sm text-muted-foreground">{data.provider_capability}</p>
            <Textarea aria-label="Effective generation settings JSON" value={text} onChange={(e) => setDraft(e.target.value)} readOnly={!canManage} className="min-h-72 font-mono text-xs" />
            {canManage && <div className="mt-3 flex gap-2">
                <Button disabled={save.isPending} onClick={submit}>Save settings</Button>
                <Button variant="outline" disabled={save.isPending} onClick={() => save.mutate({ generation_enabled: !data.generation_enabled })}>{data.generation_enabled ? "Stop new generation" : "Resume generation"}</Button>
                <Button variant="ghost" disabled={save.isPending || draft === null} onClick={() => setDraft(null)}>Discard edits</Button>
            </div>}
        </Panel>
    );
}

function StockTable({ data }: { data: WarmupContentOverview }) {
    if (data.stock.length === 0) return null;
    return (
        <Section
            title="Stock vs target"
            description="The target is calculated from the last seven days of total warmup sends. The controller keeps at least 200 shared threads, expands the bank automatically, and submits at most 250 new threads in one batch."
        >
            <TableShell>
                <thead>
                    <tr className="border-b border-border">
                        <Th>Segment</Th>
                        <Th right>Daily demand</Th>
                        <Th right>Active</Th>
                        <Th right>Target</Th>
                        <Th>Fill</Th>
                    </tr>
                </thead>
                <tbody>
                    {data.stock.map((s) => {
                        const pct = s.target > 0 ? Math.min(100, Math.round((s.active / s.target) * 100)) : 100;
                        const deficit = Math.max(0, s.target - s.active);
                        return (
                            <tr key={s.segment || "generic"} className={ROW}>
                                <Td>{s.segment || "generic"}</Td>
                                <Td right className="text-muted-foreground">
                                    {s.average_daily_sends.toLocaleString()}
                                </Td>
                                <Td right>{s.active.toLocaleString()}</Td>
                                <Td right className="text-muted-foreground">
                                    {s.target.toLocaleString()}
                                </Td>
                                <Td>
                                    <div className="flex items-center gap-2.5">
                                        <Meter
                                            value={pct}
                                            tone={pct >= 100 ? "success" : pct >= 50 ? "info" : "warning"}
                                            className="w-28"
                                        />
                                        <span className="whitespace-nowrap text-xs tabular-nums text-muted-foreground">
                                            {deficit > 0 ? `${deficit.toLocaleString()} short` : "at target"}
                                        </span>
                                    </div>
                                </Td>
                            </tr>
                        );
                    })}
                </tbody>
            </TableShell>
        </Section>
    );
}

const ROW = "h-10 border-b border-border/70 last:border-0 transition-colors hover:bg-accent/50";

function TableShell({ children }: { children: ReactNode }) {
    return (
        <div className="overflow-hidden surface-lit rounded-xl border border-border bg-card">
            <div className="overflow-x-auto">
                <table className="w-full border-collapse text-[13px]">{children}</table>
            </div>
        </div>
    );
}

function EmptyRow({ cols, title, hint }: { cols: number; title: string; hint?: string }) {
    return (
        <tr>
            <td colSpan={cols}>
                <EmptyState title={title} hint={hint} className="py-10" />
            </td>
        </tr>
    );
}

export default function OverviewPage() {
    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup-content", "overview"],
        queryFn: getWarmupContentOverview,
        refetchInterval: 30_000,
    });

    const ab = useQuery({
        queryKey: ["admin", "warmup-content", "ab", 14],
        queryFn: () => getWarmupContentAb(14),
        staleTime: 60_000,
    });

    // Content is one shared library now; pools only isolate mailbox
    // reputation, not content. Aggregate the per-pool breakdown by
    // segment+source so the table reflects the actual library shape rather
    // than misleading per-pool rows (e.g. "free has no content").
    const bySegmentSource = useMemo(() => {
        const acc = new Map<
            string,
            { segment: string; source: string; active: number; archived: number }
        >();
        for (const p of data?.by_pool ?? []) {
            const key = `${p.segment}::${p.source}`;
            const cur = acc.get(key);
            if (cur) {
                cur.active += p.active;
                cur.archived += p.archived;
            } else {
                acc.set(key, {
                    segment: p.segment,
                    source: p.source,
                    active: p.active,
                    archived: p.archived,
                });
            }
        }
        return Array.from(acc.values());
    }, [data?.by_pool]);

    if (isLoading) {
        return (
            <div className="space-y-8">
                <Skeleton className="h-56 rounded-lg" />
                <Skeleton className="h-24 rounded-lg" />
                <Skeleton className="h-40 rounded-lg" />
            </div>
        );
    }
    if (error) {
        return <ErrorState error={error} title="Failed to load overview" onRetry={() => refetch()} />;
    }
    if (!data) return null;

    const abRows = ab.data?.data ?? [];

    return (
        <div>
            <Section>
                <AutomationPanel data={data} />
            </Section>

            <Section><GenerationSettingsPanel data={data} /></Section>

            <Section>
                <StatGrid className="grid-cols-1 sm:grid-cols-3 md:grid-cols-3">
                    <Stat
                        icon={Inbox}
                        label="Active threads"
                        value={(data.total_active ?? 0).toLocaleString()}
                        sub="available to warmup sends"
                    />
                    <Stat
                        icon={Archive}
                        label="Archived"
                        value={(data.total_archived ?? 0).toLocaleString()}
                        sub="retired from rotation"
                    />
                    <Stat
                        icon={Play}
                        label="Last generated"
                        value={data.last_generated_at ? new Date(data.last_generated_at).toLocaleDateString() : "Never"}
                        sub={data.last_generated_at ? fmtDate(data.last_generated_at) : "no jobs yet"}
                    />
                </StatGrid>
            </Section>

            <StockTable data={data} />

            <Section title="Library by segment & source">
                <TableShell>
                    <thead>
                        <tr className="border-b border-border">
                            <Th>Segment</Th>
                            <Th>Source</Th>
                            <Th right>Active</Th>
                            <Th right>Archived</Th>
                        </tr>
                    </thead>
                    <tbody>
                        {bySegmentSource.map((p, i) => (
                            <tr key={`${p.segment}-${p.source}-${i}`} className={ROW}>
                                <Td>{p.segment || "—"}</Td>
                                <Td className="text-muted-foreground">{p.source || "—"}</Td>
                                <Td right className={TONE_TEXT.success}>
                                    {p.active.toLocaleString()}
                                </Td>
                                <Td right className="text-muted-foreground">
                                    {p.archived.toLocaleString()}
                                </Td>
                            </tr>
                        ))}
                        {bySegmentSource.length === 0 && (
                            <EmptyRow
                                cols={4}
                                title="No generated content yet"
                                hint="The controller will submit a batch automatically; static content is active meanwhile."
                            />
                        )}
                    </tbody>
                </TableShell>
            </Section>

            <Section
                title={
                    <span className="inline-flex items-baseline gap-2">
                        Content source vs spam placement
                        {ab.data ? (
                            <span className="text-xs font-normal text-muted-foreground">
                                last {ab.data.window_days} days
                            </span>
                        ) : null}
                    </span>
                }
                description="Compares how often generated and static warmup mail lands in spam. The library remains reviewable so unsafe generated content can be archived."
            >
                {ab.error ? (
                    <ErrorState error={ab.error} title="Failed to load A/B comparison" onRetry={() => ab.refetch()} />
                ) : ab.isLoading ? (
                    <Skeleton className="h-24 rounded-lg" />
                ) : (
                    <TableShell>
                        <thead>
                            <tr className="border-b border-border">
                                <Th>Source</Th>
                                <Th right>Sent</Th>
                                <Th right>Spam placements</Th>
                                <Th right>Placement rate</Th>
                            </tr>
                        </thead>
                        <tbody>
                            {abRows.map((r) => {
                                // Backend already returns a percent (it multiplies by 100).
                                const pct = r.spam_placement_rate ?? 0;
                                const tone: Tone = pct >= 20 ? "danger" : pct >= 10 ? "warning" : "success";
                                return (
                                    <tr key={r.content_source} className={ROW}>
                                        <Td>{r.content_source}</Td>
                                        <Td right>{r.sent.toLocaleString()}</Td>
                                        <Td right>{r.spam_placements.toLocaleString()}</Td>
                                        <Td right className={cn("font-medium", TONE_TEXT[tone])}>
                                            {pct.toFixed(2)}%
                                        </Td>
                                    </tr>
                                );
                            })}
                            {abRows.length === 0 && <EmptyRow cols={4} title="Not enough delivery data yet." />}
                        </tbody>
                    </TableShell>
                )}
            </Section>
        </div>
    );
}
