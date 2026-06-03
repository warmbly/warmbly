// /warmup-content/settings — full editor for WarmupGenerationSettings:
// master toggles, generation cadence/model/caps, per-pool targets, and the
// engagement-simulation knobs.

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { FlaskConical } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorState } from "@/components/ErrorState";
import {
    getWarmupGenerationSettings,
    updateWarmupGenerationSettings,
    type WarmupGenerationPoolConfig,
    type WarmupGenerationSettings,
} from "@/lib/api/client/admin/warmupContent";

function NumberField({
    id,
    label,
    hint,
    value,
    onChange,
    min,
    max,
    step,
}: {
    id: string;
    label: string;
    hint?: string;
    value: number;
    onChange: (v: number) => void;
    min?: number;
    max?: number;
    step?: number;
}) {
    return (
        <div>
            <Label htmlFor={id} className="mb-1 text-xs">
                {label}
            </Label>
            <Input
                id={id}
                type="number"
                min={min}
                max={max}
                step={step}
                value={value}
                onChange={(e) => onChange(Number(e.target.value))}
            />
            {hint && <p className="mt-1 text-[11px] text-muted-foreground">{hint}</p>}
        </div>
    );
}

function ToggleRow({
    label,
    hint,
    checked,
    onChange,
}: {
    label: string;
    hint?: string;
    checked: boolean;
    onChange: (v: boolean) => void;
}) {
    return (
        <label className="flex items-start justify-between gap-3 rounded-md border border-border bg-card px-3 py-2.5">
            <div className="min-w-0">
                <div className="text-sm font-medium">{label}</div>
                {hint && (
                    <div className="mt-0.5 text-[11px] text-muted-foreground">{hint}</div>
                )}
            </div>
            <Switch checked={checked} onCheckedChange={onChange} />
        </label>
    );
}

export default function SettingsPage() {
    const qc = useQueryClient();
    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup-content", "settings"],
        queryFn: getWarmupGenerationSettings,
    });

    const [form, setForm] = useState<WarmupGenerationSettings | null>(null);

    useEffect(() => {
        if (data?.data) setForm(data.data);
    }, [data]);

    const save = useMutation({
        mutationFn: (body: WarmupGenerationSettings) =>
            updateWarmupGenerationSettings(body),
        onSuccess: () => {
            toast.success("Settings saved");
            qc.invalidateQueries({ queryKey: ["admin", "warmup-content"] });
        },
        onError: (err: Error) => toast.error(err.message || "Failed to save settings"),
    });

    if (isLoading || !form) {
        if (error) {
            return (
                <ErrorState
                    error={error}
                    title="Failed to load settings"
                    onRetry={() => refetch()}
                />
            );
        }
        return (
            <div className="space-y-3">
                <Skeleton className="h-16" />
                <Skeleton className="h-40" />
                <Skeleton className="h-40" />
            </div>
        );
    }

    function patch(p: Partial<WarmupGenerationSettings>) {
        setForm((f) => (f ? { ...f, ...p } : f));
    }
    function patchEngagement(p: Partial<WarmupGenerationSettings["engagement"]>) {
        setForm((f) => (f ? { ...f, engagement: { ...f.engagement, ...p } } : f));
    }
    // Warmup content is a single shared library — free/premium pools only
    // isolate mailbox reputation, not content. We persist that library as a
    // single `pools` entry (pool_type "premium") so the scheduler still tops
    // up the shared library. `library` reads that entry (with sane defaults);
    // `patchLibrary` writes it back, collapsing any legacy multi-pool array.
    const library: WarmupGenerationPoolConfig = form.pools[0] ?? {
        pool_type: "premium",
        enabled: true,
        target_active_threads: 0,
        segments: [],
    };
    function patchLibrary(p: Partial<WarmupGenerationPoolConfig>) {
        setForm((f) => {
            if (!f) return f;
            const next: WarmupGenerationPoolConfig = {
                ...(f.pools[0] ?? {
                    pool_type: "premium",
                    enabled: true,
                    target_active_threads: 0,
                    segments: [],
                }),
                ...p,
                pool_type: "premium",
            };
            return { ...f, pools: [next] };
        });
    }

    return (
        <div className="space-y-6">
            <section className="space-y-2">
                <h2 className="text-sm font-semibold">Master controls</h2>
                <div className="grid gap-2 sm:grid-cols-2">
                    <ToggleRow
                        label="AI generation enabled"
                        hint="Master switch for the offline generator. When off, no new content is produced."
                        checked={form.enabled}
                        onChange={(v) => patch({ enabled: v })}
                    />
                    <ToggleRow
                        label="Scheduled generation"
                        hint="Automatically enqueue top-up jobs on a cadence to keep the library stocked."
                        checked={form.schedule_enabled}
                        onChange={(v) => patch({ schedule_enabled: v })}
                    />
                </div>
            </section>

            <section className="space-y-3">
                <h2 className="text-sm font-semibold">Generation</h2>
                <div className="grid gap-4 rounded-lg border border-border bg-card p-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
                    <NumberField
                        id="set-cadence"
                        label="Cadence (hours)"
                        hint="Interval between scheduled top-up jobs."
                        value={form.cadence_hours}
                        min={1}
                        onChange={(v) => patch({ cadence_hours: v })}
                    />
                    <div>
                        <Label htmlFor="set-model" className="mb-1 text-xs">
                            Model
                        </Label>
                        <Input
                            id="set-model"
                            value={form.model}
                            onChange={(e) => patch({ model: e.target.value })}
                        />
                        <p className="mt-1 text-[11px] text-muted-foreground">
                            Default model used for generation.
                        </p>
                    </div>
                    <NumberField
                        id="set-max-msgs"
                        label="Max messages / thread"
                        hint="Upper bound on messages in a generated conversation."
                        value={form.max_messages_per_thread}
                        min={1}
                        onChange={(v) => patch({ max_messages_per_thread: v })}
                    />
                    <NumberField
                        id="set-daily-cap"
                        label="Daily generation cap"
                        hint="Max threads generated across all jobs per day."
                        value={form.daily_generation_cap}
                        min={0}
                        onChange={(v) => patch({ daily_generation_cap: v })}
                    />
                    <NumberField
                        id="set-ai-share"
                        label="AI selection share (%)"
                        hint="Share of warmup sends that draw from AI-generated content (0–100)."
                        value={form.ai_selection_share}
                        min={0}
                        max={100}
                        onChange={(v) =>
                            patch({ ai_selection_share: Math.max(0, Math.min(100, v)) })
                        }
                    />
                </div>
            </section>

            <section className="space-y-3">
                <h2 className="text-sm font-semibold">Content library</h2>
                <p className="text-[11px] text-muted-foreground">
                    Free and premium pools only isolate mailbox reputation — warmup
                    content is shared across both, so there's one library.
                </p>
                <div className="space-y-3 rounded-lg border border-border bg-card p-4">
                    <div className="flex items-center justify-between">
                        <span className="text-sm font-medium">Shared library</span>
                        <div className="flex items-center gap-2 text-xs text-muted-foreground">
                            <span>{library.enabled ? "Enabled" : "Disabled"}</span>
                            <Switch
                                checked={library.enabled}
                                onCheckedChange={(v) => patchLibrary({ enabled: v })}
                            />
                        </div>
                    </div>
                    <div className="grid gap-4 sm:grid-cols-2">
                        <NumberField
                            id="library-target"
                            label="Target active threads"
                            hint="Library top-up target the scheduler aims to keep stocked."
                            value={library.target_active_threads}
                            min={0}
                            onChange={(v) => patchLibrary({ target_active_threads: v })}
                        />
                        <div>
                            <Label htmlFor="library-segments" className="mb-1 text-xs">
                                Segments
                            </Label>
                            <Input
                                id="library-segments"
                                placeholder="comma,separated,segments"
                                value={library.segments.join(", ")}
                                onChange={(e) =>
                                    patchLibrary({
                                        segments: e.target.value
                                            .split(",")
                                            .map((s) => s.trim())
                                            .filter(Boolean),
                                    })
                                }
                            />
                            <p className="mt-1 text-[11px] text-muted-foreground">
                                Comma-separated segments to generate content for.
                            </p>
                        </div>
                    </div>
                </div>
            </section>

            <section className="space-y-3">
                <h2 className="text-sm font-semibold">Engagement simulation</h2>
                <p className="text-[12.5px] text-muted-foreground">
                    How recipient mailboxes behave toward warmup mail — rescuing from spam,
                    marking important/read, and dwell time before actions.
                </p>
                <div className="grid gap-4 rounded-lg border border-border bg-card p-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
                    <NumberField
                        id="eng-rescue"
                        label="Spam rescue rate"
                        hint="Fraction of spam-foldered warmup mail that gets rescued (0–1)."
                        value={form.engagement.spam_rescue_rate}
                        min={0}
                        max={1}
                        step={0.01}
                        onChange={(v) => patchEngagement({ spam_rescue_rate: v })}
                    />
                    <NumberField
                        id="eng-important"
                        label="Mark important rate"
                        hint="Fraction marked as important (0–1)."
                        value={form.engagement.mark_important_rate}
                        min={0}
                        max={1}
                        step={0.01}
                        onChange={(v) => patchEngagement({ mark_important_rate: v })}
                    />
                    <NumberField
                        id="eng-read"
                        label="Mark read rate"
                        hint="Fraction opened / marked read (0–1)."
                        value={form.engagement.mark_read_rate}
                        min={0}
                        max={1}
                        step={0.01}
                        onChange={(v) => patchEngagement({ mark_read_rate: v })}
                    />
                    <NumberField
                        id="eng-star"
                        label="Star rate (%)"
                        hint="Share of warmup mail starred / flagged (0–100)."
                        value={form.engagement.star_rate}
                        min={0}
                        max={100}
                        onChange={(v) =>
                            patchEngagement({ star_rate: Math.max(0, Math.min(100, v)) })
                        }
                    />
                    <NumberField
                        id="eng-min-dwell"
                        label="Min dwell (seconds)"
                        hint="Shortest simulated read time before an action."
                        value={form.engagement.min_dwell_seconds}
                        min={0}
                        onChange={(v) => patchEngagement({ min_dwell_seconds: v })}
                    />
                    <NumberField
                        id="eng-max-dwell"
                        label="Max dwell (seconds)"
                        hint="Longest simulated read time before an action."
                        value={form.engagement.max_dwell_seconds}
                        min={0}
                        onChange={(v) => patchEngagement({ max_dwell_seconds: v })}
                    />
                </div>
            </section>

            <div className="sticky bottom-0 flex justify-end gap-2 border-t border-border bg-background/95 py-3 backdrop-blur">
                <Button
                    variant="outline"
                    onClick={() => data?.data && setForm(data.data)}
                    disabled={save.isPending}
                >
                    Reset
                </Button>
                <Button onClick={() => form && save.mutate(form)} disabled={save.isPending}>
                    <FlaskConical className="size-4" />
                    {save.isPending ? "Saving…" : "Save settings"}
                </Button>
            </div>
        </div>
    );
}
