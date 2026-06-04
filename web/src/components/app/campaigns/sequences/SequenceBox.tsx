import { Trash2Icon } from "lucide-react";
import { cn } from "@/lib/utils";

// One selectable step in the sequence stepper. Renders "Step N", the step's
// display name, and a subject preview. Active rows highlight sky; the delete
// affordance stays touch-reachable (visible on mobile, hover-revealed on md+).
export default function SequenceBox({
    index,
    name,
    subject,
    active,
    onClick,
    onDelete,
}: {
    index: number;
    name: string;
    subject: string;
    active: boolean;
    onClick: () => void;
    onDelete: () => void;
}) {
    return (
        <div
            onClick={onClick}
            role="button"
            tabIndex={0}
            onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onClick();
                }
            }}
            className={cn(
                "group relative w-full select-none cursor-pointer rounded-md border px-3 py-2.5 transition-colors",
                active
                    ? "border-sky-300 bg-sky-50 ring-2 ring-sky-100"
                    : "border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50",
            )}
        >
            <div className="flex items-center gap-2">
                <span
                    className={cn(
                        "shrink-0 size-5 rounded-md inline-flex items-center justify-center font-mono text-[10.5px] tabular-nums font-medium",
                        active ? "bg-sky-600 text-white" : "bg-slate-100 text-slate-500",
                    )}
                >
                    {index + 1}
                </span>
                <span
                    className={cn(
                        "min-w-0 truncate text-[12.5px] font-medium",
                        active ? "text-sky-700" : "text-slate-700",
                    )}
                >
                    {name || `Step ${index + 1}`}
                </span>
                <button
                    type="button"
                    aria-label="Delete step"
                    onClick={(e) => {
                        e.stopPropagation();
                        onDelete();
                    }}
                    className="ml-auto shrink-0 size-5 rounded inline-flex items-center justify-center text-slate-400 hover:text-rose-600 hover:bg-rose-50 transition-colors opacity-100 md:opacity-0 md:group-hover:opacity-100 focus:opacity-100"
                >
                    <Trash2Icon className="w-3.5 h-3.5" />
                </button>
            </div>
            <p
                className={cn(
                    "mt-1 truncate text-[11px]",
                    active ? "text-sky-600/80" : "text-slate-400",
                )}
            >
                {subject || "No subject yet"}
            </p>
        </div>
    );
}
