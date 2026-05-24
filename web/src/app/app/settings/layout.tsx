// Settings layout — left rail with real router links.
//
// Replaces the hash-based section switch. Each section is now a real
// route so:
//   - /app/settings/profile
//   - /app/settings/notifications
//   - /app/settings/security
//   - /app/settings/members
//   - /app/settings/workspace   (owner-only)
//   - /app/settings/danger
//
// The route table renders this layout once and swaps the right pane
// via <Outlet />. Visiting /app/settings (no section) redirects to
// /profile so a deep link or back-button always lands on a real
// section.

import { Navigate, NavLink, Outlet, useLocation } from "react-router-dom";
import {
    AlertOctagonIcon,
    BellIcon,
    BriefcaseIcon,
    CreditCardIcon,
    ShieldCheckIcon,
    ShieldIcon,
    UserIcon,
    UsersIcon,
} from "lucide-react";
import { Page, PageTopbar } from "@/components/layout/Page";
import useFeatureAccess from "@/hooks/useFeatureAccess";

interface SectionDef {
    path: string;
    label: string;
    icon: React.ComponentType<{ className?: string }>;
    description: string;
    ownerOnly?: boolean;
}

const SECTIONS: SectionDef[] = [
    { path: "profile",       label: "Profile",       icon: UserIcon,         description: "Personal information." },
    { path: "notifications", label: "Notifications", icon: BellIcon,         description: "What you get notified about." },
    { path: "security",      label: "Security",      icon: ShieldIcon,       description: "Password, 2FA, active sessions." },
    { path: "members",       label: "Members",       icon: UsersIcon,        description: "Team and invitations." },
    { path: "roles",         label: "Roles & access", icon: ShieldCheckIcon,  description: "Who can do what.", ownerOnly: true },
    { path: "workspace",     label: "Workspace",     icon: BriefcaseIcon,    description: "Org-wide settings.", ownerOnly: true },
    { path: "billing",       label: "Billing",       icon: CreditCardIcon,   description: "Plan, payment, invoices.", ownerOnly: true },
    { path: "danger",        label: "Danger zone",   icon: AlertOctagonIcon, description: "Irreversible actions." },
];

export default function SettingsLayout() {
    const location = useLocation();
    const access = useFeatureAccess();

    // When user hits bare /app/settings, route them to the default
    // section so the URL stays meaningful. Done at layout level so
    // it works for every entry path.
    if (
        location.pathname === "/app/settings" ||
        location.pathname === "/app/settings/"
    ) {
        return <Navigate to="/app/settings/profile" replace />;
    }

    const visibleSections = SECTIONS.filter((s) => !s.ownerOnly || access.isOwner);
    const currentPath = location.pathname.replace(/^\/app\/settings\//, "");
    const current =
        visibleSections.find((s) => s.path === currentPath) ?? visibleSections[0];

    return (
        <Page>
            <PageTopbar
                eyebrow="Settings"
                subtitle={current?.description ?? "Account and workspace"}
            />

            <div className="flex-1 min-h-0 flex">
                <nav className="w-[220px] shrink-0 border-r border-slate-200/70 py-2 overflow-y-auto">
                    {visibleSections.map((s) => (
                        <NavLink
                            key={s.path}
                            to={`/app/settings/${s.path}`}
                            className={({ isActive }) =>
                                `group block w-[calc(100%-0.75rem)] mx-1.5 my-px flex items-center gap-2 px-2 h-7 rounded text-[12.5px] text-left transition-colors ${
                                    isActive
                                        ? "bg-slate-200/70 text-slate-900 font-medium"
                                        : "text-slate-600 hover:text-slate-900 hover:bg-slate-200/40"
                                }`
                            }
                        >
                            {({ isActive }) => (
                                <>
                                    <s.icon
                                        className={`w-[14px] h-[14px] shrink-0 ${
                                            isActive
                                                ? "text-slate-700"
                                                : "text-slate-400 group-hover:text-slate-600"
                                        }`}
                                    />
                                    <span className="truncate">{s.label}</span>
                                </>
                            )}
                        </NavLink>
                    ))}
                </nav>

                <div className="flex-1 min-w-0 overflow-y-auto">
                    <Outlet />
                </div>
            </div>
        </Page>
    );
}
