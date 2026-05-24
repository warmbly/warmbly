// Bulk edit selected contacts — brae-themed slide-over.
//
// Operations supported on N contacts at once:
//   - add / remove campaigns
//   - subscribe / unsubscribe
//   - custom field ADD / EDIT / DELETE / RENAME
//
// Same visual language as ContactEdit: 32rem panel, hairline borders,
// slate-900 primary action, 12.5px body text. No blue buttons.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    CheckIcon,
    ChevronDownIcon,
    Loader2Icon,
    MinusCircleIcon,
    PlusCircleIcon,
    PlusIcon,
    TrashIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import { Label, TextInput } from "@/components/ui/field";
import useUpdateContactsBulk from "@/lib/api/hooks/app/contacts/useUpdateContactsBulk";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import useClickOutside from "@/hooks/useClickOutside";
import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

type FieldType = "ADD" | "EDIT" | "DELETE" | "RENAME";
const FIELD_TYPES: { id: FieldType; label: string; hint: string }[] = [
    { id: "ADD",    label: "Add",    hint: "Only set the key if it's not already present." },
    { id: "EDIT",   label: "Edit",   hint: "Set the key to this value, overwriting any prior value." },
    { id: "DELETE", label: "Delete", hint: "Remove the key from each selected contact." },
    { id: "RENAME", label: "Rename", hint: "Rename the key — \"value\" is the new key name." },
];

interface Field {
    type: FieldType;
    key: string;
    value: string;
}

export default function ContactsEditBulk({
    active,
    setActive,
    selected,
}: {
    active: boolean;
    setActive: React.Dispatch<React.SetStateAction<boolean>>;
    selected: string[];
}) {
    const [campaignsAdd, setCampaignsAdd] = React.useState<MiniCampaign[]>([]);
    const [campaignsRemove, setCampaignsRemove] = React.useState<MiniCampaign[]>([]);
    const [fields, setFields] = React.useState<Field[]>([]);
    const [subscribeMode, setSubscribeMode] = React.useState<"unchanged" | "subscribe" | "unsubscribe">("unchanged");

    const update = useUpdateContactsBulk();

    const dirty =
        campaignsAdd.length > 0 ||
        campaignsRemove.length > 0 ||
        fields.length > 0 ||
        subscribeMode !== "unchanged";

    function reset() {
        setCampaignsAdd([]);
        setCampaignsRemove([]);
        setFields([]);
        setSubscribeMode("unchanged");
    }

    async function submit() {
        if (!dirty) return;
        const data = {
            contacts: selected,
            add_campaigns: campaignsAdd.map((c) => c.id),
            remove_campaigns: campaignsRemove.map((c) => c.id),
            fields,
            subscribe:
                subscribeMode === "subscribe" ? true : subscribeMode === "unsubscribe" ? false : undefined,
        };
        try {
            await toast.promise(update.mutateAsync(data), {
                loading: `Updating ${selected.length} contacts…`,
                success: `Updated ${selected.length} contacts`,
                error: (err: AppError) => buildError(err),
            });
            reset();
            setActive(false);
        } catch {
            /* surfaced */
        }
    }

    React.useEffect(() => {
        if (!active) reset();
    }, [active]);

    React.useEffect(() => {
        function onKey(e: KeyboardEvent) {
            if (e.key === "Escape") {
                if (dirty && !window.confirm("Discard bulk changes?")) return;
                setActive(false);
            }
        }
        if (active) window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [dirty, active, setActive]);

    return (
        <AnimatePresence>
            {active && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    className="fixed inset-0 z-[110] flex justify-end bg-slate-900/30 backdrop-blur-[2px]"
                    onMouseDown={() => (dirty && !window.confirm("Discard bulk changes?") ? undefined : setActive(false))}
                >
                    <motion.aside
                        key="panel"
                        initial={{ x: 32, opacity: 0 }}
                        animate={{ x: 0, opacity: 1 }}
                        exit={{ x: 32, opacity: 0 }}
                        transition={{ duration: 0.2, ease: [0.32, 0.72, 0, 1] }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="flex flex-col w-[32rem] max-w-[95%] h-full bg-white border-l border-slate-200 shadow-[-12px_0_24px_-12px_rgba(15,23,42,0.08)]"
                    >
                        <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Bulk edit
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">
                                {selected.length} {selected.length === 1 ? "contact" : "contacts"}
                            </span>
                            {dirty && (
                                <span className="text-[10px] uppercase tracking-[0.14em] text-amber-700 bg-amber-50 px-1.5 py-0.5 rounded-sm font-medium border border-amber-100">
                                    Pending
                                </span>
                            )}
                            <button
                                type="button"
                                onClick={() => (dirty && !window.confirm("Discard bulk changes?") ? undefined : setActive(false))}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </header>

                        <div className="flex-1 min-h-0 overflow-y-auto px-5 py-5 space-y-6">
                            <Section title="Campaigns" subtitle="Add or remove campaign membership in one shot.">
                                <div className="grid grid-cols-1 gap-3">
                                    <div>
                                        <Label className="flex items-center gap-1 text-emerald-700">
                                            <PlusCircleIcon className="w-3 h-3" />
                                            Add to campaigns
                                        </Label>
                                        <CampaignPicker selected={campaignsAdd} setSelected={setCampaignsAdd} />
                                    </div>
                                    <div>
                                        <Label className="flex items-center gap-1 text-red-700">
                                            <MinusCircleIcon className="w-3 h-3" />
                                            Remove from campaigns
                                        </Label>
                                        <CampaignPicker selected={campaignsRemove} setSelected={setCampaignsRemove} />
                                    </div>
                                </div>
                            </Section>

                            <Section
                                title="Subscription"
                                subtitle="Force every selected contact to a subscription state — or leave it untouched."
                            >
                                <div className="inline-flex rounded-md border border-slate-200 bg-white p-0.5">
                                    {(
                                        [
                                            ["unchanged", "Leave alone"],
                                            ["subscribe", "Subscribe"],
                                            ["unsubscribe", "Unsubscribe"],
                                        ] as const
                                    ).map(([id, label]) => (
                                        <button
                                            key={id}
                                            type="button"
                                            onClick={() => setSubscribeMode(id)}
                                            className={`h-6 px-2.5 rounded text-[11.5px] font-medium transition-colors ${
                                                subscribeMode === id
                                                    ? "bg-slate-900 text-white"
                                                    : "text-slate-500 hover:text-slate-900"
                                            }`}
                                        >
                                            {label}
                                        </button>
                                    ))}
                                </div>
                            </Section>

                            <Section
                                title="Custom fields"
                                subtitle="Add, edit, delete or rename custom fields across every selected contact."
                                accessory={
                                    fields.length < 100 && (
                                        <button
                                            type="button"
                                            onClick={() =>
                                                setFields((f) => [...f, { type: "ADD", key: "", value: "" }])
                                            }
                                            className="h-6 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-[11px] text-slate-600 hover:text-slate-900 inline-flex items-center gap-1 transition-colors"
                                        >
                                            <PlusIcon className="w-3 h-3" />
                                            Add operation
                                        </button>
                                    )
                                }
                            >
                                {fields.length === 0 ? (
                                    <div className="rounded-md border border-dashed border-slate-200 px-3 py-4 text-[11.5px] text-slate-400 text-center">
                                        No field operations queued.
                                    </div>
                                ) : (
                                    <div className="space-y-2">
                                        {fields.map((f, idx) => (
                                            <FieldRow
                                                key={idx}
                                                field={f}
                                                onChange={(next) =>
                                                    setFields((cur) =>
                                                        cur.map((c, i) => (i === idx ? next : c)),
                                                    )
                                                }
                                                onRemove={() =>
                                                    setFields((cur) => cur.filter((_, i) => i !== idx))
                                                }
                                            />
                                        ))}
                                    </div>
                                )}
                            </Section>
                        </div>

                        <footer className="h-12 px-3 border-t border-slate-200 flex items-center gap-1.5 shrink-0 bg-slate-50/30">
                            <button
                                type="button"
                                onClick={reset}
                                disabled={!dirty}
                                className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-40 disabled:hover:bg-transparent"
                            >
                                Discard
                            </button>
                            <button
                                type="button"
                                onClick={submit}
                                disabled={!dirty || update.isPending}
                                className="ml-auto h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                            >
                                {update.isPending ? (
                                    <Loader2Icon className="w-3 h-3 animate-spin" />
                                ) : (
                                    <CheckIcon className="w-3 h-3" />
                                )}
                                Apply to {selected.length}
                            </button>
                        </footer>
                    </motion.aside>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function Section({
    title,
    subtitle,
    accessory,
    children,
}: {
    title: string;
    subtitle?: string;
    accessory?: React.ReactNode;
    children: React.ReactNode;
}) {
    return (
        <section>
            <div className="flex items-start gap-2 mb-2">
                <div className="flex-1 min-w-0">
                    <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500">
                        {title}
                    </h2>
                    {subtitle && (
                        <p className="text-[11px] text-slate-400 leading-tight mt-0.5">{subtitle}</p>
                    )}
                </div>
                {accessory && <div className="shrink-0">{accessory}</div>}
            </div>
            <div className="space-y-2">{children}</div>
        </section>
    );
}

function FieldRow({
    field,
    onChange,
    onRemove,
}: {
    field: Field;
    onChange: (next: Field) => void;
    onRemove: () => void;
}) {
    const [showType, setShowType] = React.useState(false);
    const dropRef = React.useRef<HTMLDivElement>(null);
    useClickOutside(dropRef, () => setShowType(false));
    const typeDef = FIELD_TYPES.find((t) => t.id === field.type)!;

    return (
        <div className="rounded-md border border-slate-200 bg-white p-2.5 space-y-2">
            <div className="flex items-center gap-1.5">
                {/* Type selector */}
                <div ref={dropRef} className="relative shrink-0">
                    <button
                        type="button"
                        onClick={() => setShowType((s) => !s)}
                        className="h-7 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-[11.5px] font-medium text-slate-700 inline-flex items-center gap-1 transition-colors"
                    >
                        {typeDef.label}
                        <ChevronDownIcon className="w-3 h-3 text-slate-400" />
                    </button>
                    <AnimatePresence>
                        {showType && (
                            <motion.div
                                initial={{ opacity: 0, y: -4 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, y: -4 }}
                                transition={{ duration: 0.12 }}
                                className="absolute top-full left-0 mt-1 z-20 w-56 rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] py-1"
                            >
                                {FIELD_TYPES.map((t) => (
                                    <button
                                        key={t.id}
                                        type="button"
                                        onClick={() => {
                                            onChange({ ...field, type: t.id });
                                            setShowType(false);
                                        }}
                                        className={`w-full px-2.5 py-1.5 text-left hover:bg-slate-100 transition-colors ${
                                            t.id === field.type ? "bg-slate-50" : ""
                                        }`}
                                    >
                                        <div className="text-[11.5px] text-slate-900 font-medium leading-tight">
                                            {t.label}
                                        </div>
                                        <div className="text-[10.5px] text-slate-500 leading-tight mt-0.5">
                                            {t.hint}
                                        </div>
                                    </button>
                                ))}
                            </motion.div>
                        )}
                    </AnimatePresence>
                </div>
                <TextInput
                    value={field.key}
                    onChange={(v) => onChange({ ...field, key: v })}
                    placeholder="key"
                    className="flex-1"
                />
                <button
                    type="button"
                    onClick={onRemove}
                    aria-label="Remove operation"
                    className="size-7 rounded-md text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors shrink-0"
                >
                    <TrashIcon className="w-3 h-3" />
                </button>
            </div>
            {field.type !== "DELETE" && (
                <TextInput
                    value={field.value}
                    onChange={(v) => onChange({ ...field, value: v })}
                    placeholder={field.type === "RENAME" ? "new key name" : "value"}
                    className="w-full"
                />
            )}
        </div>
    );
}

/**
 * Inline brae-themed campaign picker, duplicated here so the panel
 * doesn't import legacy popup primitives. Kept private to this file.
 */
function CampaignPicker({
    selected,
    setSelected,
}: {
    selected: MiniCampaign[];
    setSelected: React.Dispatch<React.SetStateAction<MiniCampaign[]>>;
}) {
    const [open, setOpen] = React.useState(false);
    const [search, setSearch] = React.useState("");
    const [enabled, setEnabled] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));

    React.useEffect(() => {
        if (open) setEnabled(true);
    }, [open]);

    const { campaigns } = useCampaigns({ query: search, folder: "", enabled });

    function toggle(c: { id: string; name: string }) {
        setSelected((cur) =>
            cur.some((x) => x.id === c.id)
                ? cur.filter((x) => x.id !== c.id)
                : [...cur, { id: c.id, name: c.name }],
        );
    }

    return (
        <div ref={ref} className="relative">
            <div className="rounded-md border border-slate-200 bg-white">
                {selected.length === 0 ? (
                    <div
                        onClick={() => setOpen((o) => !o)}
                        className="px-3 py-2 text-[11.5px] text-slate-400 cursor-pointer hover:text-slate-600"
                    >
                        Click to pick campaigns.
                    </div>
                ) : (
                    <div className="px-2 py-2 flex flex-wrap gap-1">
                        {selected.map((c) => (
                            <span
                                key={c.id}
                                className="inline-flex items-center gap-1 h-5 pl-1.5 pr-1 rounded text-[11px] font-medium bg-slate-900 text-white"
                            >
                                {c.name}
                                <button
                                    type="button"
                                    onClick={(e) => {
                                        e.stopPropagation();
                                        toggle(c);
                                    }}
                                    className="opacity-70 hover:opacity-100"
                                    aria-label={`Remove ${c.name}`}
                                >
                                    <XIcon className="w-2.5 h-2.5" />
                                </button>
                            </span>
                        ))}
                        <button
                            type="button"
                            onClick={() => setOpen((o) => !o)}
                            className="inline-flex items-center gap-1 h-5 px-1.5 rounded text-[11px] font-medium border border-dashed border-slate-300 text-slate-500 hover:border-slate-400 hover:text-slate-700"
                        >
                            <PlusIcon className="w-2.5 h-2.5" />
                            Add
                        </button>
                    </div>
                )}
            </div>

            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={{ opacity: 0, y: -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -4 }}
                        transition={{ duration: 0.12 }}
                        className="absolute top-full left-0 right-0 mt-1 z-20 rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] overflow-hidden"
                    >
                        <div className="px-2 py-1.5 border-b border-slate-200">
                            <input
                                value={search}
                                onChange={(e) => setSearch(e.target.value)}
                                placeholder="Search campaigns…"
                                autoFocus
                                className="w-full h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                            />
                        </div>
                        <div className="max-h-56 overflow-y-auto py-1">
                            {campaigns.length === 0 ? (
                                <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">
                                    No campaigns found.
                                </div>
                            ) : (
                                campaigns.map((c) => {
                                    const checked = selected.some((s) => s.id === c.id);
                                    return (
                                        <button
                                            key={c.id}
                                            type="button"
                                            onClick={() => toggle(c)}
                                            className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-100 transition-colors"
                                        >
                                            <span
                                                className={`size-3.5 rounded border flex items-center justify-center transition-colors ${
                                                    checked
                                                        ? "border-slate-900 bg-slate-900"
                                                        : "border-slate-300 bg-white"
                                                }`}
                                            >
                                                {checked && <CheckIcon className="w-2 h-2 text-white" />}
                                            </span>
                                            <span className="truncate">{c.name}</span>
                                        </button>
                                    );
                                })
                            )}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
