// SegmentEditor: right-side drawer that creates or edits a segment: name,
// color, all/any match and the condition list, with a live "matches N
// contacts" preview fed by POST /segments/preview.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Loader2Icon, PlusIcon, Trash2Icon, XIcon } from "lucide-react";
import toast from "react-hot-toast";

import { Label, NumberInput, TextInput } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { DatePicker } from "@/components/ui/DatePicker";
import { Segmented } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import CategoryPicker from "@/components/app/contacts/CategoryPicker";
import { CampaignMultiPicker, EnumMultiPicker, SegmentMultiPicker } from "./SegmentPickers";
import { useConfirm } from "@/hooks/context/confirm";
import {
    useCreateSegment,
    useSegmentFields,
    useSegmentPreview,
    useUpdateSegment,
} from "@/lib/api/hooks/app/segments";
import type Segment from "@/lib/api/models/app/segments/Segment";
import {
    SEGMENT_OPERATORS,
    VALUELESS_OPERATORS,
    type SegmentCondition,
    type SegmentFieldSpec,
    type SegmentMatch,
} from "@/lib/api/models/app/segments/Segment";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

const COLORS = ["#0284c7", "#7c3aed", "#db2777", "#dc2626", "#ea580c", "#ca8a04", "#16a34a", "#0d9488", "#475569"];

interface Draft {
    name: string;
    description: string;
    color: string;
    match: SegmentMatch;
    conditions: SegmentCondition[];
}

// Starting conditions for a new segment, e.g. saved from the filter panel.
export interface SegmentPreset {
    conditions: SegmentCondition[];
}

function draftFrom(segment?: Segment | null, preset?: SegmentPreset | null): Draft {
    const conditions = segment?.conditions ?? preset?.conditions ?? [];
    return {
        name: segment?.name ?? "",
        description: segment?.description ?? "",
        color: segment?.color ?? COLORS[0],
        match: segment?.match ?? "all",
        conditions: conditions.map((c) => ({ ...c, values: c.values ? [...c.values] : undefined })),
    };
}

function sameDraft(a: Draft, b: Draft): boolean {
    return JSON.stringify(a) === JSON.stringify(b);
}

// A condition the server would accept: a field, an operator, and a value
// whenever the operator wants one.
function complete(c: SegmentCondition): boolean {
    if (!c.field || !c.operator) return false;
    if (VALUELESS_OPERATORS.has(c.operator)) return true;
    if (c.operator === "in" || c.operator === "not_in") return (c.values?.length ?? 0) > 0;
    return (c.value ?? "").trim() !== "";
}

export default function SegmentEditor({
    open,
    onClose,
    segment,
    preset,
    onSaved,
}: {
    open: boolean;
    onClose: () => void;
    // Edit this segment; omit to create a new one.
    segment?: Segment | null;
    // Pre-filled conditions for a new segment; ignored when editing.
    preset?: SegmentPreset | null;
    onSaved?: (segment: Segment) => void;
}) {
    const confirm = useConfirm();
    const fields = useSegmentFields(open);
    const create = useCreateSegment();
    const update = useUpdateSegment();

    const [draft, setDraft] = React.useState<Draft>(() => draftFrom(segment, preset));
    const [initial, setInitial] = React.useState<Draft>(() => draftFrom(segment, preset));
    React.useEffect(() => {
        if (open) {
            const d = draftFrom(segment, preset);
            setDraft(d);
            setInitial(d);
        }
    }, [open, segment, preset]);
    const dirty = !sameDraft(draft, initial);

    // Live count, debounced so typing a value does not fire a query per key.
    const [debounced, setDebounced] = React.useState<Draft | null>(null);
    React.useEffect(() => {
        if (!open) return;
        const t = setTimeout(() => setDebounced(draft), 350);
        return () => clearTimeout(t);
    }, [draft, open]);
    const previewInput = React.useMemo(() => {
        if (!debounced) return null;
        const conds = debounced.conditions.filter(complete);
        return { id: segment?.id, match: debounced.match, conditions: conds };
    }, [debounced, segment?.id]);
    const preview = useSegmentPreview(open ? previewInput : null);

    const busy = create.isPending || update.isPending;
    const requestClose = React.useCallback(() => {
        if (busy) return;
        if (dirty) {
            confirm.show("Discard your changes to this segment?", async () => onClose());
            return;
        }
        onClose();
    }, [busy, dirty, confirm, onClose]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, requestClose]);

    const incomplete = draft.conditions.filter((c) => !complete(c)).length;
    const canSave = draft.name.trim() !== "" && incomplete === 0 && !busy;
    const blocker =
        draft.name.trim() === ""
            ? "Give the segment a name."
            : incomplete > 0
              ? `${incomplete} condition${incomplete === 1 ? " is" : "s are"} missing a value.`
              : null;

    async function save() {
        if (!canSave) return;
        const body = {
            name: draft.name.trim(),
            description: draft.description.trim(),
            color: draft.color,
            match: draft.match,
            conditions: draft.conditions,
        };
        try {
            const saved = segment
                ? await update.mutateAsync({ id: segment.id, data: body })
                : await create.mutateAsync(body);
            toast.success(segment ? "Segment updated" : "Segment created");
            setInitial(draft);
            onSaved?.(saved);
            onClose();
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    function setCondition(i: number, next: SegmentCondition) {
        setDraft((d) => ({ ...d, conditions: d.conditions.map((c, j) => (j === i ? next : c)) }));
    }

    const specs = fields.data ?? [];

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.18 }}
                    onMouseDown={requestClose}
                    className="fixed inset-0 z-[100] flex justify-end bg-slate-900/30 backdrop-blur-[2px]"
                >
                    <motion.aside
                        key="panel"
                        role="dialog"
                        aria-modal="true"
                        aria-label={segment ? "Edit segment" : "New segment"}
                        initial={{ x: "100%" }}
                        animate={{ x: 0 }}
                        exit={{ x: "100%" }}
                        transition={{ type: "spring", stiffness: 300, damping: 32 }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="flex flex-col bg-white w-[560px] max-w-[95%] h-full border-l border-slate-200 shadow-[-8px_0_24px_-12px_rgba(15,23,42,0.12)]"
                    >
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-3 shrink-0">
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                {segment ? "Edit segment" : "New segment"}
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-700 inline-flex items-center gap-1.5">
                                {preview.isFetching ? (
                                    <Loader2Icon className="w-3 h-3 animate-spin text-slate-400" />
                                ) : preview.isError ? (
                                    <span className="text-rose-600">Cannot count</span>
                                ) : (
                                    <>
                                        Matches{" "}
                                        <span className="font-mono tabular-nums text-slate-900">
                                            {(preview.data ?? 0).toLocaleString()}
                                        </span>{" "}
                                        contact{preview.data === 1 ? "" : "s"}
                                    </>
                                )}
                            </span>
                            <button
                                type="button"
                                onClick={requestClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        <div className="flex-1 overflow-y-auto">
                            <div className="px-4 py-4 space-y-4 border-b border-slate-200/60">
                                <div className="flex gap-3">
                                    <div className="flex-1">
                                        <Label>Name</Label>
                                        <TextInput
                                            value={draft.name}
                                            onChange={(v) => setDraft((d) => ({ ...d, name: v }))}
                                            placeholder="Warm leads in fintech"
                                            autoFocus={!segment}
                                            className="w-full"
                                        />
                                    </div>
                                    <div>
                                        <Label>Color</Label>
                                        <div className="flex items-center gap-1 h-7">
                                            {COLORS.map((c) => (
                                                <button
                                                    key={c}
                                                    type="button"
                                                    onClick={() => setDraft((d) => ({ ...d, color: c }))}
                                                    aria-label={`Color ${c}`}
                                                    aria-pressed={draft.color === c}
                                                    className={cn(
                                                        "size-4 rounded-full border-2 transition-transform",
                                                        draft.color === c ? "border-slate-900 scale-110" : "border-transparent hover:scale-110",
                                                    )}
                                                    style={{ backgroundColor: c }}
                                                />
                                            ))}
                                        </div>
                                    </div>
                                </div>
                                <div>
                                    <Label>Description</Label>
                                    <TextInput
                                        value={draft.description}
                                        onChange={(v) => setDraft((d) => ({ ...d, description: v }))}
                                        placeholder="What this audience is for (optional)"
                                        className="w-full"
                                    />
                                </div>
                            </div>

                            <div className="px-4 py-4 space-y-3">
                                <div className="flex items-center gap-2">
                                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Conditions</span>
                                    <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">{draft.conditions.length}</span>
                                    <div className="ml-auto flex items-center gap-2 text-[12px] text-slate-600">
                                        <span>Match</span>
                                        <Segmented<SegmentMatch>
                                            value={draft.match}
                                            onChange={(v) => setDraft((d) => ({ ...d, match: v }))}
                                            options={[
                                                { value: "all", label: "all" },
                                                { value: "any", label: "any" },
                                            ]}
                                        />
                                    </div>
                                </div>

                                {draft.conditions.length === 0 && (
                                    <div className="rounded-md border border-dashed border-slate-200 px-3 py-4 text-center">
                                        <p className="text-[12.5px] text-slate-900 font-medium">No conditions yet</p>
                                        <p className="text-[11.5px] text-slate-400 mt-0.5">
                                            Without conditions the segment only holds contacts you add by hand.
                                        </p>
                                    </div>
                                )}

                                <div className="space-y-2">
                                    {draft.conditions.map((c, i) => (
                                        <ConditionRow
                                            key={i}
                                            index={i}
                                            condition={c}
                                            specs={specs}
                                            match={draft.match}
                                            selfId={segment?.id}
                                            onChange={(next) => setCondition(i, next)}
                                            onRemove={() =>
                                                setDraft((d) => ({ ...d, conditions: d.conditions.filter((_, j) => j !== i) }))
                                            }
                                        />
                                    ))}
                                </div>

                                <button
                                    type="button"
                                    disabled={draft.conditions.length >= 50}
                                    onClick={() =>
                                        setDraft((d) => ({
                                            ...d,
                                            conditions: [...d.conditions, { field: "", operator: "", value: "" }],
                                        }))
                                    }
                                    className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 bg-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                                >
                                    <PlusIcon className="w-3 h-3" />
                                    Add condition
                                </button>
                            </div>
                        </div>

                        <footer className="px-3 h-12 border-t border-slate-200 flex items-center gap-2 shrink-0 bg-slate-50/30">
                            <span className="text-[11px] text-slate-400 min-w-0 truncate">
                                {blocker ?? (segment ? "Changes apply to every list using this segment." : "Membership stays live as contacts change.")}
                            </span>
                            <button
                                type="button"
                                onClick={requestClose}
                                disabled={busy}
                                className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-50"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={save}
                                disabled={!canSave}
                                className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                            >
                                {busy && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                {segment ? "Save segment" : "Create segment"}
                            </button>
                        </footer>
                    </motion.aside>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function ConditionRow({
    index,
    condition,
    specs,
    match,
    selfId,
    onChange,
    onRemove,
}: {
    index: number;
    condition: SegmentCondition;
    specs: SegmentFieldSpec[];
    match: SegmentMatch;
    selfId?: string;
    onChange: (next: SegmentCondition) => void;
    onRemove: () => void;
}) {
    const spec = specs.find((s) => s.field === condition.field);
    const fieldOptions = React.useMemo<SelectOption[]>(
        () => specs.map((s) => ({ value: s.field, label: s.label, group: s.group })),
        [specs],
    );
    const operators = spec ? SEGMENT_OPERATORS[spec.kind] : [];
    const operatorOptions: SelectOption[] = operators.map((o) => ({ value: o.id, label: o.label }));

    function pickField(field: string) {
        const next = specs.find((s) => s.field === field);
        const ops = next ? SEGMENT_OPERATORS[next.kind] : [];
        onChange({ field, operator: ops[0]?.id ?? "", value: "", values: undefined });
    }

    function pickOperator(operator: string) {
        onChange({ ...condition, operator, value: VALUELESS_OPERATORS.has(operator) ? "" : condition.value, values: condition.values });
    }

    return (
        <div className="rounded-md border border-slate-200 bg-white p-2 space-y-1.5">
            <div className="flex items-center gap-1.5">
                <span className="w-8 shrink-0 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                    {index === 0 ? "If" : match === "all" ? "and" : "or"}
                </span>
                <SelectMenu
                    value={condition.field}
                    onChange={pickField}
                    options={fieldOptions}
                    placeholder="Pick a field…"
                    className="min-w-0 flex-1"
                    fullWidth
                    aria-label="Field"
                />
                <SelectMenu
                    value={condition.operator}
                    onChange={pickOperator}
                    options={operatorOptions}
                    placeholder="Operator"
                    disabled={!spec}
                    className="min-w-0 flex-1"
                    fullWidth
                    aria-label="Operator"
                />
                <button
                    type="button"
                    onClick={onRemove}
                    aria-label="Remove condition"
                    className="size-7 shrink-0 rounded-md text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors"
                >
                    <Trash2Icon className="w-3.5 h-3.5" />
                </button>
            </div>
            {spec && !VALUELESS_OPERATORS.has(condition.operator) && (
                <div className="pl-[38px]">
                    <ValueInput spec={spec} condition={condition} selfId={selfId} onChange={onChange} />
                </div>
            )}
        </div>
    );
}

function ValueInput({
    spec,
    condition,
    selfId,
    onChange,
}: {
    spec: SegmentFieldSpec;
    condition: SegmentCondition;
    selfId?: string;
    onChange: (next: SegmentCondition) => void;
}) {
    const values = condition.values ?? [];
    const setValues = (next: string[]) => onChange({ ...condition, values: next });
    const setValue = (next: string) => onChange({ ...condition, value: next });
    switch (spec.kind) {
        case "text":
            return <TextInput value={condition.value ?? ""} onChange={setValue} placeholder="Value" className="w-full" />;
        case "number":
            return (
                <NumberInput
                    value={Number(condition.value ?? 0) || 0}
                    onChange={(n) => setValue(String(Math.max(0, Math.round(n))))}
                    min={0}
                    className="w-40"
                />
            );
        case "date":
            if (condition.operator === "within_days" || condition.operator === "not_within_days") {
                return (
                    <NumberInput
                        value={Number(condition.value ?? 0) || 0}
                        onChange={(n) => setValue(String(Math.min(3650, Math.max(1, Math.round(n)))))}
                        min={1}
                        max={3650}
                        suffix="days"
                        className="w-40"
                    />
                );
            }
            return (
                <DatePicker
                    value={(condition.value ?? "").slice(0, 10)}
                    onChange={setValue}
                    clearable={false}
                    placeholder="Pick a date"
                    className="w-48"
                />
            );
        case "enum":
            return <EnumMultiPicker value={values} onChange={setValues} options={spec.options ?? []} labels={spec.option_labels} />;
        case "category":
            return <CategoryPicker value={values} onChange={setValues} placeholder="Pick categories…" allowCreate={false} />;
        case "campaign":
            return <CampaignMultiPicker value={values} onChange={setValues} />;
        case "segment":
            return <SegmentMultiPicker value={values} onChange={setValues} exclude={selfId} />;
        default:
            return null;
    }
}
