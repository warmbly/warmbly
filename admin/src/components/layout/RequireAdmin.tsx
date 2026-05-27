// Route guard for authenticated admin-only pages.
//
// 1. If there's no token, bounce to /auth/login.
// 2. If /auth/me 401s or otherwise fails, treat as logged-out.
// 3. If the user is authenticated but doesn't have is_admin, render an
//    obvious "you are not an admin" screen rather than silently
//    forwarding them — same-domain dashboard users could otherwise
//    land here by mistake.

import { Navigate, Outlet, useLocation } from "react-router-dom";
import { useMe } from "@/hooks/useMe";
import { getToken } from "@/lib/auth/storage";
import { AdminBadge } from "./AdminBadge";

export function RequireAdmin() {
    const loc = useLocation();
    const hasToken = !!getToken();
    const { data: me, isLoading, isError } = useMe();

    if (!hasToken) {
        return <Navigate to="/auth/login" state={{ from: loc.pathname }} replace />;
    }

    if (isLoading) {
        return (
            <div className="min-h-screen flex flex-col items-center justify-center gap-4 bg-background">
                <AdminBadge />
                <div className="text-sm text-muted-foreground">Loading admin session…</div>
            </div>
        );
    }

    if (isError || !me) {
        return <Navigate to="/auth/login" state={{ from: loc.pathname }} replace />;
    }

    if (!me.is_admin) {
        return (
            <div className="min-h-screen flex flex-col items-center justify-center gap-4 bg-background p-6 text-center">
                <AdminBadge />
                <h1 className="text-xl font-semibold">Access denied</h1>
                <p className="text-sm text-muted-foreground max-w-sm">
                    Your account ({me.email}) does not have admin permissions. Ask an existing
                    admin to grant access, or return to the dashboard.
                </p>
                <a href="/auth/login" className="text-sm underline text-muted-foreground">
                    Switch account
                </a>
            </div>
        );
    }

    return <Outlet />;
}
