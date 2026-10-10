import { DashboardImage } from "@/components/ui/dashboard-image";

// User menu — bottom of the sidebar.
//
// Moved off the shadcn DropdownMenu onto the same PopoverMenu primitive
// every other dropdown in the dashboard uses (folders, sort, accounts,
// org switcher). One animation curve, one surface, one set of styles.
//
// Opens upward (side="top") from the trigger so the popover settles up
// from the bottom of the sidebar instead of falling off-screen.

import { useNavigate } from "@tanstack/react-router";
import {
    CompassIcon,
    LogOutIcon,
    SettingsIcon,
} from "lucide-react";
import { useAppStore } from "@/stores";
import useLogout from "@/lib/api/hooks/auth/useLogout";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { cn } from "@/lib/utils";
import { ThemeSwitch } from "@/components/app/theme/ThemePicker";

export function UserNav({ collapsed = false }: { collapsed?: boolean }) {
    const navigate = useNavigate();
    const user = useAppStore((s) => s.user);
    const logoutMutation = useLogout();
    const setProductTourOpen = useAppStore((s) => s.setProductTourOpen);

    if (!user) return null;

    const handleLogout = async () => {
        await logoutMutation.mutateAsync();
        navigate({ to: "/auth/login", replace: true });
    };

    const initials = user.email.slice(0, 2).toUpperCase();
    const displayName =
        user.first_name && user.last_name
            ? `${user.first_name} ${user.last_name}`
            : user.email;

    return (
        <PopoverMenu side="top" align="start">
            <PopoverMenuTrigger asChild>
                <button
                    aria-label={collapsed ? displayName : undefined}
                    // One element in both shapes so it eases with the sidebar's
                    // width: the avatar keeps its place at the rail's centre, the name fades.
                    className={cn(
                        "flex items-center mx-3 my-2 pl-0.5 rounded-md hover:bg-slate-200/40 cursor-pointer transition-[width,padding,gap,background-color] duration-200 ease-out motion-reduce:transition-none",
                        collapsed
                            ? "w-8 gap-0 pr-0.5 py-0.5"
                            : "w-[calc(100%-1.5rem)] gap-2.5 pr-1.5 py-1",
                    )}
                >
                    <div className="w-7 h-7 rounded-full bg-slate-900 flex items-center justify-center shrink-0 overflow-hidden">
                        {user.avatar_url ? (
                            <DashboardImage
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
                    <div
                        aria-hidden={collapsed || undefined}
                        className={cn(
                            "flex-1 min-w-0 overflow-hidden whitespace-nowrap text-left transition-opacity ease-out motion-reduce:transition-none",
                            collapsed ? "opacity-0 duration-100" : "opacity-100 duration-200 delay-75",
                        )}
                    >
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
                <div className="flex h-8 items-center justify-between gap-3 pl-3 pr-1.5">
                    <span className="text-[12.5px] text-slate-700">Theme</span>
                    <ThemeSwitch />
                </div>
                <PopoverMenuSeparator />
                <PopoverMenuItem
                    onSelect={() => navigate({ to: "/app/settings" })}
                    icon={<SettingsIcon className="w-3 h-3" />}
                >
                    Settings
                </PopoverMenuItem>
                <PopoverMenuItem
                    onSelect={() => setProductTourOpen(true)}
                    icon={<CompassIcon className="w-3 h-3" />}
                >
                    Take the tour
                </PopoverMenuItem>
                <PopoverMenuSeparator />
                <PopoverMenuItem
                    onSelect={handleLogout}
                    icon={<LogOutIcon className="w-3 h-3" />}
                    disabled={logoutMutation.isPending}
                    danger
                >
                    {logoutMutation.isPending ? "Signing out…" : "Log out"}
                </PopoverMenuItem>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
