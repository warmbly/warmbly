// The data-group checklist shared by the export and import panels.
//
// Groups come from the server's own catalog rather than a copy here, so a new
// group appears in the dashboard the moment the backend knows about it.

import { CheckIcon } from "lucide-react";
import type { OrgDataGroup, OrgDataGroupInfo } from "@/lib/api/models/app/orgtransfer/OrgTransfer";
import { dependentsOf } from "@/lib/api/models/app/orgtransfer/OrgTransfer";

export default function GroupPicker({
    groups,
    selected,
    onToggle,
    disabled,
}: {
    groups: OrgDataGroupInfo[];
    selected: Set<OrgDataGroup>;
    onToggle: (key: OrgDataGroup) => void;
    disabled?: boolean;
}) {
    if (groups.length === 0) {
        return <div className="text-[12px] text-slate-400">Loading data groups…</div>;
    }

    return (
        <div className="rounded-md border border-slate-200 divide-y divide-slate-200/70 overflow-hidden">
            {groups.map((g) => {
                const on = g.required || selected.has(g.key);
                // A group another selected group depends on cannot be unticked
                // on its own: the import would fail on a foreign key.
                const pinnedBy = dependentsOf(g.key, selected, groups);
                const locked = g.required || pinnedBy.length > 0 || disabled;
                return (
                    <button
                        key={g.key}
                        type="button"
                        disabled={locked}
                        onClick={() => onToggle(g.key)}
                        className={`w-full flex items-start gap-2.5 px-3 py-2.5 text-left transition-colors ${
                            locked ? "cursor-default" : "hover:bg-slate-50"
                        }`}
                    >
                        <span
                            className={`mt-px shrink-0 size-[15px] rounded-[4px] border inline-flex items-center justify-center transition-colors ${
                                on
                                    ? "bg-sky-600 border-sky-600 text-white"
                                    : "bg-white border-slate-300"
                            } ${locked ? "opacity-60" : ""}`}
                        >
                            {on && <CheckIcon className="w-2.5 h-2.5" strokeWidth={3} />}
                        </span>
                        <span className="min-w-0 flex-1">
                            <span className="flex items-center gap-1.5">
                                <span className="text-[12.5px] font-medium text-slate-900">{g.label}</span>
                                {g.required && (
                                    <span className="text-[10px] uppercase tracking-[0.08em] font-medium rounded-sm px-1 bg-slate-100 text-slate-500">
                                        Always
                                    </span>
                                )}
                                {!g.required && pinnedBy.length > 0 && (
                                    <span className="text-[10px] uppercase tracking-[0.08em] font-medium rounded-sm px-1 bg-slate-100 text-slate-500">
                                        Needed by {pinnedBy.map((d) => d.label).join(", ")}
                                    </span>
                                )}
                                {g.heavy && (
                                    <span className="text-[10px] uppercase tracking-[0.08em] font-medium rounded-sm px-1 bg-amber-50 text-amber-700">
                                        Large
                                    </span>
                                )}
                            </span>
                            <span className="block text-[11.5px] text-slate-500 leading-tight mt-0.5">
                                {g.description}
                            </span>
                        </span>
                    </button>
                );
            })}
        </div>
    );
}
