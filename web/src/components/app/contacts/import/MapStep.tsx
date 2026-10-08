// The column mapper shared by the file import and the Google Sheets sync: every
// column with its fill and a few values, where it goes, and a preview of the
// contacts the mapping makes.

import React from "react";
import useBrowseState from "@/hooks/useBrowseState";
import { browseText, browseBoolean, importViewSchema } from "@/lib/browse-contacts-campaigns";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertTriangleIcon,
    BracesIcon,
    CheckIcon,
    Columns3Icon,
    EyeIcon,
    FileSpreadsheetIcon,
    HistoryIcon,
    MailIcon,
    PlusIcon,
    ShieldCheckIcon,
    SparklesIcon,
    WandSparklesIcon,
} from "lucide-react";

import type { ImportColumnMapping, ImportPreview } from "@/lib/api/client/app/contacts/importContacts";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import { SearchInput, TextInput } from "@/components/ui/field";
import { Checkbox } from "@/components/ui/checkbox";
import useCustomFieldKeys from "@/lib/api/hooks/app/contacts/useCustomFieldKeys";
import {
    CUSTOM_KEY_RULES,
    STANDARD_TARGETS,
    VERIFICATION_VOCABULARY_LABELS,
    columnSamples,
    customKeyOf,
    customKeyStatus,
    derivePreview,
    fillRate,
    foldCustomKey,
    isCustomTarget,
    isValidCustomKey,
    matchExistingKey,
    normalizeCustomKey,
    suggestCustomKey,
    targetIdentity,
} from "../importShared";
import TargetIcon from "./TargetIcon";

type View = "columns" | "preview";

export default function MapStep({
    importId,
    preview: detected,
    mapping,
    setMapping,
    hasHeader,
    setHasHeader,
}: {
    importId: string;
    preview: ImportPreview;
    mapping: ImportColumnMapping[];
    setMapping: React.Dispatch<React.SetStateAction<ImportColumnMapping[]>>;
    hasHeader: boolean;
    setHasHeader: (v: boolean) => void;
}) {
    // The header choice moves the first row between header and data for real.
    const preview = React.useMemo(() => derivePreview(detected, hasHeader), [detected, hasHeader]);
    const [view, setView] = useBrowseState<View>(`contacts:imports:${importId}:mapping:view`, "columns", importViewSchema);
    const [query, setQuery] = useBrowseState(`contacts:imports:${importId}:mapping:query`, "", browseText);
    const [unmappedOnly, setUnmappedOnly] = useBrowseState(`contacts:imports:${importId}:mapping:unmapped-only`, false, browseBoolean);
    const { data: existingKeys = [] } = useCustomFieldKeys();

    function getMapping(idx: number): ImportColumnMapping {
        return mapping.find((m) => m.index === idx) ?? { index: idx, target: "ignore" };
    }

    function updateMapping(idx: number, next: ImportColumnMapping) {
        setMapping((cur) =>
            cur.map((m) => (m.index === idx ? next : m)).concat(cur.some((m) => m.index === idx) ? [] : [next]),
        );
    }

    // A column the judgment placed keeps its marker until the user changes it.
    function isInferred(idx: number, m: ImportColumnMapping): boolean {
        if (!detected.inferred_columns?.includes(idx)) return false;
        const s = detected.suggested_mapping.find((x) => x.index === idx);
        return !!s && s.target === m.target && (s.custom_key ?? "") === (m.custom_key ?? "");
    }

    // Which columns write to each destination, so a row can say when another
    // column shares its field. Column numbers are 1-based, as on screen.
    const writers = new Map<string, number[]>();
    for (const m of mapping) {
        const id = targetIdentity(m);
        if (id === null || m.index >= preview.columns.length) continue;
        writers.set(id, [...(writers.get(id) ?? []), m.index + 1]);
    }
    function takenKeysFor(idx: number): Map<string, number> {
        const taken = new Map<string, number>();
        for (const [id, cols] of writers) {
            const other = cols.find((c) => c !== idx + 1);
            if (id.startsWith("custom:") && other !== undefined) taken.set(id.slice(7), other);
        }
        return taken;
    }

    // Unrecognised columns default to Ignore. Offer the obvious bulk action for
    // the ones whose header is already a usable field name, landing on the
    // workspace's own spelling when the field exists. Only with a real header
    // row: "Column 4" is a legal field name but never the one you want.
    const claimable: { idx: number; key: string }[] = [];
    if (hasHeader) {
        const claimed = new Set([...writers.keys()]);
        preview.columns.forEach((header, idx) => {
            if (getMapping(idx).target !== "ignore") return;
            const key = matchExistingKey(header, existingKeys) ?? suggestCustomKey(header);
            if (key === "" || claimed.has(`custom:${key}`)) return;
            claimed.add(`custom:${key}`);
            claimable.push({ idx, key });
        });
    }

    function claimAllAsCustom() {
        setMapping((cur) => {
            const next = [...cur];
            for (const { idx, key } of claimable) {
                const at = next.findIndex((m) => m.index === idx);
                const entry: ImportColumnMapping = { index: idx, target: "custom", custom_key: key };
                if (at >= 0) next[at] = entry;
                else next.push(entry);
            }
            return next;
        });
    }

    const mappedCount = preview.columns.filter((_, idx) => getMapping(idx).target !== "ignore").length;
    const hasEmail = mapping.some((m) => m.target === "email" && m.index < preview.columns.length);
    // The column that looks most like addresses, offered when none is mapped.
    const emailCandidate = hasEmail
        ? -1
        : preview.columns.findIndex((_, idx) => columnSamples(preview, idx, 3).some((v) => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v)));

    const q = query.trim().toLowerCase();
    const visible = preview.columns
        .map((col, idx) => ({ col, idx }))
        .filter(({ col, idx }) => {
            if (unmappedOnly && getMapping(idx).target !== "ignore") return false;
            if (q === "") return true;
            return col.toLowerCase().includes(q) || columnSamples(preview, idx).some((v) => v.toLowerCase().includes(q));
        });

    return (
        <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
                <div className="flex items-center gap-2.5 min-w-0 flex-1">
                    <div className="size-8 rounded-md bg-emerald-50 text-emerald-700 flex items-center justify-center shrink-0">
                        <FileSpreadsheetIcon className="w-4 h-4" />
                    </div>
                    <div className="min-w-0">
                        <p className="text-[12.5px] text-slate-900 font-medium truncate">{preview.filename}</p>
                        <p className="text-[11px] text-slate-500">
                            {preview.format.toUpperCase()} · {preview.total_rows.toLocaleString()} rows ·{" "}
                            {preview.columns.length} columns · {mappedCount} mapped
                        </p>
                    </div>
                </div>
                {detected.mapping_source === "saved" && (
                    <span
                        className="inline-flex items-center gap-1 h-6 px-2 rounded-md bg-sky-50 text-sky-700 text-[11px] font-medium"
                        title="These headers were imported before, so the mapping you confirmed then is applied again."
                    >
                        <HistoryIcon className="w-3 h-3" />
                        Mapping remembered
                    </span>
                )}
                <label className="inline-flex items-center gap-1.5 text-[11.5px] text-slate-700 cursor-pointer select-none">
                    <Checkbox tone="slate" checked={hasHeader} onChange={(e) => setHasHeader(e.target.checked)} />
                    First row is header
                </label>
            </div>

            <AnimatePresence initial={false}>
                {!hasEmail && (
                    <motion.div
                        key="need-email"
                        initial={{ opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: "auto" }}
                        exit={{ opacity: 0, height: 0 }}
                        className="overflow-hidden"
                    >
                        <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 flex flex-wrap items-center gap-2">
                            <MailIcon className="w-3.5 h-3.5 text-amber-600 shrink-0" />
                            <p className="text-[12px] text-amber-900 flex-1 min-w-[200px]">
                                Every contact needs an email address. Pick the column that holds them.
                            </p>
                            {emailCandidate >= 0 && (
                                <button
                                    type="button"
                                    onClick={() => updateMapping(emailCandidate, { index: emailCandidate, target: "email" })}
                                    className="h-6 px-2 rounded-md bg-white border border-amber-200 hover:border-amber-300 text-[11.5px] font-medium text-amber-900 inline-flex items-center gap-1"
                                >
                                    <WandSparklesIcon className="w-3 h-3" />
                                    Use “{preview.columns[emailCandidate]}”
                                </button>
                            )}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>

            <div className="flex flex-wrap items-center gap-2">
                <div className="inline-flex items-center rounded-md border border-slate-200 p-0.5 bg-slate-50/60">
                    {(
                        [
                            { id: "columns", label: "Columns", icon: Columns3Icon },
                            { id: "preview", label: "Preview contacts", icon: EyeIcon },
                        ] as const
                    ).map((t) => (
                        <button
                            key={t.id}
                            type="button"
                            onClick={() => setView(t.id)}
                            className={`h-6 px-2 rounded text-[11.5px] inline-flex items-center gap-1.5 transition-colors ${
                                view === t.id ? "bg-white text-slate-900 font-medium shadow-sm" : "text-slate-500 hover:text-slate-800"
                            }`}
                        >
                            <t.icon className="w-3 h-3" />
                            {t.label}
                        </button>
                    ))}
                </div>
                {view === "columns" && (
                    <>
                        {preview.columns.length > 6 && (
                            <SearchInput value={query} onChange={setQuery} placeholder="Find a column…" className="w-full sm:w-48" />
                        )}
                        <label className="inline-flex items-center gap-1.5 text-[11.5px] text-slate-600 cursor-pointer select-none">
                            <Checkbox tone="slate" checked={unmappedOnly} onChange={(e) => setUnmappedOnly(e.target.checked)} />
                            Unmapped only
                        </label>
                        {claimable.length > 0 && (
                            <button
                                type="button"
                                onClick={claimAllAsCustom}
                                className="sm:ml-auto h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[11.5px] font-medium inline-flex items-center gap-1.5 transition-colors"
                            >
                                <BracesIcon className="w-3 h-3" />
                                Keep {claimable.length} more as custom fields
                            </button>
                        )}
                    </>
                )}
            </div>

            {view === "columns" ? (
                <div className="rounded-md border border-slate-200 overflow-hidden">
                    <div className="hidden md:grid grid-cols-[minmax(0,1.1fr)_minmax(0,1.3fr)_minmax(0,1.1fr)] gap-3 px-3 h-8 items-center bg-slate-50/60 border-b border-slate-200">
                        <span className="text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Column in file</span>
                        <span className="text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Values</span>
                        <span className="text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Imports as</span>
                    </div>
                    {visible.length === 0 && (
                        <p className="px-3 py-6 text-center text-[12px] text-slate-400">
                            {unmappedOnly && q === "" ? "Every column is mapped." : "No column matches."}
                        </p>
                    )}
                    {visible.map(({ col, idx }) => {
                        const m = getMapping(idx);
                        const samples = columnSamples(preview, idx);
                        const rate = fillRate(preview, idx);
                        const ignored = m.target === "ignore";
                        return (
                            <div
                                key={idx}
                                className={`grid grid-cols-1 md:grid-cols-[minmax(0,1.1fr)_minmax(0,1.3fr)_minmax(0,1.1fr)] gap-x-3 gap-y-1.5 px-3 py-2.5 border-b border-slate-100 last:border-b-0 transition-colors ${
                                    ignored ? "bg-white" : "bg-sky-50/20"
                                }`}
                            >
                                <div className="min-w-0 flex items-start gap-2">
                                    <span className="mt-0.5 text-[10.5px] text-slate-400 font-mono w-5 shrink-0 text-right">{idx + 1}</span>
                                    <div className="min-w-0 flex-1">
                                        <p className={`text-[12.5px] font-medium truncate ${ignored ? "text-slate-500" : "text-slate-900"}`} title={col}>
                                            {col}
                                        </p>
                                        {rate !== null && (
                                            <div className="mt-1 flex items-center gap-1.5" title={`${Math.round(rate * 100)}% of rows have a value`}>
                                                <div className="h-1 w-16 rounded-full bg-slate-100 overflow-hidden">
                                                    <div
                                                        className={`h-full rounded-full ${rate >= 0.9 ? "bg-emerald-500" : rate >= 0.5 ? "bg-sky-500" : "bg-amber-400"}`}
                                                        style={{ width: `${Math.max(2, rate * 100)}%` }}
                                                    />
                                                </div>
                                                <span className="text-[10.5px] text-slate-400 tabular-nums">{Math.round(rate * 100)}% filled</span>
                                            </div>
                                        )}
                                    </div>
                                </div>
                                <div className="min-w-0 flex flex-wrap items-start gap-1 md:pt-0.5 pl-7 md:pl-0">
                                    {samples.length === 0 ? (
                                        <span className="text-[11px] text-slate-300 italic">empty</span>
                                    ) : (
                                        samples.map((v, i) => (
                                            <span
                                                key={i}
                                                title={v}
                                                className="max-w-full md:max-w-[160px] truncate h-5 px-1.5 rounded bg-slate-100 text-slate-600 text-[11px] font-mono leading-5"
                                            >
                                                {v}
                                            </span>
                                        ))
                                    )}
                                </div>
                                <div className="min-w-0 pl-7 md:pl-0">
                                    <TargetPicker
                                        value={m}
                                        header={col}
                                        onChange={(next) => updateMapping(idx, next)}
                                        existingKeys={existingKeys}
                                        takenKeys={takenKeysFor(idx)}
                                        writers={writers.get(targetIdentity(m) ?? "") ?? []}
                                        inferred={isInferred(idx, m)}
                                    />
                                    <AnimatePresence initial={false}>
                                        {m.target === "verification_status" && (
                                            <motion.p
                                                key="vocab"
                                                initial={{ opacity: 0, y: -4 }}
                                                animate={{ opacity: 1, y: 0 }}
                                                exit={{ opacity: 0, y: -4 }}
                                                className="mt-1 text-[10.5px] text-emerald-700 inline-flex items-center gap-1"
                                            >
                                                <ShieldCheckIcon className="w-3 h-3" />
                                                {m.verification_provider && VERIFICATION_VOCABULARY_LABELS[m.verification_provider]
                                                    ? `${VERIFICATION_VOCABULARY_LABELS[m.verification_provider]} results recognised`
                                                    : "Verification results recognised; these leads skip the built-in check"}
                                            </motion.p>
                                        )}
                                    </AnimatePresence>
                                </div>
                            </div>
                        );
                    })}
                </div>
            ) : (
                <ContactPreview preview={preview} mapping={mapping} />
            )}
        </div>
    );
}

// ContactPreview shows the first rows as the contacts they will become.
function ContactPreview({ preview, mapping }: { preview: ImportPreview; mapping: ImportColumnMapping[] }) {
    const valueOf = (row: string[], target: string) =>
        mapping
            .filter((m) => m.target === target && m.index < preview.columns.length)
            .map((m) => (row[m.index] ?? "").trim())
            .filter(Boolean)
            .pop() ?? "";
    const customs = mapping.filter((m) => isCustomTarget(m.target) && customKeyOf(m) !== "" && m.index < preview.columns.length);
    const rows = preview.sample_rows.slice(0, 8);
    if (rows.length === 0) {
        return <p className="px-3 py-6 text-center text-[12px] text-slate-400">The file has no data rows.</p>;
    }
    return (
        <div className="rounded-md border border-slate-200 overflow-x-auto">
            <table className="w-full text-left">
                <thead className="bg-slate-50/60">
                    <tr className="border-b border-slate-200">
                        {[
                            { t: "email", l: "Email" },
                            { t: "first_name", l: "Name" },
                            { t: "company", l: "Company" },
                        ].map((h) => (
                            <th key={h.t} className="px-3 h-8 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] whitespace-nowrap">
                                <span className="inline-flex items-center gap-1">
                                    <TargetIcon target={h.t} />
                                    {h.l}
                                </span>
                            </th>
                        ))}
                        {customs.slice(0, 3).map((m) => (
                            <th key={m.index} className="px-3 h-8 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] whitespace-nowrap">
                                <span className="inline-flex items-center gap-1">
                                    <TargetIcon target="custom" />
                                    {customKeyOf(m)}
                                </span>
                            </th>
                        ))}
                    </tr>
                </thead>
                <tbody>
                    {rows.map((row, i) => {
                        const addr = valueOf(row, "email");
                        const valid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(addr.replace(/^.*<([^>]+)>.*$/, "$1"));
                        const name = [valueOf(row, "first_name"), valueOf(row, "last_name")].filter(Boolean).join(" ");
                        return (
                            <tr key={i} className="border-b border-slate-100 last:border-b-0">
                                <td className="px-3 py-1.5 text-[12px] whitespace-nowrap">
                                    {addr ? (
                                        <span className={`inline-flex items-center gap-1 ${valid ? "text-slate-900" : "text-red-600"}`}>
                                            {!valid && <AlertTriangleIcon className="w-3 h-3" />}
                                            {addr}
                                        </span>
                                    ) : (
                                        <span className="text-red-500 inline-flex items-center gap-1">
                                            <AlertTriangleIcon className="w-3 h-3" />
                                            no email
                                        </span>
                                    )}
                                </td>
                                <td className="px-3 py-1.5 text-[12px] text-slate-700 whitespace-nowrap">{name || <span className="text-slate-300">—</span>}</td>
                                <td className="px-3 py-1.5 text-[12px] text-slate-700 whitespace-nowrap">
                                    {valueOf(row, "company") || <span className="text-slate-300">—</span>}
                                </td>
                                {customs.slice(0, 3).map((m) => (
                                    <td key={m.index} className="px-3 py-1.5 text-[12px] text-slate-600 whitespace-nowrap max-w-[180px] truncate">
                                        {(row[m.index] ?? "").trim() || <span className="text-slate-300">—</span>}
                                    </td>
                                ))}
                            </tr>
                        );
                    })}
                </tbody>
            </table>
            {customs.length > 3 && (
                <p className="px-3 py-1.5 border-t border-slate-100 text-[11px] text-slate-400">
                    and {customs.length - 3} more custom field{customs.length - 3 === 1 ? "" : "s"}
                </p>
            )}
        </div>
    );
}

// MappingNote says what a custom mapping will do to the workspace's fields
// (fill an existing one, create one, or nearly duplicate one) and when another
// column writes to the same place.
function MappingNote({
    mapping,
    existingKeys,
    writers,
    column,
    onUseExisting,
}: {
    mapping: ImportColumnMapping;
    existingKeys: string[];
    // Every column writing where this one does, in the order the importer
    // applies them: the last non-empty cell is the one kept.
    writers: number[];
    column: number;
    onUseExisting: (key: string) => void;
}) {
    const sharedWith = writers.filter((c) => c !== column);
    const kept = writers[writers.length - 1];
    const key = customKeyOf(mapping);
    const status = isCustomTarget(mapping.target) && isValidCustomKey(key) ? customKeyStatus(key, existingKeys) : null;
    if (!status && sharedWith.length === 0) return null;
    return (
        <div className="mt-1 space-y-0.5">
            {status?.kind === "existing" && (
                <p className="text-[10.5px] text-emerald-700 inline-flex items-center gap-1">
                    <CheckIcon className="w-3 h-3 shrink-0" />
                    Fills your existing field
                </p>
            )}
            {status?.kind === "new" && (
                <p className="text-[10.5px] text-sky-700 inline-flex items-center gap-1">
                    <PlusIcon className="w-3 h-3 shrink-0" />
                    Creates a new field
                </p>
            )}
            {status?.kind === "similar" && (
                <p className="text-[10.5px] text-amber-700 flex flex-wrap items-center gap-x-1">
                    <AlertTriangleIcon className="w-3 h-3 shrink-0" />
                    <span>
                        You already have <span className="font-medium">{status.existing}</span>.
                    </span>
                    <button
                        type="button"
                        onClick={() => onUseExisting(status.existing)}
                        className="font-medium text-amber-800 underline underline-offset-2 hover:text-amber-900"
                    >
                        Use it
                    </button>
                </p>
            )}
            {sharedWith.length > 0 && (
                <p className="text-[10.5px] text-slate-500 leading-snug">
                    Also filled by column {sharedWith.join(", ")}. {sharedWith.length === 1 ? "When both have a value" : "When several do"}, column {kept}'s is kept.
                </p>
            )}
        </div>
    );
}

export function TargetPicker({
    value,
    onChange,
    header,
    existingKeys = [],
    takenKeys,
    writers = [],
    inferred = false,
}: {
    value: ImportColumnMapping;
    onChange: (next: ImportColumnMapping) => void;
    /** The column's header, used to pre-fill the custom-field name. */
    header?: string;
    /** The workspace's custom fields, most used first. */
    existingKeys?: string[];
    /** Custom fields other columns already write to, with that column's number. */
    takenKeys?: Map<string, number>;
    /** Every column writing where this one does, in the order they are applied. */
    writers?: number[];
    /** The suggestion came from what the header means, not what it says. */
    inferred?: boolean;
}) {
    const [open, setOpen] = React.useState(false);
    const [query, setQuery] = React.useState("");
    // Set once the user names a new field, so the name box stays put when
    // what they type happens to match an existing field.
    const [naming, setNaming] = React.useState(false);

    const isCustom = isCustomTarget(value.target.toString());
    const customKey = isCustom ? customKeyOf(value) : "";
    const rawKey = value.custom_key ?? customKey;
    const keyExists = isCustom && existingKeys.includes(customKey);
    const isExisting = keyExists && !naming;
    const keyInvalid = isCustom && rawKey.trim() !== "" && !isValidCustomKey(rawKey);
    const stdLabel = STANDARD_TARGETS.find((t) => t.id === value.target)?.label;
    const label = isExisting ? customKey : isCustom ? (keyExists ? "Existing field" : "New field") : stdLabel ?? "Ignore";

    const q = normalizeCustomKey(query);
    const fq = foldCustomKey(q);
    const matches = (text: string) => fq === "" || foldCustomKey(text).includes(fq);
    const standard = STANDARD_TARGETS.filter((t) => matches(t.label));
    const custom = existingKeys.filter(matches);
    // Typing a name nobody has yet offers to create it, the way a tag input does.
    const canCreate = q !== "" && isValidCustomKey(q) && !existingKeys.includes(q);

    function setMenuOpen(o: boolean) {
        setOpen(o);
        if (!o) setQuery("");
    }

    function pickStandard(id: string) {
        setNaming(false);
        onChange({ index: value.index, target: id });
    }

    function pickExisting(key: string) {
        setNaming(false);
        onChange({ index: value.index, target: "custom", custom_key: key });
    }

    function pickNew(name: string) {
        setNaming(true);
        onChange({ index: value.index, target: "custom", custom_key: name });
    }

    // Enter takes an exact name first, then the first match; with nothing
    // typed it does nothing, so it can never remap a column by accident.
    function pickFirst() {
        if (q === "") return;
        const exactStd = standard.find((t) => t.label.toLowerCase() === q.toLowerCase());
        if (exactStd) pickStandard(exactStd.id);
        else if (existingKeys.includes(q)) pickExisting(q);
        else if (standard.length > 0) pickStandard(standard[0].id);
        else if (custom.length > 0) pickExisting(custom[0]);
        else if (canCreate) pickNew(q);
        else return;
        setMenuOpen(false);
    }

    return (
        <>
            <div className="flex items-center gap-1.5">
                <PopoverMenu align="start" open={open} onOpenChange={setMenuOpen}>
                    <PopoverMenuTrigger asChild>
                        <SelectButton
                            icon={<TargetIcon target={isCustom ? "custom" : value.target} />}
                            label={label}
                            title={keyExists ? `Existing custom field: ${customKey}` : undefined}
                            className={`flex-1 min-w-0 ${value.target === "ignore" ? "text-slate-500" : value.target === "email" ? "border-sky-200 bg-sky-50/40" : ""}`}
                        />
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={240} className="py-0 w-[260px]">
                        <div className="px-2 py-1.5 border-b border-slate-200">
                            <input
                                value={query}
                                onChange={(e) => setQuery(e.target.value)}
                                onKeyDown={(e) => {
                                    if (e.key === "Enter") {
                                        e.preventDefault();
                                        pickFirst();
                                    }
                                }}
                                placeholder={existingKeys.length > 0 ? "Search or name a new field…" : "Search or name a field…"}
                                autoFocus
                                aria-label="Search fields"
                                className="w-full h-5 bg-transparent text-[16px] md:text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                            />
                        </div>
                        <div className="max-h-[min(60vh,360px)] overflow-y-auto py-1">
                            {standard.length > 0 && <PopoverMenuLabel>Standard</PopoverMenuLabel>}
                            {standard.map((t) => (
                                <PopoverMenuItem
                                    key={t.id}
                                    icon={<TargetIcon target={t.id} />}
                                    selected={value.target === t.id && !isCustom}
                                    onSelect={() => pickStandard(t.id)}
                                >
                                    {t.label}
                                </PopoverMenuItem>
                            ))}
                            {custom.length > 0 && <PopoverMenuLabel>Your custom fields</PopoverMenuLabel>}
                            {custom.map((key) => {
                                const takenBy = takenKeys?.get(key);
                                return (
                                    <PopoverMenuItem
                                        key={key}
                                        icon={<BracesIcon className="w-3 h-3" />}
                                        selected={isCustom && customKey === key}
                                        onSelect={() => pickExisting(key)}
                                        trailing={
                                            isCustom && customKey === key ? undefined : takenBy !== undefined ? (
                                                <span className="text-[10.5px] text-slate-400">column {takenBy}</span>
                                            ) : undefined
                                        }
                                    >
                                        {key}
                                    </PopoverMenuItem>
                                );
                            })}
                            <PopoverMenuLabel>New</PopoverMenuLabel>
                            {canCreate ? (
                                <PopoverMenuItem icon={<PlusIcon className="w-3 h-3" />} onSelect={() => pickNew(q)}>
                                    Create “{q}”
                                </PopoverMenuItem>
                            ) : (
                                <PopoverMenuItem
                                    icon={<PlusIcon className="w-3 h-3" />}
                                    selected={isCustom && !isExisting}
                                    // Start from the column header: "Company Mobile"
                                    // is a valid field name, so there is nothing to
                                    // type in the common case.
                                    onSelect={() => pickNew(isCustom && !isExisting ? rawKey : suggestCustomKey(header ?? ""))}
                                >
                                    New custom field…
                                </PopoverMenuItem>
                            )}
                            {q !== "" && !canCreate && standard.length === 0 && custom.length === 0 && (
                                <p className="px-3 pb-1.5 text-[11px] text-slate-400 leading-snug">{CUSTOM_KEY_RULES}</p>
                            )}
                        </div>
                    </PopoverMenuContent>
                </PopoverMenu>
                {isCustom && !isExisting && (
                    <TextInput
                        value={rawKey}
                        onChange={(v) => {
                            setNaming(true);
                            onChange({ index: value.index, target: "custom", custom_key: v });
                        }}
                        placeholder="field name"
                        invalid={keyInvalid}
                        title={keyInvalid ? CUSTOM_KEY_RULES : undefined}
                        className="w-24 md:w-32"
                    />
                )}
            </div>
            {inferred && (
                <p className="mt-1 text-[10.5px] text-sky-700 inline-flex items-center gap-1">
                    <SparklesIcon className="w-3 h-3 shrink-0" />
                    Matched by meaning. Check it.
                </p>
            )}
            <MappingNote
                mapping={value}
                existingKeys={existingKeys}
                writers={writers}
                column={value.index + 1}
                onUseExisting={pickExisting}
            />
        </>
    );
}
