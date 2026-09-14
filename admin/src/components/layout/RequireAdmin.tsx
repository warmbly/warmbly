// Route guard for authenticated admin-only pages.
//
// 1. If there's no token, bounce to /auth/login.
// 2. If /auth/me 401s or otherwise fails, treat as logged-out.
// 3. If the user is authenticated but doesn't have is_admin, render an
//    obvious "you are not an admin" screen rather than silently
//    forwarding them — same-domain dashboard users could otherwise
//    land here by mistake.

import { useEffect } from "react";
import { Navigate, Outlet, useLocation } from "react-router-dom";
import { useMe } from "@/hooks/useMe";
import { getToken } from "@/lib/auth/storage";
import { noteStep, setErrorIdentity } from "@/lib/observability";
import { AdminBadge } from "./AdminBadge";

export function RequireAdmin() {
    const loc = useLocation();
    const hasToken = !!getToken();
    const { data: me, isLoading, isError } = useMe();

    // Every admin route is behind this guard, so it is the one place that
    // knows both who is signed in and where they are. Every reported event and
    // exception carries them, which is the difference between an issue
    // somebody can act on and a stack trace with no owner. See
    // lib/observability.
    const userId = me?.id ?? null;
    const email = me?.email ?? null;
    const name = [me?.first_name, me?.last_name].filter(Boolean).join(" ") || null;
    useEffect(() => {
        setErrorIdentity(userId ? { userId, email, name } : null);
        return () => setErrorIdentity(null);
    }, [userId, email, name]);

    const path = loc.pathname;
    useEffect(() => {
        noteStep(`Opened ${path}`, { path });
    }, [path]);

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
