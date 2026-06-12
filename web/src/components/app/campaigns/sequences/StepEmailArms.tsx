// The step's email editor as a tabbed set of arms. The first tab is the
// Original (the step's own email, the control), then one tab per A/B variant,
// then Add. Each tab edits that arm in place with the same composer, so the
// original and the variants are first-class siblings, split by weight at send
// time (see internal/app/advanced SelectVariant). Replaces the old stacked
// "main composer + separate variants box".

import React from "react";
import { motion } from "framer-motion";
import { PlusIcon, Loader2Icon, Trash2Icon, TrophyIcon } from "lucide-react";
import toast from "react-hot-toast";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import type ABVariant from "@/lib/api/models/app/campaigns/ABVariant";
import type { ABVariantStats } from "@/lib/api/models/app/campaigns/ABVariant";
import { Label, TextInput, NumberInput } from "@/components/ui/field";
import { Toggle } from "../preferences/components/CampaignPreferenceBoolBox";
import EmailContentEditor from "./EmailContentEditor";
import { htmlToPlain } from "./emailPreview";
import SequenceView from "./SequenceView";
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
    const variants = (all ?? []).filter((v) => v.step_id === sequence.id);
    const create = useCreateABVariant(campaignId);

    const { data: analysis } = useCampaignABAnalysis(campaignId, (all ?? []).length > 0);
    const statsById = React.useMemo(() => {
        const m = new Map<string, ABVariantStats>();
        for (const s of analysis?.variants ?? []) m.set(s.variant_id, s);
        return m;
    }, [analysis]);
    const winnerId = analysis?.winner_id ?? null;

    // The split: control plus every ACTIVE variant, by weight.
    const wOf = (v: ABVariant) => (v.weight > 0 ? v.weight : CONTROL_WEIGHT);
    const totalWeight =
        CONTROL_WEIGHT + variants.filter((v) => v.is_active).reduce((s, v) => s + wOf(v), 0);
    const pct = (w: number) => (totalWeight > 0 ? Math.round((w / totalWeight) * 100) : 0);

    const [selected, setSelected] = React.useState<string>("original");
    // If the selected variant disappears (deleted here or elsewhere), fall back.
    React.useEffect(() => {
        if (selected !== "original" && !variants.some((v) => v.id === selected)) {
            setSelected("original");
        }
    }, [variants, selected]);

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
            toast.error(buildError(e as unknown as AppError));
        }
    };

    const selectedVariant = variants.find((v) => v.id === selected) ?? null;

    return (
        <div className="rounded-md border border-slate-200 bg-white">
            {/* Arm tabs: Original (control) plus each variant, then Add. */}
            <div className="shrink-0 px-2 flex items-center gap-1 border-b border-slate-200 overflow-x-auto no-scrollbar">
                <ArmTab
                    active={selected === "original"}
                    onClick={() => setSelected("original")}
                    label="Original"
                    hint={variants.length > 0 ? `${pct(CONTROL_WEIGHT)}%` : undefined}
                />
                {variants.map((v, i) => (
                    <ArmTab
                        key={v.id}
                        active={selected === v.id}
                        onClick={() => setSelected(v.id)}
                        label={v.name || `Variant ${LETTERS[i] ?? i + 1}`}
                        hint={v.is_active ? `${pct(wOf(v))}%` : "off"}
                        winner={winnerId === v.id}
                    />
                ))}
                <button
                    type="button"
                    onClick={addVariant}
                    disabled={create.isPending || variants.length >= 5}
                    title="Add an A/B variant"
                    className="ml-1 h-7 px-2 inline-flex items-center gap-1 rounded-md text-[12px] font-medium text-slate-500 hover:bg-slate-100 hover:text-slate-900 disabled:opacity-50 shrink-0"
                >
                    {create.isPending ? (
                        <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                    ) : (
                        <PlusIcon className="w-3.5 h-3.5" />
                    )}
                    Add
                </button>
            </div>

            {selected === "original" ? (
                <div>
                    <SequenceView embedded campaignId={campaignId} sequence={sequence} index={index} />
                    {variants.length > 0 && (
                        <p className="px-3 pb-3 text-[11px] text-slate-400">
                            This is the control arm. About {pct(CONTROL_WEIGHT)}% of contacts get it, the rest split across the variants.
                        </p>
                    )}
                </div>
            ) : selectedVariant ? (
                <VariantEditor
                    key={selectedVariant.id}
                    campaignId={campaignId}
                    variant={selectedVariant}
                    stats={statsById.get(selectedVariant.id)}
                    isWinner={winnerId === selectedVariant.id}
                    splitPct={selectedVariant.is_active ? pct(wOf(selectedVariant)) : 0}
                    onDeleted={() => setSelected("original")}
                />
            ) : null}
        </div>
    );
}

function ArmTab({
    active,
    onClick,
    label,
    hint,
    winner,
}: {
    active: boolean;
    onClick: () => void;
    label: string;
    hint?: string;
    winner?: boolean;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            className={`relative h-10 px-2.5 inline-flex items-center gap-1.5 whitespace-nowrap text-[12.5px] transition-colors ${
                active ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-800"
            }`}
        >
            {winner && <TrophyIcon className="w-3 h-3 text-amber-500" />}
            {label}
            {hint && <span className="text-[10.5px] tabular-nums text-slate-400">{hint}</span>}
            {active && (
                <motion.span
                    layoutId="arm-tab-underline"
                    className="absolute left-1.5 right-1.5 -bottom-px h-0.5 rounded-full bg-sky-600"
                    transition={{ type: "spring", duration: 0.3, bounce: 0.15 }}
                />
            )}
        </button>
    );
}

function VariantEditor({
    campaignId,
    variant,
    stats,
    isWinner,
    splitPct,
    onDeleted,
}: {
    campaignId: string;
    variant: ABVariant;
    stats?: ABVariantStats;
    isWinner?: boolean;
    splitPct: number;
    onDeleted: () => void;
}) {
    const update = useUpdateABVariant(campaignId);
    const del = useDeleteABVariant(campaignId);
    const confirm = useConfirm();

    const [name, setName] = React.useState(variant.name);
    const [weight, setWeight] = React.useState(variant.weight);
    const [subject, setSubject] = React.useState(variant.subject);
    const [bodyHtml, setBodyHtml] = React.useState(variant.body_html);
    const [active, setActive] = React.useState(variant.is_active);

    React.useEffect(() => {
        setName(variant.name);
        setWeight(variant.weight);
        setSubject(variant.subject);
        setBodyHtml(variant.body_html);
        setActive(variant.is_active);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [variant.id, variant.updated_at]);

    const dirty =
        name !== variant.name ||
        weight !== variant.weight ||
        subject !== variant.subject ||
        bodyHtml !== variant.body_html ||
        active !== variant.is_active;

    const save = () => {
        update.mutate(
            {
                variantId: variant.id,
                input: { name, weight, subject, body_html: bodyHtml, body_plain: htmlToPlain(bodyHtml), is_active: active },
            },
            {
                onSuccess: () => toast.success("Variant saved."),
                onError: (e) => toast.error(buildError(e as unknown as AppError)),
            },
        );
    };

    const remove = () => {
        confirm.show(`Delete "${variant.name}"? Its content will be removed from this step.`, async () => {
            await del.mutateAsync(variant.id);
            toast.success("Variant removed.");
            onDeleted();
        });
    };

    return (
        <div className={`space-y-3 p-3 ${isWinner ? "bg-amber-50/30" : ""}`}>
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
            <div className="flex flex-wrap items-end gap-3">
                <div className="w-[160px]">
                    <Label>Variant name</Label>
                    <TextInput value={name} onChange={setName} placeholder="Variant B" />
                </div>
                <div>
                    <Label>Weight</Label>
                    <NumberInput value={weight} onChange={setWeight} min={1} max={100} className="w-28" />
                </div>
                <span className="h-7 inline-flex items-center text-[11px] tabular-nums text-slate-400">
                    {active ? `~${splitPct}% of contacts` : "paused"}
                </span>
                <label className="inline-flex items-center gap-2 h-7 text-[12px] text-slate-600 select-none">
                    <Toggle id={`var-active-${variant.id}`} value={active} onChange={setActive} />
                    Active
                </label>
                <div className="ml-auto flex items-center gap-2">
                    <button
                        type="button"
                        onClick={remove}
                        title="Delete variant"
                        className="size-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:text-rose-600 hover:bg-rose-50 transition-colors"
                    >
                        <Trash2Icon className="w-3.5 h-3.5" />
                    </button>
                    <button
                        type="button"
                        onClick={save}
                        disabled={!dirty || update.isPending}
                        className="h-7 px-3 rounded-md bg-sky-600 text-[12px] font-medium text-white hover:bg-sky-700 inline-flex items-center gap-1.5 disabled:opacity-40"
                    >
                        {update.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        Save
                    </button>
                </div>
            </div>
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
