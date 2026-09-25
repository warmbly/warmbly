// Bulk edit selected contacts — drawer in the ContactEdit family (32rem panel,
// hairline sections, slate-900 primary, 12.5px body).
//
// Operations applied to N contacts at once:
//   - add / remove campaigns
//   - add / remove categories
//   - subscribe / unsubscribe
//   - custom field ADD / EDIT / DELETE / RENAME
//
// A bulk write is hard to undo, so the panel never leaves you guessing: the
// header names where the selection came from, and everything queued is listed
// as a removable chip above the footer before you commit it.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    CheckIcon,
    ChevronDownIcon,
    Loader2Icon,
    MinusIcon,
    PlusIcon,
    TrashIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import { TextInput } from "@/components/ui/field";
import useUpdateContactsBulk from "@/lib/api/hooks/app/contacts/useUpdateContactsBulk";
import useCustomFieldKeys from "@/lib/api/hooks/app/contacts/useCustomFieldKeys";
import type ContactSelection from "@/lib/api/models/app/contacts/ContactSelection";
import useClickOutside from "@/hooks/useClickOutside";
import { useConfirm } from "@/hooks/context/confirm";
import useFlipPlacement from "@/hooks/useFlipPlacement";
import { CampaignMultiPicker } from "@/components/app/segments/SegmentPickers";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import CategoryPicker from "./CategoryPicker";
import CustomFieldKeyInput from "./CustomFieldKeyInput";

type FieldType = "ADD" | "EDIT" | "DELETE" | "RENAME";
const FIELD_TYPES: { id: FieldType; label: string; hint: string }[] = [
    { id: "ADD", label: "Add", hint: "Only set the key if it's not already present." },
    { id: "EDIT", label: "Edit", hint: "Set the key to this value, overwriting any prior value." },
    { id: "DELETE", label: "Delete", hint: "Remove the key from each selected contact." },
    { id: "RENAME", label: "Rename", hint: "Rename the key — \"value\" is the new key name." },
];

interface Field {
    type: FieldType;
    key: string;
    value: string;
}

// A field operation only travels when it is complete: a key, plus a value for
// every type that needs one. Half-filled rows are kept out of the request the
// same way the contact filter bar drops half-filled custom-field pills.
function isComplete(f: Field): boolean {
    if (!f.key.trim()) return false;
    return f.type === "DELETE" || f.value.trim().length > 0;
}

const plural = (n: number, one: string, many: string) => `${n.toLocaleString()} ${n === 1 ? one : many}`;

export default function ContactsEditBulk({
    active,
    setActive,
    selection,
    count,
    onDone,
    scope,
}: {
    active: boolean;
    setActive: React.Dispatch<React.SetStateAction<boolean>>;
    // Either the ticked ids or the filter behind "select all matching"; the
    // server resolves the second shape.
    selection: ContactSelection;
    count: number;
    onDone?: () => void;
    // Where the selection was made, shown in the header so a bulk write is
    // never applied without knowing which list it came from.
    scope?: { kind: "segment" | "campaign"; name: string };
}) {
    const [campaignsAdd, setCampaignsAdd] = React.useState<string[]>([]);
    const [campaignsRemove, setCampaignsRemove] = React.useState<string[]>([]);
    const [categoriesAdd, setCategoriesAdd] = React.useState<string[]>([]);
    const [categoriesRemove, setCategoriesRemove] = React.useState<string[]>([]);
    const [fields, setFields] = React.useState<Field[]>([]);
    const [subscribeMode, setSubscribeMode] = React.useState<"unchanged" | "subscribe" | "unsubscribe">("unchanged");

    const update = useUpdateContactsBulk();
    const confirm = useConfirm();

    const reset = React.useCallback(() => {
        setCampaignsAdd([]);
        setCampaignsRemove([]);
        setCategoriesAdd([]);
        setCategoriesRemove([]);
        setFields([]);
        setSubscribeMode("unchanged");
    }, []);

    const readyFields = React.useMemo(() => fields.filter(isComplete), [fields]);
    const incomplete = fields.length - readyFields.length;

    // What the Apply button will actually do, in the order it reads. Each entry
    // can be taken back without hunting for the control that set it.
    const changes = React.useMemo(() => {
        const out: { key: string; label: string; clear: () => void }[] = [];
        if (campaignsAdd.length > 0)
            out.push({ key: "ca", label: `Add to ${plural(campaignsAdd.length, "campaign", "campaigns")}`, clear: () => setCampaignsAdd([]) });
        if (campaignsRemove.length > 0)
            out.push({ key: "cr", label: `Remove from ${plural(campaignsRemove.length, "campaign", "campaigns")}`, clear: () => setCampaignsRemove([]) });
        if (categoriesAdd.length > 0)
            out.push({ key: "ka", label: `Add ${plural(categoriesAdd.length, "category", "categories")}`, clear: () => setCategoriesAdd([]) });
        if (categoriesRemove.length > 0)
            out.push({ key: "kr", label: `Remove ${plural(categoriesRemove.length, "category", "categories")}`, clear: () => setCategoriesRemove([]) });
        if (subscribeMode !== "unchanged")
            out.push({ key: "s", label: subscribeMode === "subscribe" ? "Subscribe" : "Unsubscribe", clear: () => setSubscribeMode("unchanged") });
        if (readyFields.length > 0)
            out.push({ key: "f", label: `${plural(readyFields.length, "field change", "field changes")}`, clear: () => setFields([]) });
        return out;
    }, [campaignsAdd, campaignsRemove, categoriesAdd, categoriesRemove, subscribeMode, readyFields]);

    // Anything the user touched guards the close; only complete changes can be
    // applied, so the two are deliberately not the same question.
    const touched = changes.length > 0 || fields.length > 0;
    const applicable = changes.length > 0;

    const requestClose = React.useCallback(() => {
        if (touched) confirm.show("Discard bulk changes?", async () => setActive(false));
        else setActive(false);
    }, [touched, confirm, setActive]);

    async function submit() {
        if (!applicable) return;
        try {
            await toast.promise(
                update.mutateAsync({
                    ...selection,
                    add_campaigns: campaignsAdd,
                    remove_campaigns: campaignsRemove,
                    add_categories: categoriesAdd,
                    remove_categories: categoriesRemove,
                    fields: readyFields,
                    subscribe: subscribeMode === "subscribe" ? true : subscribeMode === "unsubscribe" ? false : undefined,
                }),
                {
                    loading: `Updating ${plural(count, "contact", "contacts")}…`,
                    success: `Updated ${plural(count, "contact", "contacts")}`,
                    error: (err: AppError) => buildError(err),
                },
            );
            reset();
            setActive(false);
            onDone?.();
        } catch {
            /* surfaced by toast.promise */
        }
    }

    React.useEffect(() => {
        if (!active) reset();
    }, [active, reset]);

    // Escape closes the innermost layer only: a picker dropdown or the confirm
    // owns the key while it is on screen.
    React.useEffect(() => {
        if (!active) return;
        function onKey(e: KeyboardEvent) {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            requestClose();
        }
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [requestClose, active]);

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
                    onMouseDown={requestClose}
                >
                    <motion.aside
                        key="panel"
                        role="dialog"
                        aria-modal="true"
                        aria-label="Bulk edit contacts"
                        initial={{ x: 32, opacity: 0 }}
                        animate={{ x: 0, opacity: 1 }}
                        exit={{ x: 32, opacity: 0 }}
                        transition={{ duration: 0.2, ease: [0.32, 0.72, 0, 1] }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="flex flex-col w-[32rem] max-w-[95%] h-full bg-white border-l border-slate-200 shadow-[-12px_0_24px_-12px_rgba(15,23,42,0.08)]"
                    >
                        <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium shrink-0">Bulk edit</span>
                            <div className="h-4 w-px bg-slate-200 shrink-0" />
                            <span className="text-[12.5px] text-slate-900 font-medium shrink-0">
                                {plural(count, "contact", "contacts")}
                            </span>
                            {scope && (
                                <span className="inline-flex items-center h-5 px-1.5 rounded bg-sky-50 text-sky-700 text-[10px] font-medium min-w-0">
                                    <span className="truncate">
                                        in {scope.name}
                                    </span>
                                </span>
                            )}
                            <button
                                type="button"
                                onClick={requestClose}
                                aria-label="Close"
                                className="ml-auto shrink-0 size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </header>

                        <div className="flex-1 min-h-0 overflow-y-auto divide-y divide-slate-100">
                            <Section title="Campaigns" subtitle="Membership changes apply to every selected contact.">
                                <PickerRow direction="add" label="Add to">
                                    <CampaignMultiPicker value={campaignsAdd} onChange={setCampaignsAdd} />
                                </PickerRow>
                                <PickerRow direction="remove" label="Remove from">
                                    <CampaignMultiPicker value={campaignsRemove} onChange={setCampaignsRemove} />
                                </PickerRow>
                            </Section>

                            <Section title="Categories" subtitle="Labels put on the contacts themselves.">
                                <PickerRow direction="add" label="Add">
                                    <CategoryPicker value={categoriesAdd} onChange={setCategoriesAdd} />
                                </PickerRow>
                                <PickerRow direction="remove" label="Remove">
                                    <CategoryPicker
                                        value={categoriesRemove}
                                        onChange={setCategoriesRemove}
                                        allowCreate={false}
                                        placeholder="Pick categories to strip…"
                                    />
                                </PickerRow>
                            </Section>

                            <Section title="Subscription" subtitle="Marketing consent. Leave it alone unless you mean to change it.">
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
                                            aria-pressed={subscribeMode === id}
                                            className={cn(
                                                "h-6 px-2.5 rounded text-[11.5px] font-medium transition-colors",
                                                subscribeMode === id ? "bg-slate-900 text-white" : "text-slate-500 hover:text-slate-900",
                                            )}
                                        >
                                            {label}
                                        </button>
                                    ))}
                                </div>
                            </Section>

                            <Section
                                title="Custom fields"
                                subtitle="Queue as many operations as you need; each runs on every selected contact."
                                accessory={
                                    fields.length < 100 && (
                                        <button
                                            type="button"
                                            onClick={() => setFields((f) => [...f, { type: "ADD", key: "", value: "" }])}
                                            className="h-6 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-[11px] text-slate-600 hover:text-slate-900 inline-flex items-center gap-1 transition-colors"
                                        >
                                            <PlusIcon className="w-3 h-3" />
                                            Add operation
                                        </button>
                                    )
                                }
                            >
                                {fields.length === 0 ? (
                                    <button
                                        type="button"
                                        onClick={() => setFields([{ type: "ADD", key: "", value: "" }])}
                                        className="w-full rounded-md border border-dashed border-slate-200 hover:border-slate-300 px-3 py-4 text-[11.5px] text-slate-400 hover:text-slate-600 transition-colors"
                                    >
                                        No field operations queued. Add one.
                                    </button>
                                ) : (
                                    <div className="space-y-2">
                                        {fields.map((f, idx) => (
                                            <FieldRow
                                                key={idx}
                                                field={f}
                                                onChange={(next) => setFields((cur) => cur.map((c, i) => (i === idx ? next : c)))}
                                                onRemove={() => setFields((cur) => cur.filter((_, i) => i !== idx))}
                                            />
                                        ))}
                                    </div>
                                )}
                            </Section>
                        </div>

                        {/* Everything that is about to happen, in one place, each
                            item removable. Bulk writes are hard to walk back. */}
                        <div className="shrink-0 border-t border-slate-200 bg-slate-50/60 px-4 py-2.5">
                            <div className="flex items-center gap-2 mb-1.5">
                                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Will apply</span>
                                {incomplete > 0 && (
                                    <span className="text-[10.5px] text-amber-700">
                                        {plural(incomplete, "field operation is", "field operations are")} incomplete and will be skipped
                                    </span>
                                )}
                            </div>
                            {changes.length === 0 ? (
                                <p className="text-[11.5px] text-slate-400">Nothing yet. Pick a change above.</p>
                            ) : (
                                <div className="flex flex-wrap gap-1">
                                    {changes.map((c) => (
                                        <span
                                            key={c.key}
                                            className="inline-flex items-center gap-1 h-5 pl-1.5 pr-1 rounded border border-slate-200 bg-white text-[11px] font-medium text-slate-700"
                                        >
                                            {c.label}
                                            <button
                                                type="button"
                                                onClick={c.clear}
                                                aria-label={`Undo: ${c.label}`}
                                                className="text-slate-400 hover:text-slate-900 transition-colors"
                                            >
                                                <XIcon className="w-2.5 h-2.5" />
                                            </button>
                                        </span>
                                    ))}
                                </div>
                            )}
                        </div>

                        <footer className="h-12 px-3 border-t border-slate-200 flex items-center gap-1.5 shrink-0 bg-white">
                            <button
                                type="button"
                                onClick={reset}
                                disabled={!touched}
                                className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-40 disabled:hover:bg-transparent"
                            >
                                Discard
                            </button>
                            <button
                                type="button"
                                onClick={submit}
                                disabled={!applicable || update.isPending}
                                title={applicable ? undefined : "Pick at least one change to apply"}
                                className="ml-auto shrink-0 whitespace-nowrap h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                            >
                                {update.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CheckIcon className="w-3 h-3" />}
                                {applicable
                                    ? `Apply ${plural(changes.length, "change", "changes")} to ${count.toLocaleString()}`
                                    : `Apply to ${count.toLocaleString()}`}
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
        <section className="px-5 py-4">
            <div className="flex items-start gap-2 mb-2.5">
                <div className="flex-1 min-w-0">
                    <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500">{title}</h2>
                    {subtitle && <p className="text-[11px] text-slate-400 leading-tight mt-0.5">{subtitle}</p>}
                </div>
                {accessory && <div className="shrink-0">{accessory}</div>}
            </div>
            <div className="space-y-2.5">{children}</div>
        </section>
    );
}

// Add and remove sit side by side under one heading, told apart by a small
// tinted glyph rather than shouting the whole label in green and red.
function PickerRow({
    direction,
    label,
    children,
}: {
    direction: "add" | "remove";
    label: string;
    children: React.ReactNode;
}) {
    return (
        <div className="flex items-start gap-2">
            <span
                className={cn(
                    "mt-0.5 shrink-0 inline-flex items-center gap-1 h-7 px-1.5 rounded text-[10px] uppercase tracking-[0.1em] font-medium w-[92px]",
                    direction === "add" ? "text-emerald-700 bg-emerald-50/70" : "text-rose-700 bg-rose-50/70",
                )}
            >
                {direction === "add" ? <PlusIcon className="w-2.5 h-2.5 shrink-0" /> : <MinusIcon className="w-2.5 h-2.5 shrink-0" />}
                <span className="truncate">{label}</span>
            </span>
            <div className="flex-1 min-w-0">{children}</div>
        </div>
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
    const triggerRef = React.useRef<HTMLButtonElement>(null);
    useClickOutside(dropRef, () => setShowType(false));
    const typePlacement = useFlipPlacement(triggerRef, showType, 180);
    const { data: existingKeys = [] } = useCustomFieldKeys();
    const typeDef = FIELD_TYPES.find((t) => t.id === field.type)!;
    const needsValue = field.type !== "DELETE";
    const missing = !field.key.trim() ? "key" : needsValue && !field.value.trim() ? (field.type === "RENAME" ? "new key name" : "value") : null;

    return (
        <div className={cn("rounded-md border bg-white p-2.5 space-y-2", missing ? "border-amber-200" : "border-slate-200")}>
            <div className="flex items-center gap-1.5">
                <div ref={dropRef} className="relative shrink-0">
                    <button
                        ref={triggerRef}
                        type="button"
                        onClick={() => setShowType((s) => !s)}
                        aria-expanded={showType}
                        className="h-7 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-[11.5px] font-medium text-slate-700 inline-flex items-center gap-1 transition-colors"
                    >
                        {typeDef.label}
                        <ChevronDownIcon className="w-3 h-3 text-slate-400" />
                    </button>
                    <AnimatePresence>
                        {showType && (
                            <motion.div
                                data-floating
                                initial={{ opacity: 0, y: typePlacement === "top" ? 4 : -4 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, y: typePlacement === "top" ? 4 : -4 }}
                                transition={{ duration: 0.12 }}
                                className={cn(
                                    "absolute left-0 z-30 w-56 rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] py-1",
                                    typePlacement === "top" ? "bottom-full mb-1" : "top-full mt-1",
                                )}
                            >
                                {FIELD_TYPES.map((t) => (
                                    <button
                                        key={t.id}
                                        type="button"
                                        onClick={() => {
                                            onChange({ ...field, type: t.id });
                                            setShowType(false);
                                        }}
                                        className={cn(
                                            "w-full px-2.5 py-1.5 text-left hover:bg-slate-100 transition-colors",
                                            t.id === field.type && "bg-slate-50",
                                        )}
                                    >
                                        <div className="text-[11.5px] text-slate-900 font-medium leading-tight">{t.label}</div>
                                        <div className="text-[10.5px] text-slate-500 leading-tight mt-0.5">{t.hint}</div>
                                    </button>
                                ))}
                            </motion.div>
                        )}
                    </AnimatePresence>
                </div>
                <CustomFieldKeyInput
                    value={field.key}
                    onChange={(v) => onChange({ ...field, key: v })}
                    keys={existingKeys}
                    placeholder="key"
                    className="flex-1"
                />
                {/* The value slot keeps its width on DELETE so switching type
                    does not shuffle the row underneath the pointer. */}
                <div className="flex-1">
                    {needsValue ? (
                        <TextInput
                            value={field.value}
                            onChange={(v) => onChange({ ...field, value: v })}
                            placeholder={field.type === "RENAME" ? "new key name" : "value"}
                            className="w-full"
                        />
                    ) : (
                        <div className="h-7 rounded-md border border-dashed border-slate-200 px-2 flex items-center text-[11px] text-slate-400">
                            no value needed
                        </div>
                    )}
                </div>
                <button
                    type="button"
                    onClick={onRemove}
                    aria-label="Remove operation"
                    className="size-7 rounded-md text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors shrink-0"
                >
                    <TrashIcon className="w-3 h-3" />
                </button>
            </div>
            <p className={cn("text-[10.5px] leading-tight", missing ? "text-amber-700" : "text-slate-400")}>
                {missing ? `Needs a ${missing}; skipped until then.` : typeDef.hint}
            </p>
        </div>
    );
}
