// Generic list modal — used by Folders and Tags (same shape).
//
// Each item is {id, title, color}. The modal renders:
//
//   [eyebrow] · description  …………………………………………………  [×]
//   ─────────────────────────────────────────────────────────────
//   ● Title                                            edit ⋮
//   ─────────────────────────────────────────────────────────────
//   + Add Folder
//   ─────────────────────────────────────────────────────────────
//   [Done]
//
// Inline edit mode swaps the row in place: color swatch popover +
// title input + Save / Cancel. No drag-and-drop here — keeping it
// simple; ordering can come back as up/down arrows if requested.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, Loader2Icon, PencilIcon, PlusIcon, TrashIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { TextInput, Label } from "@/components/ui/field";
import { useConfirm } from "@/hooks/context/confirm";

export interface LabelItem {
    id: string;
    title: string;
    color: string;
    position: number;
}

interface Props {
    open: boolean;
    onClose: () => void;
    eyebrow: string;
    subtitle: string;
    items: LabelItem[];
    onCreate: (title: string, color: string) => Promise<unknown>;
    onUpdate: (id: string, data: { title?: string; color?: string }) => Promise<unknown>;
    onDelete: (id: string) => Promise<unknown>;
    addCta: string;
    max?: number;
}

const PALETTE = [
    "#94a3b8", // slate
    "#38bdf8", // sky
    "#10b981", // emerald
    "#f59e0b", // amber
    "#ef4444", // red
    "#a855f7", // violet
    "#ec4899", // pink
    "#14b8a6", // teal
];

export function LabelListModal({
    open,
    onClose,
    eyebrow,
    subtitle,
    items,
    onCreate,
    onUpdate,
    onDelete,
    addCta,
    max = 50,
}: Props) {
    const [adding, setAdding] = React.useState(false);
    const [newTitle, setNewTitle] = React.useState("");
    // Pick the next palette color in rotation when starting an add. The
    // user can change it via the swatch popover before committing.
    const [newColor, setNewColor] = React.useState<string>(PALETTE[0]);
    const [paletteOpen, setPaletteOpen] = React.useState(false);
    const [creating, setCreating] = React.useState(false);

    React.useEffect(() => {
        if (!open) {
            setAdding(false);
            setNewTitle("");
            setPaletteOpen(false);
        }
    }, [open]);

    React.useEffect(() => {
        if (adding) {
            // Rotate the default color so consecutive adds aren't all the
            // same swatch (purely cosmetic, the user can still override).
            setNewColor(PALETTE[items.length % PALETTE.length]);
        }
    }, [adding, items.length]);

    async function commitAdd() {
        const t = newTitle.trim();
        if (t.length < 1) {
            toast.error("Name is required");
            return;
        }
        setCreating(true);
        try {
            await toast.promise(onCreate(t, newColor), {
                loading: "Creating…",
                success: "Created",
                error: (e: AppError) => buildError(e),
            });
            setNewTitle("");
            setAdding(false);
        } catch {
            /* surfaced by toast.promise */
        } finally {
            setCreating(false);
        }
    }

    const sorted = React.useMemo(
        () => [...items].sort((a, b) => a.position - b.position),
        [items],
    );

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
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.16 }}
                        onClick={(e) => e.stopPropagation()}
                        className="w-full max-w-[480px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[80vh]"
                    >
                        {/* Header */}
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                {eyebrow}
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12px] text-slate-600 truncate">{subtitle}</span>
                            <button
                                type="button"
                                onClick={onClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        {/* Body */}
                        <div className="flex-1 min-h-0 overflow-y-auto">
                            {sorted.length === 0 && !adding && (
                                <div className="px-5 py-10 text-center">
                                    <p className="text-[12.5px] text-slate-700 font-medium mb-1">
                                        Nothing here yet
                                    </p>
                                    <p className="text-[11.5px] text-slate-400">
                                        Add your first to get started.
                                    </p>
                                </div>
                            )}
                            <div className="divide-y divide-slate-200/60">
                                {sorted.map((item) => (
                                    <ItemRow
                                        key={item.id}
                                        item={item}
                                        onUpdate={onUpdate}
                                        onDelete={onDelete}
                                    />
                                ))}
                                {adding && (
                                    <div className="px-3 py-2 flex items-center gap-2 bg-slate-50/60">
                                        <div className="relative shrink-0">
                                            <button
                                                type="button"
                                                onClick={() => setPaletteOpen((o) => !o)}
                                                className="size-5 rounded-md border border-slate-200 hover:border-slate-300 transition-colors flex items-center justify-center"
                                                aria-label="Pick color"
                                            >
                                                <span className="size-3 rounded-full" style={{ backgroundColor: newColor }} />
                                            </button>
                                            {paletteOpen && (
                                                <div
                                                    className="absolute z-10 top-7 left-0 rounded-md bg-white border border-slate-200 shadow-[0_4px_12px_-2px_rgba(15,23,42,0.08)] p-1.5 grid grid-cols-4 gap-1"
                                                    onClick={(e) => e.stopPropagation()}
                                                >
                                                    {PALETTE.map((p) => (
                                                        <button
                                                            key={p}
                                                            type="button"
                                                            onClick={() => {
                                                                setNewColor(p);
                                                                setPaletteOpen(false);
                                                            }}
                                                            className="size-6 rounded flex items-center justify-center hover:bg-slate-50 transition-colors"
                                                            aria-label={`Color ${p}`}
                                                        >
                                                            <span className="size-3.5 rounded-full" style={{ backgroundColor: p }} />
                                                        </button>
                                                    ))}
                                                </div>
                                            )}
                                        </div>
                                        <TextInput
                                            value={newTitle}
                                            onChange={setNewTitle}
                                            placeholder="Name…"
                                            autoFocus
                                            className="flex-1"
                                            onKeyDown={(e) => {
                                                if (e.key === "Enter") commitAdd();
                                                if (e.key === "Escape") setAdding(false);
                                            }}
                                        />
                                        <button
                                            type="button"
                                            onClick={commitAdd}
                                            disabled={creating}
                                            className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                        >
                                            {creating ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CheckIcon className="w-3 h-3" />}
                                            Add
                                        </button>
                                        <button
                                            type="button"
                                            onClick={() => {
                                                setAdding(false);
                                                setNewTitle("");
                                                setPaletteOpen(false);
                                            }}
                                            className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                                            aria-label="Cancel"
                                        >
                                            <XIcon className="w-3.5 h-3.5" />
                                        </button>
                                    </div>
                                )}
                            </div>
                        </div>

                        {/* Footer */}
                        <div className="px-3 h-12 border-t border-slate-200 flex items-center gap-1.5 shrink-0">
                            {!adding && sorted.length < max && (
                                <button
                                    type="button"
                                    onClick={() => setAdding(true)}
                                    className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors"
                                >
                                    <PlusIcon className="w-3 h-3" />
                                    {addCta}
                                </button>
                            )}
                            {sorted.length >= max && (
                                <span className="text-[11px] text-slate-400">
                                    Maximum of {max} items reached
                                </span>
                            )}
                            <button
                                type="button"
                                onClick={onClose}
                                className="ml-auto h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium transition-colors"
                            >
                                Done
                            </button>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function ItemRow({
    item,
    onUpdate,
    onDelete,
}: {
    item: LabelItem;
    onUpdate: (id: string, data: { title?: string; color?: string }) => Promise<unknown>;
    onDelete: (id: string) => Promise<unknown>;
}) {
    const confirm = useConfirm();
    const [editing, setEditing] = React.useState(false);
    const [title, setTitle] = React.useState(item.title);
    const [color, setColor] = React.useState(item.color);
    const [saving, setSaving] = React.useState(false);
    const [deleting, setDeleting] = React.useState(false);
    const [paletteOpen, setPaletteOpen] = React.useState(false);

    React.useEffect(() => {
        if (!editing) {
            setTitle(item.title);
            setColor(item.color);
        }
    }, [editing, item.title, item.color]);

    const isDirty = title.trim() !== item.title || color !== item.color;

    async function save() {
        if (!isDirty) {
            setEditing(false);
            return;
        }
        const t = title.trim();
        if (t.length < 1) {
            toast.error("Name is required");
            return;
        }
        setSaving(true);
        try {
            await toast.promise(
                onUpdate(item.id, {
                    title: t !== item.title ? t : undefined,
                    color: color !== item.color ? color : undefined,
                }),
                {
                    loading: "Saving…",
                    success: "Saved",
                    error: (e: AppError) => buildError(e),
                },
            );
            setEditing(false);
        } catch {
            /* surfaced */
        } finally {
            setSaving(false);
        }
    }

    async function remove() {
        setDeleting(true);
        try {
            await toast.promise(onDelete(item.id), {
                loading: "Deleting…",
                success: "Deleted",
                error: (e: AppError) => buildError(e),
            });
        } catch {
            /* surfaced */
        } finally {
            setDeleting(false);
            confirm?.setShow(false);
        }
    }

    if (editing) {
        return (
            <div className="px-3 py-2 flex items-center gap-2 bg-slate-50/60">
                <div className="relative shrink-0">
                    <button
                        type="button"
                        onClick={() => setPaletteOpen((o) => !o)}
                        className="size-5 rounded-md border border-slate-200 hover:border-slate-300 transition-colors flex items-center justify-center"
                        aria-label="Pick color"
                    >
                        <span className="size-3 rounded-full" style={{ backgroundColor: color }} />
                    </button>
                    {paletteOpen && (
                        <div
                            className="absolute z-10 top-7 left-0 rounded-md bg-white border border-slate-200 shadow-[0_4px_12px_-2px_rgba(15,23,42,0.08)] p-1.5 grid grid-cols-4 gap-1"
                            onClick={(e) => e.stopPropagation()}
                        >
                            {PALETTE.map((p) => (
                                <button
                                    key={p}
                                    type="button"
                                    onClick={() => {
                                        setColor(p);
                                        setPaletteOpen(false);
                                    }}
                                    className="size-6 rounded flex items-center justify-center hover:bg-slate-50 transition-colors"
                                    aria-label={`Color ${p}`}
                                >
                                    <span className="size-3.5 rounded-full" style={{ backgroundColor: p }} />
                                </button>
                            ))}
                        </div>
                    )}
                </div>
                <TextInput
                    value={title}
                    onChange={setTitle}
                    className="flex-1"
                    autoFocus
                    onKeyDown={(e) => {
                        if (e.key === "Enter") save();
                        if (e.key === "Escape") setEditing(false);
                    }}
                />
                <button
                    type="button"
                    onClick={save}
                    disabled={saving}
                    className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                >
                    {saving ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CheckIcon className="w-3 h-3" />}
                    Save
                </button>
                <button
                    type="button"
                    onClick={() => setEditing(false)}
                    className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                    aria-label="Cancel"
                >
                    <XIcon className="w-3.5 h-3.5" />
                </button>
            </div>
        );
    }

    return (
        <div className="group px-3 h-10 flex items-center gap-2.5 hover:bg-slate-50/80 transition-colors">
            <span aria-hidden className="size-2 rounded-full shrink-0" style={{ backgroundColor: item.color }} />
            <span className="text-[12.5px] text-slate-900 font-medium truncate flex-1">{item.title}</span>
            <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
                <button
                    type="button"
                    onClick={() => setEditing(true)}
                    className="size-6 rounded text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                    aria-label="Edit"
                >
                    <PencilIcon className="w-3 h-3" />
                </button>
                <button
                    type="button"
                    onClick={() => confirm?.show(`Delete "${item.title}"?`, remove)}
                    disabled={deleting}
                    className="size-6 rounded text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors disabled:opacity-50"
                    aria-label="Delete"
                >
                    {deleting ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <TrashIcon className="w-3 h-3" />}
                </button>
            </div>
        </div>
    );
}

// Re-export Label so callers can do single-import.
export { Label };
