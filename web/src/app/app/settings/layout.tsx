// Settings layout — grouped left rail with real router links.
//
// Each section is a real route (/app/settings/<section>). The rail groups them
// under small section labels, marks the active row with a single framer-motion
// pill that slides between rows, and cross-fades the right pane on every section
// change. Visiting bare /app/settings redirects to /profile so a deep link or
// back-button always lands on a real section. Navigating away from a tab with
// unsaved auto-save changes is blocked by the dialog at the bottom.

import React from "react";
import { Navigate, NavLink, Outlet, useBlocker, useLocation } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import {
    AlertOctagonIcon,
    BellIcon,
    BoxesIcon,
    BriefcaseIcon,
    CreditCardIcon,
    GaugeIcon,
    GiftIcon,
    Loader2Icon,
    ShieldCheckIcon,
    ShieldIcon,
    UserIcon,
    UsersIcon,
    WebhookIcon,
} from "lucide-react";
import { UnsavedProvider, useUnsavedRegistry } from "@/hooks/context/unsaved";
import { usePermission, type PermissionKey } from "@/hooks/usePermission";
import { Page, PageTopbar } from "@/components/layout/Page";
import useFeatureAccess from "@/hooks/useFeatureAccess";

interface SectionDef {
    path: string;
    label: string;
    icon: React.ComponentType<{ className?: string }>;
    description: string;
    ownerOnly?: boolean;
    permission?: PermissionKey;
}

interface SectionGroup {
    label: string;
    items: SectionDef[];
}

const GROUPS: SectionGroup[] = [
    {
        label: "Account",
        items: [
            { path: "profile", label: "Profile", icon: UserIcon, description: "Personal information." },
            { path: "notifications", label: "Notifications", icon: BellIcon, description: "What you get notified about." },
            { path: "security", label: "Security", icon: ShieldIcon, description: "Password, 2FA, active sessions." },
        ],
    },
    {
        label: "Workspace",
        items: [
            { path: "members", label: "Members", icon: UsersIcon, description: "Team and invitations." },
            { path: "teams", label: "Teams", icon: UsersIcon, description: "Group members into teams." },
            { path: "roles", label: "Roles & access", icon: ShieldCheckIcon, description: "Who can do what.", ownerOnly: true },
            { path: "workspace", label: "Workspace", icon: BriefcaseIcon, description: "Org-wide settings.", ownerOnly: true },
            { path: "billing", label: "Billing", icon: CreditCardIcon, description: "Plan, payment, invoices.", ownerOnly: true },
            { path: "referral", label: "Refer & earn", icon: GiftIcon, description: "Invite teams and earn account credit.", ownerOnly: true },
            { path: "limits", label: "Limits", icon: GaugeIcon, description: "Request more capacity than your plan allows.", ownerOnly: true },
        ],
    },
    {
        label: "Developers",
        items: [
            { path: "oauth-apps", label: "OAuth apps", icon: BoxesIcon, description: "Apps that connect via OAuth2, and the apps you've authorized.", permission: "MANAGE_API_KEYS" },
            { path: "webhooks", label: "Webhooks", icon: WebhookIcon, description: "Realtime HTTP callbacks for workspace events.", permission: "MANAGE_SETTINGS" },
        ],
    },
    {
        label: "Advanced",
        items: [
            { path: "danger", label: "Danger zone", icon: AlertOctagonIcon, description: "Irreversible actions." },
        ],
    },
];

export default function SettingsLayout() {
    return (
        <UnsavedProvider>
            <SettingsLayoutInner />
        </UnsavedProvider>
    );
}

function SettingsLayoutInner() {
    const location = useLocation();
    const access = useFeatureAccess();
    const canManageApiKeys = usePermission("MANAGE_API_KEYS");
    const canManageSettings = usePermission("MANAGE_SETTINGS");
    const navRef = React.useRef<HTMLElement>(null);
    const unsaved = useUnsavedRegistry();
    const [savingLeave, setSavingLeave] = React.useState(false);

    // Block in-app navigation away from a tab with unsaved/pending/failed
    // auto-save changes; the dialog below offers save or discard.
    const blocker = useBlocker(
        React.useCallback(
            ({ currentLocation, nextLocation }: { currentLocation: { pathname: string }; nextLocation: { pathname: string } }) =>
                !!unsaved?.anyDirty() && currentLocation.pathname !== nextLocation.pathname,
            [unsaved],
        ),
    );

    // Native guard for hard navigations (reload / close tab).
    React.useEffect(() => {
        const handler = (e: BeforeUnloadEvent) => {
            if (unsaved?.anyDirty()) {
                e.preventDefault();
                e.returnValue = "";
            }
        };
        window.addEventListener("beforeunload", handler);
        return () => window.removeEventListener("beforeunload", handler);
    }, [unsaved]);

    async function saveAndLeave() {
        if (!unsaved) return;
        setSavingLeave(true);
        try {
            await unsaved.saveAll();
            setSavingLeave(false);
            blocker.proceed?.();
        } catch {
            setSavingLeave(false);
            toast.error("Couldn't save your changes — fix them or discard to leave.");
        }
    }
    function discardAndLeave() {
        unsaved?.discardAll();
        blocker.proceed?.();
    }

    // On phones the nav is a horizontal tab strip; deep links or navigation to a
    // later section should bring the active tab into view, otherwise the strip
    // sits scrolled to the start with no hint of where you are. No-op on >=md.
    React.useEffect(() => {
        if (window.matchMedia("(min-width: 768px)").matches) return;
        const el = navRef.current?.querySelector<HTMLElement>('[aria-current="page"]');
        el?.scrollIntoView({ inline: "center", block: "nearest" });
    }, [location.pathname]);

    if (
        location.pathname === "/app/settings" ||
        location.pathname === "/app/settings/"
    ) {
        return <Navigate to="/app/settings/profile" replace />;
    }

    const visibleGroups = GROUPS.map((g) => ({
        ...g,
        items: g.items.filter(
            (s) =>
                (!s.ownerOnly || access.isOwner) &&
                (s.permission !== "MANAGE_API_KEYS" || canManageApiKeys) &&
                (s.permission !== "MANAGE_SETTINGS" || canManageSettings),
        ),
    })).filter((g) => g.items.length > 0);

    const currentPath = location.pathname.replace(/^\/app\/settings\//, "");
    const allItems = visibleGroups.flatMap((g) => g.items);
    const current = allItems.find((s) => s.path === currentPath) ?? allItems[0];

    return (
        <Page className="h-full min-h-0">
            <PageTopbar
                eyebrow="Settings"
                subtitle={current?.description ?? "Account and workspace"}
            />

            <div className="flex-1 min-h-0 flex flex-col md:flex-row">
                {/* Mobile: a horizontally-scrollable tab strip. >=md: vertical rail. */}
                <nav
                    ref={navRef}
                    className="flex md:flex-col shrink-0 gap-0.5 md:gap-0 overflow-x-auto md:overflow-y-auto border-b md:border-b-0 md:border-r border-slate-200/70 px-2 md:px-2.5 py-2 md:py-3 md:w-[236px]"
                >
                    {visibleGroups.map((g, gi) => (
                        <div key={g.label} className="contents md:block md:mb-1">
                            <div className={`hidden md:block px-2 ${gi === 0 ? "mb-1" : "mt-3 mb-1"}`}>
                                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                    {g.label}
                                </span>
                            </div>
                            {g.items.map((s) => (
                                <SectionLink key={s.path} section={s} />
                            ))}
                        </div>
                    ))}
                </nav>

                <div className="flex-1 min-w-0 overflow-y-auto">
                    <AnimatePresence mode="wait" initial={false}>
                        <motion.div
                            key={location.pathname}
                            initial={{ opacity: 0, y: 6 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: -4 }}
                            transition={{ duration: 0.16, ease: "easeOut" }}
                        >
                            <Outlet />
                        </motion.div>
                    </AnimatePresence>
                </div>
            </div>

            <AnimatePresence>
                {blocker.state === "blocked" && (
                    <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        className="fixed inset-0 z-50 bg-slate-900/30 flex items-center justify-center p-4"
                        onMouseDown={(e) => {
                            if (e.target === e.currentTarget && !savingLeave) blocker.reset?.();
                        }}
                    >
                        <motion.div
                            initial={{ opacity: 0, y: 8, scale: 0.98 }}
                            animate={{ opacity: 1, y: 0, scale: 1 }}
                            exit={{ opacity: 0, y: 8, scale: 0.98 }}
                            className="w-full max-w-sm rounded-lg bg-white border border-slate-200 shadow-xl p-5"
                        >
                            <h3 className="text-[14px] font-semibold text-slate-900">Unsaved changes</h3>
                            <p className="text-[12.5px] text-slate-500 leading-relaxed mt-1">
                                Some changes on this tab haven't finished saving. Save them before leaving, or discard them.
                            </p>
                            <div className="mt-4 flex items-center justify-end gap-2">
                                <button
                                    type="button"
                                    onClick={() => blocker.reset?.()}
                                    disabled={savingLeave}
                                    className="h-8 px-3 rounded-md text-[12.5px] font-medium text-slate-600 hover:bg-slate-100 transition-colors disabled:opacity-60"
                                >
                                    Stay
                                </button>
                                <button
                                    type="button"
                                    onClick={discardAndLeave}
                                    disabled={savingLeave}
                                    className="h-8 px-3 rounded-md text-[12.5px] font-medium text-rose-600 hover:bg-rose-50 transition-colors disabled:opacity-60"
                                >
                                    Discard
                                </button>
                                <button
                                    type="button"
                                    onClick={saveAndLeave}
                                    disabled={savingLeave}
                                    className="h-8 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                >
                                    {savingLeave && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                                    Save changes
                                </button>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>
        </Page>
    );
}

function SectionLink({ section }: { section: SectionDef }) {
    return (
        <NavLink
            to={`/app/settings/${section.path}`}
            className={({ isActive }) =>
                `group relative shrink-0 md:w-full flex items-center gap-2.5 px-2.5 h-8 rounded-md text-[12.5px] whitespace-nowrap text-left transition-colors ${
                    isActive ? "text-slate-900 font-medium" : "text-slate-600 hover:text-slate-900 hover:bg-slate-200/40"
                }`
            }
        >
            {({ isActive }) => (
                <>
                    {isActive && (
                        <motion.span
                            layoutId="settings-active-pill"
                            className="absolute inset-0 rounded-md bg-slate-200/70"
                            transition={{ type: "spring", stiffness: 520, damping: 42 }}
                        />
                    )}
                    <section.icon
                        className={`relative z-10 w-[14px] h-[14px] shrink-0 ${
                            isActive ? "text-slate-700" : "text-slate-400 group-hover:text-slate-600"
                        }`}
                    />
                    <span className="relative z-10 truncate">{section.label}</span>
                </>
            )}
        </NavLink>
    );
}
