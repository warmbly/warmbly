// User menu — bottom of the sidebar.
//
// Moved off the shadcn DropdownMenu onto the same PopoverMenu primitive
// every other dropdown in the dashboard uses (folders, sort, accounts,
// org switcher). One animation curve, one surface, one set of styles.
//
// Opens upward (side="top") from the trigger so the popover settles up
// from the bottom of the sidebar instead of falling off-screen.

import { useNavigate } from "react-router-dom";
import {
    LogOutIcon,
    SettingsIcon,
} from "lucide-react";
import { useAppStore } from "@/stores";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";

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
        <PopoverMenu side="top" align="start">
            <PopoverMenuTrigger asChild>
                <button className="flex items-center gap-2.5 mx-3 my-2 px-1.5 py-1 rounded-md hover:bg-slate-200/40 transition-colors w-[calc(100%-1.5rem)] cursor-pointer">
                    <div className="w-7 h-7 rounded-full bg-slate-900 flex items-center justify-center shrink-0 overflow-hidden">
                        {user.avatar_url ? (
                            <img
                                src={user.avatar_url}
                                alt=""
                                className="w-full h-full object-cover"
                            />
                        ) : (
                            <span className="text-[11px] font-medium text-white leading-none">
                                {initials}
                            </span>
                        )}
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
            </PopoverMenuTrigger>

            <PopoverMenuContent minWidth={232}>
                {/* Identity block — same name/email row but inside the
                    PopoverMenu chrome so it inherits the consistent
                    hairline border + shadow. */}
                <div className="px-3 py-2">
                    <div className="text-[12.5px] font-medium text-slate-900 truncate">
                        {displayName}
                    </div>
                    <div className="text-[11px] text-slate-400 truncate font-mono">
                        {user.email}
                    </div>
                </div>
                <PopoverMenuSeparator />
                <PopoverMenuItem
                    onSelect={() => navigate("/app/settings")}
                    icon={<SettingsIcon className="w-3 h-3" />}
                >
                    Settings
                </PopoverMenuItem>
                <PopoverMenuSeparator />
                <PopoverMenuItem
                    onSelect={handleLogout}
                    icon={<LogOutIcon className="w-3 h-3" />}
                    danger
                >
                    Log out
                </PopoverMenuItem>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
