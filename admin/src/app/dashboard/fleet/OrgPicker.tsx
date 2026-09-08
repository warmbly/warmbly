// Organization search-and-pick, used by the convert-to-dedicated and export
// dialogs. Searches /admin/organizations?q= with a short page.

import { SearchPicker } from "./SearchPicker";
import { listOrganizations } from "@/lib/api/client/admin/organizations";
import type { AdminOrgListItem } from "@/lib/api/models/admin";

export type PickedOrg = Pick<AdminOrgListItem, "id" | "name" | "owner_email">;

export function OrgPicker({
    value,
    onChange,
    autoFocus,
    disabled,
}: {
    value: PickedOrg | null;
    onChange: (v: PickedOrg | null) => void;
    autoFocus?: boolean;
    disabled?: boolean;
}) {
    return (
        <SearchPicker<PickedOrg>
            value={value}
            onChange={onChange}
            queryKey={["admin", "organizations", "picker"]}
            search={async (q) => (await listOrganizations({ q, limit: 8 })).data ?? []}
            getKey={(o) => o.id}
            placeholder="Search workspaces by name or owner email…"
            emptyText="No workspace matches."
            autoFocus={autoFocus}
            disabled={disabled}
            renderItem={(o) => (
                <div>
                    <div className="font-medium text-foreground">{o.name}</div>
                    <div className="text-[11px] text-muted-foreground">{o.owner_email}</div>
                </div>
            )}
            renderSelected={(o) => (
                <div className="truncate">
                    <span className="font-medium">{o.name}</span>
                    <span className="ml-1.5 text-[11px] text-muted-foreground">{o.owner_email}</span>
                </div>
            )}
        />
    );
}
