// Generic list modal — used by Folders and Tags (same shape).
//
// Each item is {id, title, color}. The modal renders:
//
//   [eyebrow] · description  …………………………………………………  [×]
//   ─────────────────────────────────────────────────────────────
//   ● Title                                            edit del
//   ─────────────────────────────────────────────────────────────
//   ◐ NAME [______________________________]
//   ◐ COLOR  ● ● ● ● ● ● ● ●                      [Cancel][Add]
//   ─────────────────────────────────────────────────────────────
//   + Add Folder                                          [Done]
//
// The color picker is rendered INLINE in the add/edit row instead of
// inside a popover — previous popover often got clipped by the modal's
// overflow-y-auto container, so the user couldn't see it at all. With
// the palette inline you always see the swatches and which one is
// selected (ring on the active swatch).

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, Loader2Icon, PencilIcon, PlusIcon, TrashIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { TextInput, Label } from "@/components/ui/field";
import { useConfirm } from "@/hooks/context/confirm";
import { cn } from "@/lib/utils";

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

/**
 * Inline swatch strip — a row of clickable circles. The selected one
 * gets a slate-900 ring so it's unmistakably the active choice.
 */
function ColorStrip({
    value,
    onChange,
}: {
    value: string;
    onChange: (c: string) => void;
}) {
    return (
        <div className="flex items-center gap-1.5">
            {PALETTE.map((p) => {
                const active = value.toLowerCase() === p.toLowerCase();
                return (
                    <button
                        key={p}
                        type="button"
                        onClick={() => onChange(p)}
                        aria-label={`Color ${p}`}
                        aria-pressed={active}
                        className={cn(
                            "size-5 rounded-full flex items-center justify-center transition-shadow",
                            active
                                ? "ring-2 ring-slate-900 ring-offset-1 ring-offset-white"
                                : "hover:ring-2 hover:ring-slate-300 hover:ring-offset-1 hover:ring-offset-white",
                        )}
                    >
                        <span
                            className="size-4 rounded-full"
                            style={{ backgroundColor: p }}
                        />
                    </button>
                );
            })}
        </div>
    );
}

/**
 * Two-line label form used by both add and edit. Caller wires the
 * values and the action buttons; this just renders the fields.
 */
function LabelForm({
    title,
    onTitleChange,
    color,
    onColorChange,
    onSubmit,
    onCancel,
    actions,
}: {
    title: string;
    onTitleChange: (t: string) => void;
    color: string;
    onColorChange: (c: string) => void;
    onSubmit: () => void;
    onCancel: () => void;
    actions: React.ReactNode;
}) {
    return (
        <div className="px-4 py-3 bg-slate-50/60 space-y-2.5">
            <div className="flex items-center gap-3">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium w-12 shrink-0">
                    Name
                </span>
                <TextInput
                    value={title}
                    onChange={onTitleChange}
                    placeholder="Name…"
                    autoFocus
                    className="flex-1"
                    onKeyDown={(e) => {
                        if (e.key === "Enter") onSubmit();
                        if (e.key === "Escape") onCancel();
                    }}
                />
            </div>
            <div className="flex items-center gap-3">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium w-12 shrink-0">
                    Color
                </span>
                <ColorStrip value={color} onChange={onColorChange} />
            </div>
            <div className="flex items-center gap-1.5 pt-1.5">{actions}</div>
        </div>
    );
}

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
    const [newColor, setNewColor] = React.useState<string>(PALETTE[0]);
    const [creating, setCreating] = React.useState(false);

    React.useEffect(() => {
        if (!open) {
            setAdding(false);
            setNewTitle("");
        }
    }, [open]);

    React.useEffect(() => {
        if (adding) {
            // Rotate default color so consecutive adds aren't all the
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
                        className="w-full max-w-[480px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[80dvh]"
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
                                    <LabelForm
                                        title={newTitle}
                                        onTitleChange={setNewTitle}
                                        color={newColor}
                                        onColorChange={setNewColor}
                                        onSubmit={commitAdd}
                                        onCancel={() => {
                                            setAdding(false);
                                            setNewTitle("");
                                        }}
                                        actions={
                                            <>
                                                <button
                                                    type="button"
                                                    onClick={() => {
                                                        setAdding(false);
                                                        setNewTitle("");
                                                    }}
                                                    className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                                                >
                                                    Cancel
                                                </button>
                                                <button
                                                    type="button"
                                                    onClick={commitAdd}
                                                    disabled={creating}
                                                    className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                                >
                                                    {creating ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CheckIcon className="w-3 h-3" />}
                                                    Add
                                                </button>
                                            </>
                                        }
                                    />
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
            <LabelForm
                title={title}
                onTitleChange={setTitle}
                color={color}
                onColorChange={setColor}
                onSubmit={save}
                onCancel={() => setEditing(false)}
                actions={
                    <>
                        <button
                            type="button"
                            onClick={() => confirm?.show(`Delete "${item.title}"?`, remove)}
                            disabled={deleting}
                            className="h-7 px-2.5 rounded-md text-[12px] text-red-600 hover:text-white hover:bg-red-600 font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                        >
                            {deleting ? (
                                <Loader2Icon className="w-3 h-3 animate-spin" />
                            ) : (
                                <TrashIcon className="w-3 h-3" />
                            )}
                            Delete
                        </button>
                        <button
                            type="button"
                            onClick={() => setEditing(false)}
                            className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                        >
                            Cancel
                        </button>
                        <button
                            type="button"
                            onClick={save}
                            disabled={saving || !isDirty}
                            className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                        >
                            {saving ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CheckIcon className="w-3 h-3" />}
                            Save
                        </button>
                    </>
                }
            />
        );
    }

    return (
        <div className="group px-3 h-10 flex items-center gap-2.5 hover:bg-slate-50/80 transition-colors">
            <span aria-hidden className="size-2 rounded-full shrink-0" style={{ backgroundColor: item.color }} />
            <span className="text-[12.5px] text-slate-900 font-medium truncate flex-1">{item.title}</span>
            <div className="flex items-center gap-0.5 opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-opacity">
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
