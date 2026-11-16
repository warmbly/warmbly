// Agency only: which client workspace each source workspace lands in.

import { ArrowRightIcon } from "lucide-react";
import { TextInput } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import type { AgencyClient } from "@/lib/api/models/app/agency/Agency";
import type { ToolMigration } from "@/lib/api/models/app/toolmigrate/ToolMigration";
import type { Destination, Mapping } from "./choices";
import { fmtCount, sourceName, workspaceKey } from "./labels";

const encode = (d: Destination) => (d.mode === "existing" ? `org:${d.orgId}` : d.mode);

export default function MapStep({
    migration,
    label,
    clients,
    mapping,
    onMapping,
}: {
    migration: ToolMigration;
    label: string;
    clients: AgencyClient[];
    mapping: Mapping;
    onMapping: (m: Mapping) => void;
}) {
    const sorted = [...clients].sort((a, b) => a.name.localeCompare(b.name));
    const options: SelectOption[] = [
        { value: "new", label: "New client workspace" },
        ...sorted.map((c) => ({ value: `org:${c.organization_id}`, label: c.name, group: "Existing clients" })),
        { value: "skip", label: "Skip" },
    ];

    return (
        <div className="p-4 sm:p-5 space-y-4">
            <div>
                <h3 className="text-[13px] font-semibold text-slate-900">Where does each one go?</h3>
                <p className="text-[12px] text-slate-500 mt-0.5 leading-relaxed">
                    Each {label} workspace lands in one client workspace. New clients are created when the migration starts and count toward your agency&apos;s client limit.
                </p>
            </div>
            <div className="rounded-md border border-slate-200 divide-y divide-slate-200/70">
                {(migration.inventory ?? []).map((w) => {
                    const key = workspaceKey(w.workspace);
                    const name = sourceName(w.workspace.name, label);
                    const d: Destination = mapping[key] ?? { mode: "skip" };
                    const set = (next: Destination) => onMapping({ ...mapping, [key]: next });
                    const pick = (v: string) => {
                        if (v === "new") set({ mode: "new", name: d.mode === "new" ? d.name : name });
                        else if (v === "skip") set({ mode: "skip" });
                        else set({ mode: "existing", orgId: v.slice(4) });
                    };
                    return (
                        <div key={key} className="px-3 py-2.5 flex flex-col sm:flex-row sm:items-start gap-2 sm:gap-3">
                            <div className="sm:w-56 min-w-0 shrink-0 sm:pt-1">
                                <div className="text-[12.5px] font-medium text-slate-900 truncate">{name}</div>
                                <div className="text-[11px] text-slate-500 tabular-nums">
                                    {w.error
                                        ? "Could not be read"
                                        : `${fmtCount(w.inventory.campaigns)} campaigns, ${fmtCount(w.inventory.leads)} leads, ${fmtCount(w.inventory.mailboxes)} mailboxes`}
                                </div>
                            </div>
                            <ArrowRightIcon className="hidden sm:block w-3.5 h-3.5 text-slate-300 mt-2 shrink-0" />
                            <div className="flex-1 min-w-0 space-y-1.5">
                                <SelectMenu
                                    value={encode(d)}
                                    onChange={pick}
                                    options={options}
                                    disabled={!!w.error}
                                    fullWidth
                                    aria-label={`Destination for ${name}`}
                                />
                                {d.mode === "new" && (
                                    <TextInput
                                        value={d.name}
                                        onChange={(v) => set({ mode: "new", name: v })}
                                        placeholder="Client name"
                                        maxLength={100}
                                        className="w-full"
                                    />
                                )}
                            </div>
                        </div>
                    );
                })}
            </div>
        </div>
    );
}
