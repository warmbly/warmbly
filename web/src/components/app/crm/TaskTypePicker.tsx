// TaskTypePicker — choose a task type, and manage the org's types inline:
// create, rename, recolour, delete. The selected value is the type NAME
// (empty string = no type). Shared by the tasks dialog and the campaign
// "Create task" action editor so types stay consistent everywhere.
//
// This mirrors the canonical contacts CategoryPicker: a bordered trigger
// plus a framer-motion dropdown with a typeahead SEARCH HEADER at the top,
// single-select rows (coloured dot + name + check), and an inline
// "Create '<query>'" row that appears when the search doesn't match an
// existing type — that inline create is the primary way to add a type, far
// smoother than a separate form. Per-row recolour/rename/delete affordances
// reveal on hover and stay lightweight and inline.
//
// closeOnSelect: selecting a type (or "No type", or creating a new one)
// closes the dropdown; recolour / rename / delete keep it open. Editing
// interactions stopPropagation so typing or picking a swatch never bubbles
// up to a row's select handler.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import {
    CheckIcon,
    ChevronDownIcon,
    Loader2Icon,
    PencilIcon,
    PlusIcon,
    Trash2Icon,
} from "lucide-react";

import { useConfirm } from "@/hooks/context/confirm";
import useClickOutside from "@/hooks/useClickOutside";
import useFlipPlacement from "@/hooks/useFlipPlacement";
import useTaskTypes from "@/lib/api/hooks/app/crm/taskTypes/useTaskTypes";
import useCreateTaskType from "@/lib/api/hooks/app/crm/taskTypes/useCreateTaskType";
import useUpdateTaskType from "@/lib/api/hooks/app/crm/taskTypes/useUpdateTaskType";
import useDeleteTaskType from "@/lib/api/hooks/app/crm/taskTypes/useDeleteTaskType";
import { TASK_TYPE_COLORS } from "./taskTypes";
import type TaskType from "@/lib/api/models/app/crm/TaskType";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export default function TaskTypePicker({
    value,
    onChange,
    className,
}: {
    value: string;
    onChange: (name: string) => void;
    className?: string;
}) {
    const { data: types = [], isPending } = useTaskTypes();
    const createType = useCreateTaskType();

    const [open, setOpen] = React.useState(false);
    const [query, setQuery] = React.useState("");
    const ref = React.useRef<HTMLDivElement>(null);
    const triggerRef = React.useRef<HTMLButtonElement>(null);
    useClickOutside(ref, () => setOpen(false));
    // ~290px: 33px search input + 56 max-h list (224px) + the "No type"
    // row + borders, so the flip kicks in before a tall list clips.
    const placement = useFlipPlacement(triggerRef, open, 290);

    const selected = types.find((t) => t.name === value);

    React.useEffect(() => {
        if (!open) setQuery("");
    }, [open]);

    const filtered = React.useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q) return types;
        return types.filter((t) => t.name.toLowerCase().includes(q));
    }, [types, query]);

    const queryMatchesExisting = React.useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q) return true;
        return types.some((t) => t.name.toLowerCase() === q);
    }, [types, query]);

    function pick(name: string) {
        onChange(name);
        setOpen(false);
    }

    // Auto-assign the next palette colour, preferring one not already in
    // use so freshly created types are visually distinct.
    function nextColor(): string {
        const used = new Set(types.map((t) => t.color));
        return TASK_TYPE_COLORS.find((c) => !used.has(c)) ?? TASK_TYPE_COLORS[0];
    }

    async function createAndPick() {
        const name = query.trim();
        if (!name) return;
        try {
            const t = await createType.mutateAsync({ name, color: nextColor() });
            onChange(t.name);
            setQuery("");
            setOpen(false);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    return (
        <div ref={ref} className={`relative ${className ?? ""}`}>
            <button
                ref={triggerRef}
                type="button"
                onClick={() => setOpen((o) => !o)}
                className="h-7 w-full px-2.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors"
            >
                <span
                    className="size-2 rounded-full shrink-0"
                    style={{ backgroundColor: selected?.color ?? "#cbd5e1" }}
                />
                <span className="truncate flex-1 text-left">{value || "No type"}</span>
                <ChevronDownIcon className="w-3 h-3 text-slate-400" />
            </button>

            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        transition={{ duration: 0.12 }}
                        className={`absolute left-0 right-0 z-30 rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] overflow-hidden ${
                            placement === "top" ? "bottom-full mb-1" : "top-full mt-1"
                        }`}
                    >
                        <div className="px-2 py-1.5 border-b border-slate-200">
                            <input
                                value={query}
                                onChange={(e) => setQuery(e.target.value)}
                                onKeyDown={(e) => {
                                    if (
                                        e.key === "Enter" &&
                                        query.trim() &&
                                        !queryMatchesExisting
                                    ) {
                                        e.preventDefault();
                                        createAndPick();
                                    }
                                    if (e.key === "Escape") {
                                        e.stopPropagation();
                                        setOpen(false);
                                    }
                                }}
                                placeholder="Search or create…"
                                autoFocus
                                className="w-full h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                            />
                        </div>

                        <div className="max-h-56 overflow-y-auto py-1">
                            <button
                                type="button"
                                onClick={() => pick("")}
                                className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-100 transition-colors"
                            >
                                <span className="size-2.5 rounded-full shrink-0 bg-slate-300" />
                                <span className="truncate flex-1 text-left">No type</span>
                                {value === "" && (
                                    <CheckIcon className="w-3 h-3 text-sky-600 shrink-0" />
                                )}
                            </button>

                            {isPending ? (
                                <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">
                                    Loading…
                                </div>
                            ) : (
                                filtered.map((t) => (
                                    <TypeRow
                                        key={t.id}
                                        type={t}
                                        selected={t.name === value}
                                        usedColors={types.map((x) => x.color)}
                                        onSelect={() => pick(t.name)}
                                        onRenamedSelected={(name) => onChange(name)}
                                        onDeletedSelected={() => onChange("")}
                                    />
                                ))
                            )}

                            {query.trim() && !queryMatchesExisting && (
                                <button
                                    type="button"
                                    onClick={createAndPick}
                                    disabled={createType.isPending}
                                    className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-900 font-medium hover:bg-sky-50 border-t border-slate-100 transition-colors disabled:opacity-60"
                                >
                                    {createType.isPending ? (
                                        <Loader2Icon className="w-3 h-3 animate-spin text-slate-400" />
                                    ) : (
                                        <PlusIcon className="w-3 h-3 text-sky-600" />
                                    )}
                                    Create "{query.trim()}"
                                </button>
                            )}

                            {!isPending &&
                                filtered.length === 0 &&
                                !query.trim() && (
                                    <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">
                                        No types yet. Type a name to create one.
                                    </div>
                                )}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

// TypeRow — a single existing type. Click anywhere to select; hovering
// reveals lightweight recolour / rename / delete affordances. The recolour
// swatch popover and the inline rename input stopPropagation so they never
// trigger select or close the dropdown.
function TypeRow({
    type,
    selected,
    usedColors,
    onSelect,
    onRenamedSelected,
    onDeletedSelected,
}: {
    type: TaskType;
    selected: boolean;
    usedColors: string[];
    onSelect: () => void;
    onRenamedSelected: (name: string) => void;
    onDeletedSelected: () => void;
}) {
    const update = useUpdateTaskType();
    const del = useDeleteTaskType();
    const confirm = useConfirm();

    const [renaming, setRenaming] = React.useState(false);
    const [name, setName] = React.useState(type.name);
    const [swatches, setSwatches] = React.useState(false);

    React.useEffect(() => {
        setName(type.name);
    }, [type.name]);

    async function saveName() {
        const next = name.trim();
        if (!next || next === type.name) {
            setRenaming(false);
            setName(type.name);
            return;
        }
        try {
            await update.mutateAsync({ id: type.id, data: { name: next } });
            if (selected) onRenamedSelected(next);
            setRenaming(false);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    async function recolour(color: string) {
        setSwatches(false);
        if (color === type.color) return;
        try {
            await update.mutateAsync({ id: type.id, data: { color } });
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    function remove(e: React.MouseEvent) {
        e.stopPropagation();
        confirm?.show(
            `Delete the "${type.name}" type? Existing tasks keep the label.`,
            async () => {
                try {
                    await del.mutateAsync(type.id);
                    if (selected) onDeletedSelected();
                } catch (e) {
                    toast.error(buildError(e as AppError));
                }
            },
        );
    }

    // Inline rename — a tight input that replaces the row label. Stops
    // propagation so typing never bubbles to the row's select handler.
    if (renaming) {
        return (
            <div
                className="px-2.5 h-7 flex items-center gap-2 bg-slate-50"
                onClick={(e) => e.stopPropagation()}
            >
                <span
                    className="size-2.5 rounded-full shrink-0"
                    style={{ backgroundColor: type.color }}
                />
                <input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    autoFocus
                    onBlur={saveName}
                    onKeyDown={(e) => {
                        if (e.key === "Enter") {
                            e.preventDefault();
                            saveName();
                        }
                        if (e.key === "Escape") {
                            e.stopPropagation();
                            setName(type.name);
                            setRenaming(false);
                        }
                    }}
                    className="flex-1 h-5 bg-transparent text-[12px] text-slate-900 outline-none"
                />
                {update.isPending && (
                    <Loader2Icon className="w-3 h-3 animate-spin text-slate-400 shrink-0" />
                )}
            </div>
        );
    }

    return (
        <div
            className={`group relative px-2.5 h-7 flex items-center gap-2 text-[12px] cursor-pointer transition-colors hover:bg-slate-100 ${
                selected ? "text-slate-900 font-medium" : "text-slate-700"
            }`}
            onClick={onSelect}
        >
            <button
                type="button"
                onClick={(e) => {
                    e.stopPropagation();
                    setSwatches((s) => !s);
                }}
                aria-label="Recolour type"
                className="size-2.5 rounded-full shrink-0 ring-offset-1 hover:ring-2 hover:ring-slate-300 transition-shadow"
                style={{ backgroundColor: type.color }}
            />
            <span className="truncate flex-1">{type.name}</span>

            {selected && <CheckIcon className="w-3 h-3 text-sky-600 shrink-0" />}

            <button
                type="button"
                onClick={(e) => {
                    e.stopPropagation();
                    setRenaming(true);
                }}
                aria-label="Rename type"
                className="size-5 rounded text-slate-400 hover:text-slate-700 hover:bg-slate-200/70 inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-opacity"
            >
                <PencilIcon className="w-2.5 h-2.5" />
            </button>
            <button
                type="button"
                onClick={remove}
                aria-label="Delete type"
                className="size-5 rounded text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-opacity"
            >
                <Trash2Icon className="w-2.5 h-2.5" />
            </button>

            <AnimatePresence>
                {swatches && (
                    <SwatchPopover
                        value={type.color}
                        usedColors={usedColors}
                        onPick={recolour}
                        onClose={() => setSwatches(false)}
                    />
                )}
            </AnimatePresence>
        </div>
    );
}

// SwatchPopover — a small floating palette anchored to the type's colour
// dot. Lives inside the row; stops propagation so picking a colour never
// selects the row or closes the parent dropdown. Click-outside (scoped to
// this popover) dismisses it.
function SwatchPopover({
    value,
    usedColors,
    onPick,
    onClose,
}: {
    value: string;
    usedColors: string[];
    onPick: (color: string) => void;
    onClose: () => void;
}) {
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, onClose);

    return (
        <motion.div
            ref={ref}
            initial={{ opacity: 0, scale: 0.96 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.96 }}
            transition={{ duration: 0.1 }}
            onClick={(e) => e.stopPropagation()}
            className="absolute left-1.5 top-full mt-1 z-40 rounded-md border border-slate-200 bg-white p-1.5 shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)]"
        >
            <div className="grid grid-cols-4 gap-1.5">
                {TASK_TYPE_COLORS.map((c) => {
                    const isCurrent = c === value;
                    const inUse = !isCurrent && usedColors.includes(c);
                    return (
                        <button
                            key={c}
                            type="button"
                            onClick={() => onPick(c)}
                            aria-label={c}
                            className={`relative size-5 rounded-full transition-transform ${
                                isCurrent
                                    ? "ring-2 ring-offset-1 ring-slate-900"
                                    : "hover:scale-110"
                            } ${inUse ? "opacity-60" : ""}`}
                            style={{ backgroundColor: c }}
                        >
                            {isCurrent && (
                                <CheckIcon className="absolute inset-0 m-auto w-2.5 h-2.5 text-white" />
                            )}
                        </button>
                    );
                })}
            </div>
        </motion.div>
    );
}
