// Data group toggles for export and import. Ticking a group pulls in what it
// cannot travel without (as the server would); unticking one that others
// depend on explains itself instead of silently dropping them.

import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";
import {
    dependentsOf,
    expandGroups,
    ORG_DATA_GROUP_CATALOG,
    type OrgDataGroup,
} from "@/lib/api/client/admin/transfers";
import { cn } from "@/lib/utils";

export function GroupPicker({
    selected,
    onChange,
    /** Restrict to these keys (an archive's contents on import). */
    available,
    disabled,
}: {
    selected: Set<OrgDataGroup>;
    onChange: (next: Set<OrgDataGroup>) => void;
    available?: OrgDataGroup[];
    disabled?: boolean;
}) {
    const groups = available
        ? ORG_DATA_GROUP_CATALOG.filter((g) => available.includes(g.key))
        : ORG_DATA_GROUP_CATALOG;

    function toggle(key: OrgDataGroup) {
        const next = new Set(selected);
        if (next.has(key)) {
            next.delete(key);
            for (const d of dependentsOf(key, next)) next.delete(d.key);
        } else {
            next.add(key);
        }
        onChange(expandGroups(next));
    }

    return (
        <div className="grid gap-1 sm:grid-cols-2">
            {groups.map((g) => {
                const on = g.required || selected.has(g.key);
                const deps = dependentsOf(g.key, selected);
                return (
                    <label
                        key={g.key}
                        className={cn(
                            "flex items-start gap-2 rounded-md border px-2.5 py-2 text-[12.5px]",
                            on ? "border-border bg-card" : "border-border/60 bg-muted/20",
                            g.required || disabled ? "cursor-default" : "cursor-pointer hover:bg-muted/40",
                        )}
                    >
                        <Checkbox
                            checked={on}
                            disabled={g.required || disabled}
                            onCheckedChange={() => toggle(g.key)}
                            className="mt-0.5"
                        />
                        <span className="min-w-0 flex-1">
                            <span className="flex items-center gap-1.5">
                                <span className="font-medium leading-tight">{g.label}</span>
                                {g.required && <Badge variant="outline" className="text-[9px]">required</Badge>}
                                {g.heavy && (
                                    <Badge variant="outline" className="border-amber-300 bg-amber-50 text-[9px] text-amber-700">
                                        heavy
                                    </Badge>
                                )}
                            </span>
                            <span className="block text-[11px] leading-tight text-muted-foreground">{g.description}</span>
                            {on && deps.length > 0 && !g.required && (
                                <span className="block text-[10px] leading-tight text-muted-foreground">
                                    Needed by {deps.map((d) => d.label).join(", ")}.
                                </span>
                            )}
                        </span>
                    </label>
                );
            })}
        </div>
    );
}
