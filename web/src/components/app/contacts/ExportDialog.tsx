// ExportDialog — pick format, scope, and columns, then download.
//
// Replaces the in-memory CSV generator that lived on ContactsTable.
// The backend handles the heavy lifting (no more "Exported only the
// rows currently loaded" bug) and supports CSV / XLSX / JSON.
//
// UX notes:
//   - Scope defaults to "filtered" if any filter is set, otherwise
//     "all". Selection scope only available when rows are selected.
//   - Field presets are the most common 80% paths; "Custom" reveals
//     the full checkbox list for the power user.
//   - Custom-field columns are derived from the user's current
//     filters / common fields; we don't reach into the DB for the
//     full field universe here. Users can type a key for any
//     custom field that exists on their contacts.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    CheckIcon,
    DownloadIcon,
    FileSpreadsheetIcon,
    FileTextIcon,
    FileJsonIcon,
    Loader2Icon,
    PlusIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";

import exportContacts, {
    downloadBlob,
    type ExportFormat,
    type ExportScope,
} from "@/lib/api/client/app/contacts/exportContacts";
import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import { Label, TextInput } from "@/components/ui/field";

interface Props {
    open: boolean;
    onClose: () => void;
    filters: SearchContacts;
    selectedIds: string[];
    totalKnown: number | null;
}

const STANDARD_FIELDS: { id: string; label: string; preset: "basic" | "full" | "campaign" }[] = [
    { id: "email",        label: "Email",         preset: "basic" },
    { id: "first_name",   label: "First name",    preset: "basic" },
    { id: "last_name",    label: "Last name",     preset: "basic" },
    { id: "company",      label: "Company",       preset: "basic" },
    { id: "phone",        label: "Phone",         preset: "basic" },
    { id: "subscribed",   label: "Subscribed",    preset: "basic" },
    { id: "categories",   label: "Categories",    preset: "full" },
    { id: "campaigns",    label: "Campaigns",     preset: "full" },
    { id: "created_at",   label: "Created at",    preset: "full" },
    { id: "updated_at",   label: "Updated at",    preset: "full" },
    { id: "id",           label: "Contact ID",    preset: "full" },
];

const PRESETS: { id: "basic" | "full" | "campaign-ready" | "custom"; label: string; hint: string }[] = [
    { id: "basic",          label: "Basic",          hint: "Core contact details — what most CRMs expect." },
    { id: "full",           label: "Full",           hint: "Every standard column including categories + campaigns." },
    { id: "campaign-ready", label: "Campaign-ready", hint: "Email + names + company — the minimum to import elsewhere." },
    { id: "custom",         label: "Custom",         hint: "Pick exactly what you need." },
];

const FORMATS: { id: ExportFormat; label: string; sub: string; Icon: React.ComponentType<{ className?: string }> }[] = [
    { id: "csv",  label: "CSV",  sub: "Universal, opens in Excel/Sheets",   Icon: FileTextIcon },
    { id: "xlsx", label: "XLSX", sub: "Native Excel, formatted",            Icon: FileSpreadsheetIcon },
    { id: "json", label: "JSON", sub: "Structured, round-trips via import", Icon: FileJsonIcon },
];

function presetFields(p: "basic" | "full" | "campaign-ready" | "custom"): string[] {
    switch (p) {
        case "basic":
            return STANDARD_FIELDS.filter((f) => f.preset === "basic").map((f) => f.id);
        case "full":
            return STANDARD_FIELDS.map((f) => f.id);
        case "campaign-ready":
            return ["email", "first_name", "last_name", "company"];
        case "custom":
            return [];
    }
}

function hasActiveFilters(f: SearchContacts): boolean {
    return (
        !!f.query ||
        f.filters.length > 0 ||
        (f.campaign_ids?.length ?? 0) > 0 ||
        (f.category_ids?.length ?? 0) > 0 ||
        f.subscribed !== undefined ||
        f.min_campaigns !== undefined ||
        f.max_campaigns !== undefined ||
        !!f.created_after ||
        !!f.created_before ||
        !!f.updated_after ||
        !!f.updated_before
    );
}

export default function ExportDialog({
    open,
    onClose,
    filters,
    selectedIds,
    totalKnown,
}: Props) {
    const [format, setFormat] = React.useState<ExportFormat>("csv");
    const [scope, setScope] = React.useState<ExportScope>(() =>
        selectedIds.length > 0 ? "selected" : hasActiveFilters(filters) ? "filtered" : "all",
    );
    const [preset, setPreset] = React.useState<"basic" | "full" | "campaign-ready" | "custom">("basic");
    const [fields, setFields] = React.useState<string[]>(presetFields("basic"));
    const [customFieldKeys, setCustomFieldKeys] = React.useState<string[]>([]);
    const [customKeyDraft, setCustomKeyDraft] = React.useState<string>("");
    const [filename, setFilename] = React.useState<string>("");
    const [loading, setLoading] = React.useState<boolean>(false);

    // Re-derive default scope when the dialog opens. We do NOT auto-
    // change it after the user picks one manually — that's annoying.
    React.useEffect(() => {
        if (open) {
            setScope(selectedIds.length > 0 ? "selected" : hasActiveFilters(filters) ? "filtered" : "all");
            setPreset("basic");
            setFields(presetFields("basic"));
            setCustomFieldKeys([]);
            setFilename("");
        }
    }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

    function applyPreset(p: "basic" | "full" | "campaign-ready" | "custom") {
        setPreset(p);
        if (p !== "custom") setFields(presetFields(p));
    }

    function toggleField(id: string) {
        setPreset("custom");
        setFields((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]));
    }

    function addCustomKey() {
        const k = customKeyDraft.trim();
        if (!k) return;
        if (customFieldKeys.includes(k)) return;
        setCustomFieldKeys((cur) => [...cur, k]);
        setCustomKeyDraft("");
    }

    function removeCustomKey(k: string) {
        setCustomFieldKeys((cur) => cur.filter((x) => x !== k));
    }

    async function submit() {
        const effective = [
            ...fields,
            ...customFieldKeys.map((k) => `custom:${k}`),
        ];
        if (effective.length === 0) {
            toast.error("Pick at least one column to export.");
            return;
        }
        setLoading(true);
        try {
            const result = await exportContacts({
                format,
                scope,
                contact_ids: scope === "selected" ? selectedIds : undefined,
                filters: scope === "filtered" ? filters : undefined,
                fields: effective,
                filename: filename.trim() || undefined,
            });
            downloadBlob(result.blob, result.filename);
            toast.success("Export ready");
            onClose();
        } catch (err) {
            const msg = err instanceof Error ? err.message : "Export failed.";
            toast.error(msg);
        } finally {
            setLoading(false);
        }
    }

    const scopeCount = scope === "selected" ? selectedIds.length : totalKnown ?? 0;

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onClick={onClose}
                    className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.18 }}
                        onClick={(e) => e.stopPropagation()}
                        className="w-full max-w-[640px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[90dvh]"
                    >
                        <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <DownloadIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Export
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">
                                Contacts
                            </span>
                            <button
                                type="button"
                                onClick={onClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </header>

                        <div className="flex-1 min-h-0 overflow-y-auto px-5 py-4 space-y-5">
                            <Section title="Format" subtitle="What the downloaded file should be.">
                                <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                                    {FORMATS.map(({ id, label, sub, Icon }) => (
                                        <button
                                            key={id}
                                            type="button"
                                            onClick={() => setFormat(id)}
                                            className={`text-left rounded-md border p-2.5 transition-colors ${
                                                format === id
                                                    ? "border-slate-900 bg-slate-50"
                                                    : "border-slate-200 hover:border-slate-300"
                                            }`}
                                        >
                                            <div className="flex items-center gap-1.5 mb-0.5">
                                                <Icon className="w-3.5 h-3.5 text-slate-600" />
                                                <span className="text-[12px] font-medium text-slate-900">{label}</span>
                                            </div>
                                            <div className="text-[10.5px] text-slate-500 leading-tight">{sub}</div>
                                        </button>
                                    ))}
                                </div>
                            </Section>

                            <Section title="Scope" subtitle={`${scopeCount.toLocaleString()} contacts will be exported.`}>
                                <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                                    <ScopeButton
                                        active={scope === "all"}
                                        onClick={() => setScope("all")}
                                        title="Everyone"
                                        sub="All your contacts"
                                    />
                                    <ScopeButton
                                        active={scope === "filtered"}
                                        onClick={() => setScope("filtered")}
                                        title="Filtered"
                                        sub={hasActiveFilters(filters) ? "Matches current filters" : "No filters set"}
                                        disabled={!hasActiveFilters(filters)}
                                    />
                                    <ScopeButton
                                        active={scope === "selected"}
                                        onClick={() => setScope("selected")}
                                        title="Selected"
                                        sub={selectedIds.length > 0 ? `${selectedIds.length} selected` : "Nothing selected"}
                                        disabled={selectedIds.length === 0}
                                    />
                                </div>
                            </Section>

                            <Section title="Columns" subtitle="Pick a preset or build a custom set.">
                                <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 mb-3">
                                    {PRESETS.map((p) => (
                                        <button
                                            key={p.id}
                                            type="button"
                                            onClick={() => applyPreset(p.id)}
                                            className={`text-left rounded-md border p-2 transition-colors ${
                                                preset === p.id
                                                    ? "border-slate-900 bg-slate-50"
                                                    : "border-slate-200 hover:border-slate-300"
                                            }`}
                                        >
                                            <div className="text-[11.5px] font-medium text-slate-900 leading-tight">
                                                {p.label}
                                            </div>
                                            <div className="text-[10px] text-slate-500 leading-tight mt-0.5">
                                                {p.hint}
                                            </div>
                                        </button>
                                    ))}
                                </div>

                                <div className="grid grid-cols-2 sm:grid-cols-3 gap-1.5">
                                    {STANDARD_FIELDS.map((f) => {
                                        const checked = fields.includes(f.id);
                                        return (
                                            <label
                                                key={f.id}
                                                className="flex items-center gap-2 h-7 px-2 rounded hover:bg-slate-50 cursor-pointer"
                                            >
                                                <input
                                                    type="checkbox"
                                                    className="w-3.5 h-3.5 rounded accent-slate-900"
                                                    checked={checked}
                                                    onChange={() => toggleField(f.id)}
                                                />
                                                <span className="text-[12px] text-slate-700 truncate">{f.label}</span>
                                            </label>
                                        );
                                    })}
                                </div>

                                <div className="mt-3 pt-3 border-t border-slate-100">
                                    <Label className="text-[10.5px] text-slate-500">Custom fields</Label>
                                    <div className="flex items-center gap-1.5 mt-1">
                                        <TextInput
                                            value={customKeyDraft}
                                            onChange={setCustomKeyDraft}
                                            placeholder="custom field key…"
                                            className="flex-1"
                                            onKeyDown={(e) => {
                                                if (e.key === "Enter") {
                                                    e.preventDefault();
                                                    addCustomKey();
                                                }
                                            }}
                                        />
                                        <button
                                            type="button"
                                            onClick={addCustomKey}
                                            disabled={!customKeyDraft.trim()}
                                            className="h-7 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-[11.5px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                                        >
                                            <PlusIcon className="w-3 h-3" />
                                            Add
                                        </button>
                                    </div>
                                    {customFieldKeys.length > 0 && (
                                        <div className="mt-2 flex flex-wrap gap-1">
                                            {customFieldKeys.map((k) => (
                                                <span
                                                    key={k}
                                                    className="inline-flex items-center gap-1 h-5 pl-1.5 pr-1 rounded text-[10.5px] font-medium bg-slate-100 text-slate-700 border border-slate-200"
                                                >
                                                    <span className="font-mono">{k}</span>
                                                    <button
                                                        type="button"
                                                        onClick={() => removeCustomKey(k)}
                                                        className="opacity-60 hover:opacity-100"
                                                        aria-label={`Remove ${k}`}
                                                    >
                                                        <XIcon className="w-2.5 h-2.5" />
                                                    </button>
                                                </span>
                                            ))}
                                        </div>
                                    )}
                                </div>
                            </Section>

                            <Section title="Filename" subtitle="Optional. Server will pick a sensible default.">
                                <TextInput
                                    value={filename}
                                    onChange={setFilename}
                                    placeholder="e.g. q3-leads"
                                    className="w-full"
                                />
                            </Section>
                        </div>

                        <footer className="h-12 px-3 border-t border-slate-200 flex items-center gap-1.5 shrink-0 bg-slate-50/30">
                            <span className="text-[11px] text-slate-400 truncate min-w-0">
                                {scopeCount.toLocaleString()} contacts · {fields.length + customFieldKeys.length} columns
                            </span>
                            <button
                                type="button"
                                onClick={onClose}
                                className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={submit}
                                disabled={loading}
                                className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {loading ? (
                                    <Loader2Icon className="w-3 h-3 animate-spin" />
                                ) : (
                                    <DownloadIcon className="w-3 h-3" />
                                )}
                                Download
                            </button>
                        </footer>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function Section({
    title,
    subtitle,
    children,
}: {
    title: string;
    subtitle?: string;
    children: React.ReactNode;
}) {
    return (
        <section>
            <div className="mb-2">
                <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500">
                    {title}
                </h2>
                {subtitle && (
                    <p className="text-[11px] text-slate-400 leading-tight mt-0.5">{subtitle}</p>
                )}
            </div>
            {children}
        </section>
    );
}

function ScopeButton({
    active,
    onClick,
    title,
    sub,
    disabled,
}: {
    active: boolean;
    onClick: () => void;
    title: string;
    sub: string;
    disabled?: boolean;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            disabled={disabled}
            className={`text-left rounded-md border p-2.5 transition-colors ${
                active
                    ? "border-slate-900 bg-slate-50"
                    : "border-slate-200 hover:border-slate-300"
            } ${disabled ? "opacity-50 cursor-not-allowed hover:border-slate-200" : ""}`}
        >
            <div className="flex items-center gap-1.5 mb-0.5">
                <span
                    className={`size-3 rounded-full border ${
                        active ? "border-slate-900 bg-slate-900" : "border-slate-300"
                    }`}
                >
                    {active && <CheckIcon className="w-2.5 h-2.5 text-white" />}
                </span>
                <span className="text-[12px] font-medium text-slate-900">{title}</span>
            </div>
            <div className="text-[10.5px] text-slate-500 leading-tight pl-4">{sub}</div>
        </button>
    );
}

