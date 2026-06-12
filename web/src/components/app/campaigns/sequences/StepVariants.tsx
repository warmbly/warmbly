// Per-step A/B variants editor, shown directly under the step's email composer.
// The step's own email is the CONTROL arm: when A/B testing is on, each contact
// is split across the original plus the active variants by weight (the backend
// includes the original as a control arm with weight 100). This editor surfaces
// all the arms together so it is obvious which email a contact can receive.

import React from "react";
import { PlusIcon, Loader2Icon, Trash2Icon, FlaskConicalIcon, TrophyIcon } from "lucide-react";
import toast from "react-hot-toast";
import type ABVariant from "@/lib/api/models/app/campaigns/ABVariant";
import type { ABVariantStats } from "@/lib/api/models/app/campaigns/ABVariant";
import { Label, TextInput, NumberInput } from "@/components/ui/field";
import { Toggle } from "../preferences/components/CampaignPreferenceBoolBox";
import RichTextEditor, { VariableMenu } from "./RichTextEditor";
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

const VARIABLES = ["{{.FirstName}}", "{{.LastName}}", "{{.Email}}", "{{.Company}}", "{{.Phone}}"];
const LETTERS = ["B", "C", "D", "E", "F"];
// Must match abControlWeight in internal/app/advanced/service.go.
const CONTROL_WEIGHT = 100;

function htmlToPlain(html: string): string {
    const withBreaks = html
        .replace(/<\s*br\s*\/?>/gi, "\n")
        .replace(/<\/\s*(p|div|h[1-6]|li|tr)\s*>/gi, "\n");
    if (typeof document === "undefined") return withBreaks.replace(/<[^>]+>/g, "");
    const tmp = document.createElement("div");
    tmp.innerHTML = withBreaks;
    return (tmp.textContent || "").replace(/\n{3,}/g, "\n\n").trim();
}

export default function StepVariants({
    campaignId,
    sequenceId,
    baseSubject,
    baseBodyHtml,
}: {
    campaignId: string;
    sequenceId: string;
    baseSubject: string;
    baseBodyHtml: string;
}) {
    const { data: all, isLoading } = useCampaignABVariants(campaignId);
    const create = useCreateABVariant(campaignId);
    const variants = (all ?? []).filter((v) => v.sequence_id === sequenceId);

    // Per-variant performance + the campaign winner (only fetched once variants exist).
    const { data: analysis } = useCampaignABAnalysis(campaignId, (all ?? []).length > 0);
    const statsById = React.useMemo(() => {
        const m = new Map<string, ABVariantStats>();
        for (const s of analysis?.variants ?? []) m.set(s.variant_id, s);
        return m;
    }, [analysis]);
    const winnerId = analysis?.winner_id ?? null;
    const stepHasWinner = !!winnerId && variants.some((v) => v.id === winnerId);

    // The split: the original (control) plus every ACTIVE variant, by weight.
    const wOf = (v: ABVariant) => (v.weight > 0 ? v.weight : CONTROL_WEIGHT);
    const totalWeight =
        CONTROL_WEIGHT + variants.filter((v) => v.is_active).reduce((s, v) => s + wOf(v), 0);
    const pct = (w: number) => (totalWeight > 0 ? Math.round((w / totalWeight) * 100) : 0);

    const addVariant = () => {
        const name = `Variant ${LETTERS[variants.length] ?? variants.length + 1}`;
        create.mutate(
            {
                name,
                sequence_id: sequenceId,
                weight: CONTROL_WEIGHT,
                is_active: true,
                subject: baseSubject,
                body_html: baseBodyHtml,
                body_plain: htmlToPlain(baseBodyHtml),
            },
            { onError: (e) => toast.error(buildError(e as unknown as AppError)) },
        );
    };

    // No variants yet: a slim, inline affordance under the email, not a section.
    if (!isLoading && variants.length === 0) {
        return (
            <button
                type="button"
                onClick={addVariant}
                disabled={create.isPending}
                className="group flex w-full items-center gap-2 rounded-md border border-dashed border-slate-200 px-3 py-2 text-left hover:border-sky-300 hover:bg-sky-50/40 disabled:opacity-50"
            >
                {create.isPending ? (
                    <Loader2Icon className="w-3.5 h-3.5 animate-spin text-slate-400" />
                ) : (
                    <FlaskConicalIcon className="w-3.5 h-3.5 text-slate-400 group-hover:text-sky-600" />
                )}
                <span className="text-[12px] font-medium text-slate-700 group-hover:text-sky-700">
                    Add an A/B variant
                </span>
                <span className="truncate text-[11px] text-slate-400">
                    test alternate copy, contacts split against this email
                </span>
                <PlusIcon className="ml-auto w-3.5 h-3.5 text-slate-400 group-hover:text-sky-600" />
            </button>
        );
    }

    return (
        <div className="rounded-md border border-slate-200 bg-white">
            <div className="flex items-center justify-between gap-2 border-b border-slate-200/70 px-3 py-2.5">
                <div className="flex items-center gap-2 min-w-0">
                    <FlaskConicalIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                    <div className="min-w-0">
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                            A/B test
                        </div>
                        <p className="truncate text-[11px] text-slate-400">
                            Each contact gets one version, split by weight. The email above is the control.
                        </p>
                    </div>
                </div>
                <button
                    type="button"
                    onClick={addVariant}
                    disabled={create.isPending || variants.length >= 5}
                    className="h-7 px-2.5 inline-flex items-center gap-1.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:border-slate-300 hover:text-slate-900 disabled:opacity-50 shrink-0"
                >
                    {create.isPending ? (
                        <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                    ) : (
                        <PlusIcon className="w-3.5 h-3.5" />
                    )}
                    Add variant
                </button>
            </div>
            {stepHasWinner && (
                <div className="flex items-center gap-1.5 border-b border-amber-200/70 bg-amber-50/60 px-3 py-1.5">
                    <TrophyIcon className="w-3.5 h-3.5 text-amber-600 shrink-0" />
                    <span className="text-[11.5px] text-amber-800">
                        Winner: <span className="font-medium">{analysis?.winner_name || "a variant"}</span>
                        {analysis?.winning_rule ? ` · by ${analysis.winning_rule}` : ""}
                        {analysis?.confidence ? ` · ${analysis.confidence} confidence` : ""}
                    </span>
                </div>
            )}
            {isLoading ? (
                <div className="px-3 py-4 text-[11.5px] text-slate-400">Loading variants…</div>
            ) : (
                <div className="divide-y divide-slate-200/60">
                    {/* The control arm: the step's own email, shown so the split is obvious. */}
                    <div className="flex items-center gap-2.5 px-3 py-2.5">
                        <span className="inline-flex items-center rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-slate-600">
                            Original
                        </span>
                        <span className="text-[11.5px] text-slate-500">
                            The email above, sent as the control.
                        </span>
                        <span className="ml-auto text-[11px] tabular-nums text-slate-500">
                            ~{pct(CONTROL_WEIGHT)}% of contacts
                        </span>
                    </div>
                    {variants.map((v) => (
                        <VariantCard
                            key={v.id}
                            campaignId={campaignId}
                            variant={v}
                            stats={statsById.get(v.id)}
                            isWinner={winnerId === v.id}
                            splitPct={v.is_active ? pct(wOf(v)) : 0}
                        />
                    ))}
                </div>
            )}
        </div>
    );
}

function VariantCard({
    campaignId,
    variant,
    stats,
    isWinner,
    splitPct,
}: {
    campaignId: string;
    variant: ABVariant;
    stats?: ABVariantStats;
    isWinner?: boolean;
    splitPct: number;
}) {
    const update = useUpdateABVariant(campaignId);
    const del = useDeleteABVariant(campaignId);
    const confirm = useConfirm();

    const [name, setName] = React.useState(variant.name);
    const [weight, setWeight] = React.useState(variant.weight);
    const [subject, setSubject] = React.useState(variant.subject);
    const [bodyHtml, setBodyHtml] = React.useState(variant.body_html);
    const [active, setActive] = React.useState(variant.is_active);

    // Re-seed from the canonical record after a save (updated_at changes) or when
    // a different variant renders into this card slot.
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
                input: {
                    name,
                    weight,
                    subject,
                    body_html: bodyHtml,
                    body_plain: htmlToPlain(bodyHtml),
                    is_active: active,
                },
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
        });
    };

    return (
        <div className={`p-3 space-y-3 ${isWinner ? "bg-amber-50/40" : ""}`}>
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
            <div>
                <div className="flex items-center justify-between gap-2 mb-1.5">
                    <Label className="mb-0">Subject</Label>
                    <VariableMenu variables={VARIABLES} onPick={(v) => setSubject(subject + v)} />
                </div>
                <TextInput
                    value={subject}
                    onChange={setSubject}
                    placeholder="Leave blank to reuse the step's subject"
                />
            </div>
            <div>
                <Label>Body</Label>
                <RichTextEditor
                    html={bodyHtml}
                    onChange={setBodyHtml}
                    variables={VARIABLES}
                    placeholder="Leave blank to reuse the step's body"
                />
            </div>
        </div>
    );
}

// Metric — a compact "label value" pair in the variant performance strip.
function Metric({ label, value, tone }: { label: string; value: string; tone?: string }) {
    return (
        <span className="inline-flex items-center gap-1 tabular-nums">
            <span className="text-slate-400">{label}</span>
            <span className={`font-medium ${tone ?? "text-slate-700"}`}>{value}</span>
        </span>
    );
}
