// User search-and-pick for the grant dialog. Searches /admin/users?q= with a
// short page; 2+ characters, debounced by SearchPicker.

import { SearchPicker } from "../fleet/SearchPicker";
import { searchUsers } from "@/lib/api/client/admin/users";
import type { AdminUserDetail } from "@/lib/api/models/admin";
import { userName } from "../fleet/format";

export type PickedUser = Pick<AdminUserDetail, "id" | "email" | "first_name" | "last_name" | "admin_permissions">;

export function UserPicker({
    value,
    onChange,
    autoFocus,
}: {
    value: PickedUser | null;
    onChange: (v: PickedUser | null) => void;
    autoFocus?: boolean;
}) {
    return (
        <SearchPicker<PickedUser>
            value={value}
            onChange={onChange}
            queryKey={["admin", "users", "picker"]}
            search={async (q) => (await searchUsers({ q, limit: 8 })).data ?? []}
            getKey={(u) => u.id}
            placeholder="Search users by email or name…"
            emptyText="No user matches."
            autoFocus={autoFocus}
            renderItem={(u) => (
                <div className="flex items-center justify-between gap-2">
                    <div className="min-w-0">
                        <div className="truncate font-medium text-foreground">{userName(u)}</div>
                        <div className="truncate text-[11px] text-muted-foreground">{u.email}</div>
                    </div>
                    {u.admin_permissions > 0 && (
                        <span className="shrink-0 text-[10px] uppercase tracking-wider text-[var(--admin-accent-strong)]">admin</span>
                    )}
                </div>
            )}
            renderSelected={(u) => (
                <div className="truncate">
                    <span className="font-medium">{userName(u)}</span>
                    <span className="ml-1.5 text-[11px] text-muted-foreground">{u.email}</span>
                </div>
            )}
        />
    );
}
