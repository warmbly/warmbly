// TaskTypePicker — choose a task type, and manage the org's types inline:
// create, rename, recolour, delete. The selected value is the type NAME
// (empty string = no type). Shared by the tasks dialog and the campaign
// "Create task" action editor so types stay consistent everywhere.

import React from "react";
import toast from "react-hot-toast";
import {
    CheckIcon,
    ChevronDownIcon,
    Loader2Icon,
    PencilIcon,
    PlusIcon,
    Trash2Icon,
    XIcon,
} from "lucide-react";
import useClickOutside from "@/hooks/useClickOutside";
import { useConfirm } from "@/hooks/context/confirm";
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
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));

    const selected = types.find((t) => t.name === value);

    return (
        <div ref={ref} className={`relative ${className ?? ""}`}>
            <button
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

            {open && (
                <div className="absolute left-0 top-full z-40 mt-1 w-64 rounded-md border border-slate-200 bg-white py-1 shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)]">
                    <button
                        type="button"
                        onClick={() => {
                            onChange("");
                            setOpen(false);
                        }}
                        className={`flex w-full items-center gap-2 px-2.5 py-1.5 text-[12px] transition-colors hover:bg-slate-50 ${
                            value === "" ? "text-slate-900 font-medium" : "text-slate-600"
                        }`}
                    >
                        <span className="size-2 rounded-full bg-slate-300" />
                        No type
                    </button>

                    <div className="max-h-48 overflow-y-auto">
                        {isPending ? (
                            <div className="px-2.5 py-2 text-[11.5px] text-slate-400">Loading…</div>
                        ) : (
                            types.map((t) => (
                                <TypeRow
                                    key={t.id}
                                    type={t}
                                    selected={t.name === value}
                                    onSelect={() => {
                                        onChange(t.name);
                                        setOpen(false);
                                    }}
                                    onDeletedSelected={() => onChange("")}
                                    isSelectedValue={t.name === value}
                                />
                            ))
                        )}
                    </div>

                    <div className="border-t border-slate-100 mt-1 pt-1">
                        <NewTypeRow onCreated={(name) => onChange(name)} />
                    </div>
                </div>
            )}
        </div>
    );
}

function TypeRow({
    type,
    selected,
    onSelect,
    onDeletedSelected,
    isSelectedValue,
}: {
    type: TaskType;
    selected: boolean;
    onSelect: () => void;
    onDeletedSelected: () => void;
    isSelectedValue: boolean;
}) {
    const update = useUpdateTaskType();
    const del = useDeleteTaskType();
    const confirm = useConfirm();
    const [editing, setEditing] = React.useState(false);
    const [name, setName] = React.useState(type.name);
    const [color, setColor] = React.useState(type.color);

    React.useEffect(() => {
        setName(type.name);
        setColor(type.color);
    }, [type.name, type.color]);

    async function save() {
        if (!name.trim()) return;
        try {
            await update.mutateAsync({ id: type.id, data: { name: name.trim(), color } });
            setEditing(false);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    function remove(e: React.MouseEvent) {
        e.stopPropagation();
        confirm?.show(`Delete the “${type.name}” type? Existing tasks keep the label.`, async () => {
            try {
                await del.mutateAsync(type.id);
                if (isSelectedValue) onDeletedSelected();
            } catch (e) {
                toast.error(buildError(e as AppError));
            }
        });
    }

    if (editing) {
        return (
            <div className="px-2 py-1.5 space-y-1.5 bg-slate-50/60">
                <input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    autoFocus
                    onKeyDown={(e) => {
                        if (e.key === "Enter") save();
                        if (e.key === "Escape") setEditing(false);
                    }}
                    className="w-full h-7 px-2 rounded-md border border-slate-200 text-[12px] outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                />
                <Swatches value={color} onChange={setColor} />
                <div className="flex items-center gap-1.5 justify-end">
                    <button
                        type="button"
                        onClick={() => setEditing(false)}
                        className="h-6 px-2 rounded text-[11px] text-slate-500 hover:text-slate-900 hover:bg-slate-100"
                    >
                        Cancel
                    </button>
                    <button
                        type="button"
                        onClick={save}
                        disabled={update.isPending}
                        className="h-6 px-2 rounded bg-slate-900 hover:bg-slate-800 text-white text-[11px] font-medium inline-flex items-center gap-1 disabled:opacity-60"
                    >
                        {update.isPending ? <Loader2Icon className="w-2.5 h-2.5 animate-spin" /> : <CheckIcon className="w-2.5 h-2.5" />}
                        Save
                    </button>
                </div>
            </div>
        );
    }

    return (
        <div
            className={`group flex items-center gap-2 px-2.5 py-1.5 text-[12px] cursor-pointer transition-colors hover:bg-slate-50 ${
                selected ? "text-slate-900 font-medium" : "text-slate-700"
            }`}
            onClick={onSelect}
        >
            <span className="size-2 rounded-full shrink-0" style={{ backgroundColor: type.color }} />
            <span className="truncate flex-1">{type.name}</span>
            {selected && <CheckIcon className="w-3 h-3 text-sky-600 shrink-0" />}
            <button
                type="button"
                onClick={(e) => {
                    e.stopPropagation();
                    setEditing(true);
                }}
                aria-label="Edit type"
                className="size-5 rounded text-slate-400 hover:text-slate-700 hover:bg-slate-100 inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-opacity"
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
        </div>
    );
}

function NewTypeRow({ onCreated }: { onCreated: (name: string) => void }) {
    const create = useCreateTaskType();
    const [adding, setAdding] = React.useState(false);
    const [name, setName] = React.useState("");
    const [color, setColor] = React.useState(TASK_TYPE_COLORS[0]);

    async function submit() {
        if (!name.trim()) return;
        try {
            const t = await create.mutateAsync({ name: name.trim(), color });
            onCreated(t.name);
            setName("");
            setAdding(false);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    if (!adding) {
        return (
            <button
                type="button"
                onClick={() => setAdding(true)}
                className="flex w-full items-center gap-2 px-2.5 py-1.5 text-[12px] text-sky-700 hover:bg-sky-50 transition-colors"
            >
                <PlusIcon className="w-3 h-3" />
                New type
            </button>
        );
    }

    return (
        <div className="px-2 py-1.5 space-y-1.5">
            <div className="flex items-center gap-1.5">
                <input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="Type name (e.g. LinkedIn)"
                    autoFocus
                    onKeyDown={(e) => {
                        if (e.key === "Enter") submit();
                        if (e.key === "Escape") setAdding(false);
                    }}
                    className="w-full h-7 px-2 rounded-md border border-slate-200 text-[12px] outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                />
                <button
                    type="button"
                    onClick={() => setAdding(false)}
                    aria-label="Cancel"
                    className="size-7 rounded-md text-slate-400 hover:text-slate-700 hover:bg-slate-100 inline-flex items-center justify-center shrink-0"
                >
                    <XIcon className="w-3 h-3" />
                </button>
            </div>
            <Swatches value={color} onChange={setColor} />
            <button
                type="button"
                onClick={submit}
                disabled={create.isPending || !name.trim()}
                className="h-7 w-full rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center justify-center gap-1 transition-colors disabled:opacity-60"
            >
                {create.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <PlusIcon className="w-3 h-3" />}
                Add type
            </button>
        </div>
    );
}

function Swatches({ value, onChange }: { value: string; onChange: (c: string) => void }) {
    return (
        <div className="flex flex-wrap gap-1">
            {TASK_TYPE_COLORS.map((c) => (
                <button
                    key={c}
                    type="button"
                    onClick={() => onChange(c)}
                    aria-label={c}
                    className={`size-4 rounded-full transition-transform ${value === c ? "ring-2 ring-offset-1 ring-slate-900" : "hover:scale-110"}`}
                    style={{ backgroundColor: c }}
                />
            ))}
        </div>
    );
}
