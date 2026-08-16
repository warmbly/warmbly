// Page-level gate for the admin permission bit the backend enforces on the
// matching route. Renders the denial in place of the page so a restricted
// admin gets an explanation instead of a 403 error panel.

import type { ReactNode } from "react";
import { ShieldAlert } from "lucide-react";
import { useAdminPerm } from "@/hooks/useAdminPerm";

interface Props {
    perm: number;
    // Human name of the permission, e.g. "Manage settings".
    permissionLabel: string;
    children: ReactNode;
}

export function RequirePermission({ perm, permissionLabel, children }: Props) {
    const allowed = useAdminPerm(perm);

    if (allowed) return <>{children}</>;

    return (
        <div className="rounded-lg border border-dashed border-border p-10 text-center">
            <ShieldAlert className="mx-auto size-5 text-muted-foreground" />
            <div className="mt-2 text-sm font-medium text-foreground">
                This page is restricted
            </div>
            <p className="mx-auto mt-1 max-w-md text-xs text-muted-foreground">
                Your admin account does not hold the {permissionLabel} permission. Ask an
                admin who can grant admin access to add it.
            </p>
        </div>
    );
}
