// User menu — sits at the very bottom of the sidebar on the sky chrome.
// Styled for the dark background; the dropdown itself is light because
// it overlays the content area.

import { Link, useNavigate } from "react-router-dom";
import {
    CreditCardIcon,
    LogOutIcon,
    SettingsIcon,
    UsersIcon,
} from "lucide-react";
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuGroup,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useAppStore } from "@/stores";

export function UserNav() {
    const navigate = useNavigate();
    const user = useAppStore((s) => s.user);
    const logout = useAppStore((s) => s.logout);

    if (!user) return null;

    const handleLogout = () => {
        logout();
        localStorage.removeItem("token");
        navigate("/auth/login");
    };

    const initials = user.email.slice(0, 2).toUpperCase();
    const displayName =
        user.first_name && user.last_name
            ? `${user.first_name} ${user.last_name}`
            : user.email;

    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <button className="flex items-center gap-2.5 mx-3 my-2 px-1.5 py-1 rounded-md hover:bg-white/70 transition-colors w-[calc(100%-1.5rem)] cursor-pointer">
                    <div className="w-7 h-7 rounded-full bg-slate-900 flex items-center justify-center shrink-0">
                        <span className="text-[11px] font-medium text-white leading-none">
                            {initials}
                        </span>
                    </div>
                    <div className="flex-1 min-w-0 text-left">
                        <div className="text-[13px] text-slate-900 truncate">
                            {displayName}
                        </div>
                        <div className="text-[10.5px] text-slate-500 truncate">
                            {user.email}
                        </div>
                    </div>
                </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent className="min-w-56" side="top" align="start" sideOffset={6}>
                <div className="px-2 py-1.5">
                    <div className="text-sm font-medium text-zinc-900">{displayName}</div>
                    <div className="text-xs text-zinc-400">{user.email}</div>
                </div>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                    <DropdownMenuItem asChild>
                        <Link to="/app/settings">
                            <SettingsIcon className="w-4 h-4" />
                            <span className="ml-2">Settings</span>
                        </Link>
                    </DropdownMenuItem>
                    <DropdownMenuItem asChild>
                        <Link to="/app/billing">
                            <CreditCardIcon className="w-4 h-4" />
                            <span className="ml-2">Billing</span>
                        </Link>
                    </DropdownMenuItem>
                    <DropdownMenuItem asChild>
                        <Link to="/app/team">
                            <UsersIcon className="w-4 h-4" />
                            <span className="ml-2">Team</span>
                        </Link>
                    </DropdownMenuItem>
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={handleLogout}>
                    <LogOutIcon className="w-4 h-4" />
                    <span className="ml-2">Log out</span>
                </DropdownMenuItem>
            </DropdownMenuContent>
        </DropdownMenu>
    );
}
