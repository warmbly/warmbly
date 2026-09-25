// A contact's custom fields as a form: every field the workspace already uses
// is a labelled input, so filling one in means typing a value, not its name.
// "New field" names one nobody has yet.

import React from "react";
import { ChevronDownIcon, PlusIcon, TrashIcon } from "lucide-react";
import { SearchInput, TextInput } from "@/components/ui/field";
import useCustomFieldKeys from "@/lib/api/hooks/app/contacts/useCustomFieldKeys";
import { cn } from "@/lib/utils";
import CustomFieldKeyInput from "./CustomFieldKeyInput";
import { type CustomField, draftMatch, labelledKeys, suggestKeys } from "./customFields";
import { CUSTOM_KEY_RULES, isValidCustomKey, normalizeCustomKey } from "./importShared";

// Empty workspace fields past this many wait behind "Show more".
const COLLAPSE_AFTER = 6;
const FILTER_FROM = 12;

export default function CustomFieldsEditor({
    value,
    onChange,
}: {
    value: CustomField[];
    onChange: React.Dispatch<React.SetStateAction<CustomField[]>>;
}) {
    const { data: workspace = [], isPending } = useCustomFieldKeys();
    const [expanded, setExpanded] = React.useState(false);
    const [filter, setFilter] = React.useState("");

    // Every field the form has held stays on screen once cleared, so emptying
    // one does not make it vanish from under the cursor.
    const [held, setHeld] = React.useState<string[]>(() => labelledNames(value));
    React.useEffect(() => {
        setHeld((cur) => {
            const add = labelledNames(value).filter((n) => !cur.includes(n));
            return add.length > 0 ? [...cur, ...add] : cur;
        });
    }, [value]);

    const keys = labelledKeys(workspace, [...held, ...labelledNames(value)]);
    const valueOf = (key: string) => value.find((r) => !r.draft && r.name === key)?.value ?? "";
    const filled = (key: string) => valueOf(key).trim() !== "";

    const filtering = filter.trim() !== "";
    const matching = filtering ? new Set(suggestKeys(filter, keys, keys.length)) : null;
    const visible = keys.filter((k, i) =>
        matching ? matching.has(k) : expanded || i < COLLAPSE_AFTER || held.includes(k),
    );
    const hidden = filtering ? 0 : keys.length - visible.length;
    const drafts = value.map((r, i) => ({ row: r, index: i })).filter((d) => d.row.draft);

    function setValue(key: string, v: string) {
        setHeld((cur) => (cur.includes(key) ? cur : [...cur, key]));
        onChange((cur) => {
            const i = cur.findIndex((r) => !r.draft && r.name === key);
            // A cleared field is no field: the row goes and the save removes the key.
            if (i === -1) return v === "" ? cur : [...cur, { name: key, value: v }];
            if (v === "") return cur.filter((_, j) => j !== i);
            return cur.map((r, j) => (j === i ? { ...r, value: v } : r));
        });
    }

    function addDraft() {
        onChange((cur) => [...cur, { name: "", value: "", draft: true }]);
    }

    function updateDraft(index: number, patch: Partial<CustomField>) {
        onChange((cur) => cur.map((r, j) => (j === index ? { ...r, ...patch } : r)));
    }

    function removeDraft(index: number) {
        onChange((cur) => cur.filter((_, j) => j !== index));
    }

    // Move a draft onto the labelled field it names. A field already filled in
    // keeps its value; the draft just takes the name and the hint explains.
    function adopt(index: number, key: string) {
        const draft = value[index];
        if (draft && filled(key) && draft.value.trim() !== "") {
            updateDraft(index, { name: key });
            return;
        }
        setHeld((cur) => (cur.includes(key) ? cur : [...cur, key]));
        onChange((cur) => {
            const d = cur[index];
            const rest = cur.filter((_, j) => j !== index);
            if (!d || d.value.trim() === "") return rest;
            const i = rest.findIndex((r) => !r.draft && r.name === key);
            if (i === -1) return [...rest, { name: key, value: d.value }];
            return rest.map((r, j) => (j === i ? { ...r, value: d.value } : r));
        });
    }

    const empty = keys.length === 0 && drafts.length === 0;

    return (
        <div className="space-y-2">
            {keys.length > FILTER_FROM && (
                <SearchInput value={filter} onChange={setFilter} placeholder="Filter fields…" />
            )}

            {empty && isPending ? (
                <div className="h-7 rounded-md bg-slate-100 animate-pulse" />
            ) : empty ? (
                <button
                    type="button"
                    onClick={addDraft}
                    className="w-full rounded-md border border-dashed border-slate-200 hover:border-slate-300 px-3 py-4 text-[11.5px] text-slate-400 hover:text-slate-600 text-center transition-colors"
                >
                    No custom fields yet. Add one to attach extra metadata.
                </button>
            ) : (
                visible.length > 0 && (
                    <div className="space-y-1.5">
                        {visible.map((k) => (
                            <label key={k} className="flex items-center gap-2">
                                <span
                                    title={k}
                                    className="w-[110px] md:w-[140px] shrink-0 truncate text-[12px] text-slate-600"
                                >
                                    {k}
                                </span>
                                <TextInput
                                    value={valueOf(k)}
                                    onChange={(v) => setValue(k, v)}
                                    placeholder="Empty"
                                    className="flex-1"
                                />
                            </label>
                        ))}
                    </div>
                )
            )}

            {filtering && visible.length === 0 && (
                <p className="text-[11.5px] text-slate-400">No field matches “{filter.trim()}”.</p>
            )}

            {drafts.length > 0 && (
                <div className="space-y-2">
                    {drafts.map(({ row, index }, n) => (
                        <DraftRow
                            key={`draft-${n}`}
                            row={row}
                            keys={keys}
                            targetFilled={filled}
                            onName={(v) => updateDraft(index, { name: v })}
                            onValue={(v) => updateDraft(index, { value: v })}
                            onAdopt={(k) => adopt(index, k)}
                            onRemove={() => removeDraft(index)}
                        />
                    ))}
                </div>
            )}

            {!empty && (
                <div className="flex items-center gap-2">
                    <button
                        type="button"
                        onClick={addDraft}
                        className="h-6 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-[11px] text-slate-600 hover:text-slate-900 inline-flex items-center gap-1 transition-colors"
                    >
                        <PlusIcon className="w-3 h-3" />
                        New field
                    </button>
                    {!filtering && (hidden > 0 || expanded) && keys.length > COLLAPSE_AFTER && (
                        <button
                            type="button"
                            onClick={() => setExpanded((e) => !e)}
                            aria-expanded={expanded}
                            className="ml-auto h-6 px-1.5 rounded-md text-[11px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors"
                        >
                            {expanded ? "Show fewer" : `Show ${hidden} more`}
                            <ChevronDownIcon className={cn("w-3 h-3 transition-transform", expanded && "rotate-180")} />
                        </button>
                    )}
                </div>
            )}
        </div>
    );
}

function DraftRow({
    row,
    keys,
    targetFilled,
    onName,
    onValue,
    onAdopt,
    onRemove,
}: {
    row: CustomField;
    keys: string[];
    targetFilled: (key: string) => boolean;
    onName: (v: string) => void;
    onValue: (v: string) => void;
    onAdopt: (key: string) => void;
    onRemove: () => void;
}) {
    const name = normalizeCustomKey(row.name);
    const hasValue = row.value.trim() !== "";
    const invalid = name !== "" && !isValidCustomKey(name);
    const match = name !== "" && !invalid ? draftMatch(name, keys) : null;

    let hint: React.ReactNode = null;
    let tone: "muted" | "warn" | "error" = "muted";
    let canAdopt = false;
    if (name === "") {
        if (hasValue) {
            hint = "Name this field, or remove it.";
            tone = "warn";
        }
    } else if (invalid) {
        hint = CUSTOM_KEY_RULES;
        tone = "error";
    } else if (match) {
        const taken = targetFilled(match.key);
        canAdopt = !(taken && hasValue);
        if (match.exact && taken && hasValue) {
            hint = <>“{match.key}” is already filled in above.</>;
            tone = "error";
        } else if (match.exact) {
            hint = <>“{match.key}” is already a field.</>;
        } else {
            hint = <>You already have “{match.key}”.</>;
            tone = "warn";
        }
    } else {
        hint = "Creates a new field.";
    }

    return (
        <div>
            <div className="flex items-start gap-1.5">
                <CustomFieldKeyInput
                    value={row.name}
                    onChange={onName}
                    onPick={onAdopt}
                    keys={keys}
                    autoFocus={row.name === "" && row.value === ""}
                    invalid={invalid}
                    title={invalid ? CUSTOM_KEY_RULES : undefined}
                    className="w-[110px] md:w-[140px] shrink-0"
                />
                <TextInput value={row.value} onChange={onValue} placeholder="Value" className="flex-1" />
                <button
                    type="button"
                    onClick={onRemove}
                    aria-label="Remove field"
                    className="size-7 rounded-md text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors shrink-0"
                >
                    <TrashIcon className="w-3 h-3" />
                </button>
            </div>
            {hint && (
                <p
                    className={cn(
                        "mt-1 text-[10.5px] leading-tight",
                        tone === "error" ? "text-red-600" : tone === "warn" ? "text-amber-700" : "text-slate-400",
                    )}
                >
                    {hint}
                    {match && canAdopt && (
                        <button
                            type="button"
                            onClick={() => onAdopt(match.key)}
                            className="ml-1.5 font-medium text-sky-700 hover:text-sky-800 hover:underline"
                        >
                            Use it
                        </button>
                    )}
                </p>
            )}
        </div>
    );
}

function labelledNames(rows: CustomField[]): string[] {
    return rows.filter((r) => !r.draft).map((r) => r.name);
}
