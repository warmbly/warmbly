// Per-step A/B variants editor (lives under the Step composer). Lists the
// variants scoped to this step; each variant has its own name, weight, active
// toggle, subject, and rich body. The step's own content is the implicit
// "original"; sends split across the original + active variants by weight.

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

    const addVariant = () => {
        const name = `Variant ${LETTERS[variants.length] ?? variants.length + 1}`;
        create.mutate(
            {
                name,
                sequence_id: sequenceId,
                weight: 100,
                is_active: true,
                subject: baseSubject,
                body_html: baseBodyHtml,
                body_plain: htmlToPlain(baseBodyHtml),
            },
            { onError: (e) => toast.error(buildError(e as unknown as AppError)) },
        );
    };

    return (
        <div className="rounded-md border border-slate-200 bg-white">
            <div className="flex items-center justify-between gap-2 border-b border-slate-200/70 px-3 py-2.5">
                <div className="flex items-center gap-2 min-w-0">
                    <FlaskConicalIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                    <div className="min-w-0">
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                            A/B variants
                        </div>
                        <p className="truncate text-[11px] text-slate-400">
                            {variants.length === 0
                                ? "Test alternate copy for this step — volume splits by weight."
                                : `${variants.length} variant${variants.length === 1 ? "" : "s"} + the original, split by weight.`}
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
            ) : variants.length === 0 ? (
                <div className="px-3 py-4 text-[11.5px] text-slate-400">
                    No variants yet. The step&apos;s main content is sent to everyone until you add one.
                </div>
            ) : (
                <div className="divide-y divide-slate-200/60">
                    {variants.map((v) => (
                        <VariantCard
                            key={v.id}
                            campaignId={campaignId}
                            variant={v}
                            stats={statsById.get(v.id)}
                            isWinner={winnerId === v.id}
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
}: {
    campaignId: string;
    variant: ABVariant;
    stats?: ABVariantStats;
    isWinner?: boolean;
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
