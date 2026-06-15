// The step's email editor with its A/B arms. The Original (the step's own
// email, the control) and every variant are first-class siblings, split by
// weight at send time (see internal/app/advanced SelectVariant). The traffic
// split — the Original's own share included — is one compact bar at the top:
// drag a divider or click a segment to edit that arm below. The Original's
// weight is persisted as an is_control variant row, created lazily the first
// time you move its share off the default. With no variants yet, the composer
// shows an inline "A/B test" entry in its header.

import React from "react";
import { Loader2Icon, Trash2Icon, TrophyIcon, PauseIcon, PlayIcon, SplitIcon } from "lucide-react";
import toast from "react-hot-toast";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import type ABVariant from "@/lib/api/models/app/campaigns/ABVariant";
import type { ABVariantStats } from "@/lib/api/models/app/campaigns/ABVariant";
import { Label, TextInput } from "@/components/ui/field";
import EmailContentEditor from "./EmailContentEditor";
import { htmlToPlain } from "./emailPreview";
import SequenceView from "./SequenceView";
import StepSplitAllocator, { type SplitArm } from "./StepSplitAllocator";
import {
    useCampaignABVariants,
    useCampaignABAnalysis,
    useCreateABVariant,
    useUpdateABVariant,
    useDeleteABVariant,
} from "@/lib/api/hooks/app/campaigns/useCampaignABVariants";
import { useConfirm } from "@/hooks/context/confirm";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

const LETTERS = ["B", "C", "D", "E", "F"];
// Must match abControlWeight in internal/app/advanced/service.go.
const CONTROL_WEIGHT = 100;
const MAX_VARIANTS = 5;

const clampW = (w: number) => Math.min(100, Math.max(1, Math.round(w)));
const err = (e: unknown) => toast.error(buildError(e as unknown as AppError));

export default function StepEmailArms({
    campaignId,
    sequence,
    index,
}: {
    campaignId: string;
    sequence: Sequence;
    index: number;
}) {
    const { data: all } = useCampaignABVariants(campaignId);
    const stepRows = (all ?? []).filter((v) => v.step_id === sequence.id);
    const controlRow = stepRows.find((v) => v.is_control) ?? null;
    const variants = stepRows.filter((v) => !v.is_control);

    const create = useCreateABVariant(campaignId);
    const update = useUpdateABVariant(campaignId);
    const del = useDeleteABVariant(campaignId);
    const confirm = useConfirm();
    const busy = create.isPending || update.isPending || del.isPending;

    const { data: analysis } = useCampaignABAnalysis(campaignId, (all ?? []).length > 0);
    const statsById = React.useMemo(() => {
        const m = new Map<string, ABVariantStats>();
        for (const s of analysis?.variants ?? []) m.set(s.variant_id, s);
        return m;
    }, [analysis]);
    const winnerId = analysis?.winner_id ?? null;

    const [selected, setSelected] = React.useState<string>("original");
    React.useEffect(() => {
        if (selected !== "original" && !variants.some((v) => v.id === selected)) {
            setSelected("original");
        }
    }, [variants, selected]);

    const originalWeight = controlRow ? controlRow.weight : CONTROL_WEIGHT;
    const arms: SplitArm[] = [
        { key: "original", name: "Original", weight: originalWeight, active: true, isOriginal: true },
        ...variants.map((v, i) => ({
            key: v.id,
            name: v.name || `Variant ${LETTERS[i] ?? i + 1}`,
            weight: v.weight,
            active: v.is_active,
            isOriginal: false,
            winner: winnerId === v.id,
        })),
    ];

    // Approximate share of the active split, for the editor chip.
    const activeWeightSum = arms.filter((a) => a.active).reduce((s, a) => s + Math.max(a.weight, 1), 0);
    const shareOf = (w: number) => (activeWeightSum > 0 ? Math.round((Math.max(w, 1) / activeWeightSum) * 100) : 0);

    // Persist a new split: the Original maps to its control row (created lazily),
    // each variant to its own weight. Only changed arms are written.
    const commitWeights = (next: Record<string, number>) => {
        const tasks: Promise<unknown>[] = [];
        if (next.original != null) {
            const ow = clampW(next.original);
            if (controlRow) {
                if (controlRow.weight !== ow) {
                    tasks.push(update.mutateAsync({ variantId: controlRow.id, input: { weight: ow } }));
                }
            } else if (ow !== CONTROL_WEIGHT) {
                tasks.push(
                    create.mutateAsync({
                        name: "Original",
                        step_id: sequence.id,
                        weight: ow,
                        is_control: true,
                        is_active: true,
                    }),
                );
            }
        }
        for (const v of variants) {
            const raw = next[v.id];
            if (raw == null) continue;
            const w = clampW(raw);
            if (w !== v.weight) tasks.push(update.mutateAsync({ variantId: v.id, input: { weight: w } }));
        }
        if (tasks.length) Promise.all(tasks).catch(err);
    };

    const evenSplit = () => {
        const keys = ["original", ...variants.filter((v) => v.is_active).map((v) => v.id)];
        const n = keys.length;
        if (n < 2) return;
        const base = Math.floor(100 / n);
        const rem = 100 - base * n;
        const next: Record<string, number> = {};
        keys.forEach((k, i) => (next[k] = base + (i < rem ? 1 : 0)));
        commitWeights(next);
    };

    const togglePause = (variantId: string, active: boolean) => {
        update.mutate({ variantId, input: { is_active: active } }, { onError: err });
    };

    const deleteArm = (variantId: string) => {
        const v = variants.find((x) => x.id === variantId);
        if (!v) return;
        confirm.show(`Delete "${v.name}"? Its content will be removed from this step.`, async () => {
            await del.mutateAsync(v.id);
            // Removing the last variant reverts the step to a single arm: drop the
            // control row too so the step stops A/B splitting entirely.
            if (variants.length === 1 && controlRow) {
                try {
                    await del.mutateAsync(controlRow.id);
                } catch {
                    /* harmless: a lone control row still sends the original */
                }
            }
            if (selected === v.id) setSelected("original");
            toast.success("Variant removed.");
        });
    };

    const addVariant = async () => {
        try {
            const v = await create.mutateAsync({
                name: `Variant ${LETTERS[variants.length] ?? variants.length + 1}`,
                step_id: sequence.id,
                weight: CONTROL_WEIGHT,
                is_active: true,
                subject: sequence.subject,
                body_html: sequence.body_html,
                body_plain: htmlToPlain(sequence.body_html ?? ""),
            });
            if (v?.id) setSelected(v.id);
        } catch (e) {
            err(e);
        }
    };

    const selectedVariant = variants.find((v) => v.id === selected) ?? null;

    return (
        <div className="rounded-md border border-slate-200 bg-white">
            {variants.length > 0 && (
                <StepSplitAllocator
                    arms={arms}
                    selectedKey={selected}
                    onSelect={setSelected}
                    onCommit={commitWeights}
                    onAdd={addVariant}
                    onEven={evenSplit}
                    canAdd={variants.length < MAX_VARIANTS}
                    adding={create.isPending}
                    busy={busy}
                />
            )}
            {selected === "original" || !selectedVariant ? (
                <SequenceView
                    embedded
                    campaignId={campaignId}
                    sequence={sequence}
                    index={index}
                    headerExtra={
                        variants.length === 0 ? (
                            <button
                                type="button"
                                onClick={addVariant}
                                disabled={create.isPending}
                                className="h-7 px-2.5 inline-flex items-center gap-1.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-600 transition-colors hover:border-sky-300 hover:bg-sky-50 hover:text-sky-700 disabled:opacity-50"
                            >
                                {create.isPending ? (
                                    <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                                ) : (
                                    <SplitIcon className="w-3.5 h-3.5" />
                                )}
                                A/B test
                            </button>
                        ) : undefined
                    }
                />
            ) : (
                <VariantEditor
                    key={selectedVariant.id}
                    campaignId={campaignId}
                    variant={selectedVariant}
                    stats={statsById.get(selectedVariant.id)}
                    isWinner={winnerId === selectedVariant.id}
                    sharePct={selectedVariant.is_active ? shareOf(selectedVariant.weight) : 0}
                    onTogglePause={(active) => togglePause(selectedVariant.id, active)}
                    onDelete={() => deleteArm(selectedVariant.id)}
                />
            )}
        </div>
    );
}

function VariantEditor({
    campaignId,
    variant,
    stats,
    isWinner,
    sharePct,
    onTogglePause,
    onDelete,
}: {
    campaignId: string;
    variant: ABVariant;
    stats?: ABVariantStats;
    isWinner?: boolean;
    sharePct: number;
    onTogglePause: (active: boolean) => void;
    onDelete: () => void;
}) {
    const update = useUpdateABVariant(campaignId);

    const [name, setName] = React.useState(variant.name);
    const [subject, setSubject] = React.useState(variant.subject);
    const [bodyHtml, setBodyHtml] = React.useState(variant.body_html);

    React.useEffect(() => {
        setName(variant.name);
        setSubject(variant.subject);
        setBodyHtml(variant.body_html);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [variant.id, variant.updated_at]);

    const dirty = name !== variant.name || subject !== variant.subject || bodyHtml !== variant.body_html;

    const save = () => {
        update.mutate(
            { variantId: variant.id, input: { name, subject, body_html: bodyHtml, body_plain: htmlToPlain(bodyHtml) } },
            {
                onSuccess: () => toast.success("Variant saved."),
                onError: err,
            },
        );
    };

    return (
        <div className={`space-y-3 p-3 ${isWinner ? "bg-amber-50/30" : ""}`}>
            <div className="flex items-center gap-2">
                <div className="min-w-0 flex-1">
                    <Label>Variant name</Label>
                    <TextInput value={name} onChange={setName} placeholder="Variant B" />
                </div>
                <span
                    className={`mt-4 h-7 shrink-0 inline-flex items-center rounded-md px-2 text-[11px] font-medium tabular-nums ${
                        variant.is_active ? "bg-sky-50 text-sky-700" : "bg-slate-100 text-slate-400"
                    }`}
                >
                    {variant.is_active ? `${sharePct}% of contacts` : "Paused"}
                </span>
                <button
                    type="button"
                    onClick={() => onTogglePause(!variant.is_active)}
                    title={variant.is_active ? "Pause this variant" : "Resume this variant"}
                    className="mt-4 size-7 shrink-0 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-slate-700 transition-colors"
                >
                    {variant.is_active ? <PauseIcon className="w-3.5 h-3.5" /> : <PlayIcon className="w-3.5 h-3.5" />}
                </button>
                <button
                    type="button"
                    onClick={onDelete}
                    title="Delete variant"
                    className="mt-4 size-7 shrink-0 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-rose-50 hover:text-rose-600 transition-colors"
                >
                    <Trash2Icon className="w-3.5 h-3.5" />
                </button>
                <button
                    type="button"
                    onClick={save}
                    disabled={!dirty || update.isPending}
                    className="mt-4 h-7 shrink-0 px-3 rounded-md bg-sky-600 text-[12px] font-medium text-white hover:bg-sky-700 inline-flex items-center gap-1.5 disabled:opacity-40"
                >
                    {update.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                    Save
                </button>
            </div>

            {stats && stats.total_sent > 0 && (
                <div className="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-md bg-slate-50 px-2.5 py-1.5 text-[11px]">
                    {isWinner && (
                        <span className="inline-flex items-center gap-1 text-amber-700 font-medium">
                            <TrophyIcon className="w-3 h-3" /> Winner
                        </span>
                    )}
                    <Metric label="Sent" value={stats.total_sent.toLocaleString()} />
                    <Metric label="Open" value={`${stats.open_rate.toFixed(1)}%`} tone="text-emerald-600" />
                    <Metric label="Reply" value={`${stats.reply_rate.toFixed(1)}%`} tone="text-sky-600" />
                    <Metric label="Bounce" value={`${stats.bounce_rate.toFixed(1)}%`} tone="text-rose-600" />
                </div>
            )}

            <EmailContentEditor
                subject={subject}
                onSubjectChange={setSubject}
                bodyHtml={bodyHtml}
                onBodyChange={(html) => setBodyHtml(html)}
                subjectPlaceholder="Leave blank to reuse the step's subject"
                bodyPlaceholder="Leave blank to reuse the step's body"
            />
        </div>
    );
}

function Metric({ label, value, tone }: { label: string; value: string; tone?: string }) {
    return (
        <span className="inline-flex items-center gap-1 tabular-nums">
            <span className="text-slate-400">{label}</span>
            <span className={`font-medium ${tone ?? "text-slate-700"}`}>{value}</span>
        </span>
    );
}
