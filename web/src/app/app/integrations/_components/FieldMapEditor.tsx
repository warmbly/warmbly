// FieldMapEditor — lets a user control exactly which Warmbly fields land in
// which provider fields for one CRM object. Standard fields (email/name/company/
// phone) map automatically; rows here ADD or OVERRIDE on top of that default,
// so the connection writes precisely what the user configured instead of a fixed
// shape. A full-replace save keeps it idempotent.

"use client";

import React from "react";
import { Loader2Icon, PlusIcon, Trash2Icon } from "lucide-react";
import toast from "react-hot-toast";

import { TextInput } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { useReplaceFieldMappings } from "@/lib/api/hooks/app/integrations/useFieldMappings";
import type {
    CapabilityObject,
    IntegrationFieldMapping,
} from "@/lib/api/models/app/integrations/Integration";
import { cn } from "@/lib/utils";

const CUSTOM = "__custom__";

const TRANSFORMS: SelectOption[] = [
    { value: "none", label: "Copy value" },
    { value: "uppercase", label: "Uppercase" },
    { value: "lowercase", label: "Lowercase" },
    { value: "trim", label: "Trim spaces" },
    { value: "static", label: "Static value" },
];

interface Row {
    warmbly_field: string;
    external_field: string;
    external_custom: boolean;
    transform: string;
    static_value: string;
}

export default function FieldMapEditor({
    connectionId,
    object,
    mappings,
}: {
    connectionId: string;
    object: CapabilityObject;
    mappings: IntegrationFieldMapping[];
}) {
    const replace = useReplaceFieldMappings();

    const initial = React.useMemo<Row[]>(
        () =>
            mappings
                .filter((m) => m.object_name === object.name && !m.subscription_id && m.direction === "push")
                .map((m) => ({
                    warmbly_field: m.warmbly_field,
                    external_field: m.external_field,
                    external_custom: !object.external_fields.some((f) => f.key === m.external_field),
                    transform: m.transform || "none",
                    static_value: m.static_value || "",
                })),
        [mappings, object],
    );

    const [rows, setRows] = React.useState<Row[]>(initial);
    React.useEffect(() => setRows(initial), [initial]);

    const dirty = React.useMemo(() => JSON.stringify(rows) !== JSON.stringify(initial), [rows, initial]);

    const warmblyOptions: SelectOption[] = object.warmbly_fields.map((f) => ({ value: f.key, label: f.label }));
    const externalOptions: SelectOption[] = [
        ...object.external_fields.map((f) => ({ value: f.key, label: f.label })),
        { value: CUSTOM, label: "Custom field…" },
    ];

    function patch(i: number, p: Partial<Row>) {
        setRows((r) => r.map((row, idx) => (idx === i ? { ...row, ...p } : row)));
    }
    function addRow() {
        setRows((r) => [
            ...r,
            { warmbly_field: warmblyOptions[0]?.value ?? "", external_field: "", external_custom: false, transform: "none", static_value: "" },
        ]);
    }
    function removeRow(i: number) {
        setRows((r) => r.filter((_, idx) => idx !== i));
    }

    async function save() {
        const out: { warmbly_field: string; external_field: string; transform: string; static_value: string }[] = [];
        for (const row of rows) {
            const ext = row.external_field.trim();
            if (!ext) continue; // skip incomplete rows silently
            if (row.transform === "static") {
                if (!row.static_value.trim()) {
                    toast.error(`The static mapping for "${ext}" needs a value`);
                    return;
                }
            } else if (!row.warmbly_field.trim()) {
                toast.error(`The mapping for "${ext}" needs a Warmbly field`);
                return;
            }
            out.push({
                warmbly_field: row.transform === "static" ? "" : row.warmbly_field,
                external_field: ext,
                transform: row.transform,
                static_value: row.transform === "static" ? row.static_value : "",
            });
        }
        await toast.promise(replace.mutateAsync({ connectionId, object: object.name, mappings: out }), {
            loading: "Saving field mapping…",
            success: "Field mapping saved",
            error: "Could not save mapping",
        });
    }

    return (
        <div className="space-y-2.5">
            <p className="text-[11.5px] text-slate-500 leading-relaxed">
                Email, name, company and phone map automatically. Add rows to send more Warmbly data
                into {object.label.toLowerCase()} fields, or override a default.
            </p>

            {rows.length > 0 && (
                <div className="space-y-2">
                    {rows.map((row, i) => (
                        <div key={i} className="rounded-md border border-slate-200 p-2 space-y-1.5">
                            <div className="flex flex-col items-stretch gap-1.5 sm:flex-row sm:items-center">
                                <div className="flex-1 min-w-0">
                                    {row.transform === "static" ? (
                                        <TextInput
                                            value={row.static_value}
                                            onChange={(v) => patch(i, { static_value: v })}
                                            placeholder="Static value"
                                        />
                                    ) : (
                                        <SelectMenu
                                            value={row.warmbly_field}
                                            onChange={(v) => patch(i, { warmbly_field: v })}
                                            options={warmblyOptions}
                                            className="w-full"
                                            aria-label="Warmbly field"
                                        />
                                    )}
                                </div>
                                <span className="text-slate-400 text-[11px] shrink-0 self-center rotate-90 sm:rotate-0 sm:self-auto">→</span>
                                <div className="flex-1 min-w-0">
                                    <SelectMenu
                                        value={row.external_custom ? CUSTOM : row.external_field}
                                        onChange={(v) =>
                                            v === CUSTOM
                                                ? patch(i, { external_custom: true, external_field: "" })
                                                : patch(i, { external_custom: false, external_field: v })
                                        }
                                        options={externalOptions}
                                        className="w-full"
                                        aria-label={`${object.label} field`}
                                    />
                                </div>
                                <button
                                    type="button"
                                    onClick={() => removeRow(i)}
                                    aria-label="Remove mapping"
                                    className="h-6 w-6 shrink-0 self-end sm:self-auto rounded text-slate-400 hover:text-rose-600 hover:bg-rose-50 inline-flex items-center justify-center transition-colors"
                                >
                                    <Trash2Icon className="w-3.5 h-3.5" />
                                </button>
                            </div>
                            <div className="flex flex-col gap-1.5 sm:flex-row sm:items-center">
                                {row.external_custom && (
                                    <TextInput
                                        value={row.external_field}
                                        onChange={(v) => patch(i, { external_field: v })}
                                        placeholder="Provider field API name"
                                        className="w-full sm:flex-1 font-mono"
                                    />
                                )}
                                <SelectMenu
                                    value={row.transform}
                                    onChange={(v) => patch(i, { transform: v })}
                                    options={TRANSFORMS}
                                    className={row.external_custom ? "w-full sm:w-36" : "w-full"}
                                    aria-label="Transform"
                                />
                            </div>
                        </div>
                    ))}
                </div>
            )}

            <div className="flex items-center justify-between pt-0.5">
                <button
                    type="button"
                    onClick={addRow}
                    className="h-6 px-2 rounded text-[11px] text-sky-700 hover:bg-sky-50 inline-flex items-center gap-1 transition-colors"
                >
                    <PlusIcon className="w-3 h-3" />
                    Add field
                </button>
                {dirty && (
                    <div className="flex items-center gap-2">
                        <button
                            type="button"
                            onClick={() => setRows(initial)}
                            className="h-6 px-2.5 rounded text-[11.5px] text-slate-600 hover:text-slate-900"
                        >
                            Reset
                        </button>
                        <button
                            type="button"
                            onClick={save}
                            disabled={replace.isPending}
                            className={cn(
                                "h-6 px-2.5 rounded text-[11.5px] font-medium text-white bg-sky-600 hover:bg-sky-700 inline-flex items-center gap-1.5 transition-colors",
                                replace.isPending && "opacity-60",
                            )}
                        >
                            {replace.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                            Save mapping
                        </button>
                    </div>
                )}
            </div>
        </div>
    );
}
