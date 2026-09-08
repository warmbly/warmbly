// An admin's bits as chips grouped by catalog category. Bits the catalog does
// not know (retired ones still stored on old super admins) are not shown.

import { Badge } from "@/components/ui/badge";
import type { PermissionInfo } from "@/lib/api/client/admin/admins";
import { groupByCategory, hasBit, humanize } from "./permissions";

export function PermissionChips({ mask, catalog }: { mask: number; catalog: PermissionInfo[] }) {
    const groups = groupByCategory(catalog)
        .map((g) => ({ ...g, items: g.items.filter((p) => hasBit(mask, p.permission)) }))
        .filter((g) => g.items.length > 0);
    if (groups.length === 0) {
        return <span className="text-xs text-muted-foreground">No live permissions</span>;
    }
    return (
        <div className="flex flex-col gap-1">
            {groups.map((g) => (
                <div key={g.category} className="flex flex-wrap items-center gap-1">
                    <span className="mr-0.5 text-[10px] uppercase tracking-wider text-muted-foreground">{g.category}</span>
                    {g.items.map((p) => (
                        <Badge
                            key={p.name}
                            variant="outline"
                            title={p.description}
                            className={
                                p.name === "grant_admin_access"
                                    ? "border-[var(--admin-accent)] bg-[var(--admin-accent-soft)] text-[10px] text-[var(--admin-accent-strong)]"
                                    : "text-[10px]"
                            }
                        >
                            {humanize(p.name)}
                        </Badge>
                    ))}
                </div>
            ))}
        </div>
    );
}
