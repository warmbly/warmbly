// User dropdown in the topbar. Shows the logged-in admin's email + a
// shortcut back to the dashboard. Logout clears the admin token
// (we don't touch dashboard tokens because they live under a different
// localStorage key) and bounces back to /auth/login.

import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { ExternalLink, LogOut, User as UserIcon } from "lucide-react";
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Button } from "@/components/ui/button";
import { useMe } from "@/hooks/useMe";
import { logout as logoutCall } from "@/lib/api/client/auth";
import { clearToken } from "@/lib/auth/storage";
import { DASHBOARD_URL } from "@/lib/env";

export function UserMenu() {
    const { data: me } = useMe();
    const nav = useNavigate();
    const qc = useQueryClient();

    async function handleLogout() {
        try {
            await logoutCall();
        } catch {
            /* even if the server call fails, drop our local session */
        } finally {
            clearToken();
            qc.clear();
            nav("/auth/login", { replace: true });
        }
    }

    const initials = (
        (me?.first_name?.[0] ?? "") + (me?.last_name?.[0] ?? "")
    ).toUpperCase() || (me?.email?.[0]?.toUpperCase() ?? "?");

    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="sm" className="gap-2 px-2">
                    <span className="size-7 rounded-full bg-zinc-900 text-white text-xs font-semibold flex items-center justify-center">
                        {initials}
                    </span>
                    <span className="hidden md:inline text-sm text-muted-foreground">
                        {me?.email ?? "signed in"}
                    </span>
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
                <DropdownMenuLabel className="font-normal">
                    <div className="text-xs text-muted-foreground">Signed in as</div>
                    <div className="text-sm truncate">{me?.email ?? "—"}</div>
                </DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem asChild>
                    <a href={DASHBOARD_URL} target="_blank" rel="noreferrer">
                        <ExternalLink className="size-4" />
                        Open dashboard
                    </a>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                    <a href="/settings/encryption">
                        <UserIcon className="size-4" />
                        Admin settings
                    </a>
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={handleLogout} className="text-[var(--admin-danger)] focus:text-[var(--admin-danger)]">
                    <LogOut className="size-4" />
                    Log out
                </DropdownMenuItem>
            </DropdownMenuContent>
        </DropdownMenu>
    );
}
