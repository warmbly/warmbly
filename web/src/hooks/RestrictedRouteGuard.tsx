// Keeps a member restricted to selected resources on the pages their scope
// covers, including after their access changes while a page is open.

import { useEffect } from "react";
import { useLocation, useNavigate } from "@tanstack/react-router";
import { useAppStore } from "@/stores";
import { isAccessRestricted } from "@/hooks/usePermission";
import { isScopedPath, restrictedHome } from "@/lib/accessScope";

export function RestrictedRouteGuard() {
    const org = useAppStore((s) => s.currentOrganization);
    const pathname = useLocation({ select: (l) => l.pathname });
    const navigate = useNavigate();
    const restricted = isAccessRestricted(org);
    const outside = restricted && !isScopedPath(pathname);

    useEffect(() => {
        if (outside) void navigate({ to: restrictedHome(org), replace: true });
    }, [outside, org, navigate]);

    return null;
}
