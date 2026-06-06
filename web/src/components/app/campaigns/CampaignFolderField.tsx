// Themed folder multi-select for the campaign settings page.
//
// Replaces the off-theme chip-box FolderSelector with toggle chips in our own
// slate/sky language: each folder is a pill tinted with its own color, a solid
// color dot, and a check when selected. Clicking toggles membership.

import { CheckIcon, PlusIcon, Settings2Icon } from "lucide-react";
import { useUserProfile } from "@/hooks/context/user";
import { cn, hexToRgba } from "@/lib/utils";

export default function CampaignFolderField({
    selected,
    onToggle,
}: {
    selected: string[];
    onToggle: (id: string) => void;
}) {
    const p = useUserProfile();
    const folders = [...(p.user.folders ?? [])].sort((a, b) => a.position - b.position);

    if (folders.length === 0) {
        return (
            <div className="rounded-md border border-dashed border-slate-200 bg-slate-50/50 px-4 py-5 text-center">
                <p className="text-[12.5px] text-slate-600 font-medium">No folders yet</p>
                <p className="text-[11.5px] text-slate-400 mt-0.5">
                    Create folders to organize your campaigns.
                </p>
                <button
                    type="button"
                    onClick={() => p.setFoldersEdit(true)}
                    className="mt-3 inline-flex items-center gap-1.5 h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium transition-colors"
                >
                    <PlusIcon className="w-3 h-3" />
                    New folder
                </button>
            </div>
        );
    }

    return (
        <div className="flex flex-wrap items-center gap-2">
            {folders.map((f) => {
                const on = selected.includes(f.id);
                return (
                    <button
                        key={f.id}
                        type="button"
                        onClick={() => onToggle(f.id)}
                        aria-pressed={on}
                        className={cn(
                            "inline-flex items-center gap-1.5 h-7 pl-2 pr-2.5 rounded-md border text-[12.5px] font-medium transition-colors",
                            on
                                ? "border-transparent text-slate-800"
                                : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:text-slate-900",
                        )}
                        style={on ? { backgroundColor: hexToRgba(f.color, 0.16) } : undefined}
                    >
                        <span
                            className="inline-block size-2.5 rounded-full ring-1 ring-black/5 shrink-0"
                            style={{ backgroundColor: f.color }}
                        />
                        <span className="truncate max-w-[160px]">{f.title}</span>
                        {on && <CheckIcon className="w-3.5 h-3.5 text-sky-600 shrink-0" strokeWidth={2.5} />}
                    </button>
                );
            })}
            <button
                type="button"
                onClick={() => p.setFoldersEdit(true)}
                className="inline-flex items-center gap-1.5 h-7 px-2 rounded-md text-[12px] font-medium text-slate-500 hover:text-slate-900 hover:bg-slate-50 transition-colors"
            >
                <Settings2Icon className="w-3.5 h-3.5" />
                Manage
            </button>
        </div>
    );
}
