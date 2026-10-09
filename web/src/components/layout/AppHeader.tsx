// Top breadcrumb bar.
//
// Reads as one continuous line across the entire top of the shell:
//
//   [Warmbly logo]  >  [Org picker]  >  [Current section]      [⌘K  ⚡]
//
// The logo sits over the sidebar column, the org picker + section live
// in the open area, the right side has connection indicator + search.
// All on the sky-colored chrome — text is white-ish, dividers are faint.
//
// This component is purely the row. Layout (where it sits) is decided by
// AppShell, not here.

import { useRef } from "react";
import { Link } from "@tanstack/react-router";
import { ChevronRight, Menu, Search } from "lucide-react";
import { Logo } from "@/components/svg";
import AgentMark from "@/components/app/agent/AgentMark";
import RemieTip from "@/components/app/agent/RemieTip";
import { useAppStore } from "@/stores";
import { ConnectionIndicator } from "@/components/shared/ConnectionIndicator";
import { useAccessRestricted, usePermission } from "@/hooks/usePermission";
import ShortcutTooltip from "@/components/ui/shortcut-tooltip";
import PresenceAvatars from "@/components/app/presence/PresenceAvatars";
import OutboxIndicator from "@/components/app/unibox/compose/OutboxIndicator";
import { NotificationBell } from "./NotificationBell";
import { OrgSwitcher } from "./OrgSwitcher";
import { BetaPill } from "./BetaPill";
import { PlanPill } from "./PlanPill";
import { VersionPill } from "./VersionPill";
import { CreditsMeter } from "./CreditsMeter";
import { useIsMobile } from "@/hooks/use-mobile";
import { cn } from "@/lib/utils";
import { hrefTarget } from "@/lib/routerSearch";
import { useHeaderBreadcrumbs } from "@/hooks/useHeaderBreadcrumbs";

export function AppHeader({ onMenu }: { onMenu?: () => void }) {
    const crumbs = useHeaderBreadcrumbs();
    const restricted = useAccessRestricted();
    const setCommandPaletteOpen = useAppStore((s) => s.setCommandPaletteOpen);
    // The logo zone spans the sidebar column, so it has to collapse with it or
    // the breadcrumb stops lining up with the content panel below.
    const isMobile = useIsMobile();
    const navCollapsed = useAppStore((s) => s.navCollapsed) && !isMobile;

    return (
        <div className="h-14 flex items-center shrink-0">
            {/* Logo zone — sidebar-width on >=md, compact with a menu button
                on mobile (the sidebar collapses into a drawer below md). */}
            <button
                type="button"
                onClick={onMenu}
                aria-label="Open menu"
                className="md:hidden ml-1.5 w-9 h-9 rounded-md flex items-center justify-center text-slate-600 hover:text-slate-900 hover:bg-slate-200/60 transition-colors shrink-0"
            >
                <Menu className="w-5 h-5" />
            </button>
            <Link
                to="/app/emails"
                className={cn(
                    "h-full flex items-center gap-2.5 shrink-0 group pl-2 pr-3 md:transition-[width,padding] md:duration-200 md:ease-out",
                    navCollapsed ? "md:w-14 md:px-0 md:justify-center" : "md:w-64 md:px-5",
                )}
            >
                {/* Cool blue-leaning gray at rest; deeper blue-gray on hover.
                    Light enough to read as neutral chrome, but with a clear
                    blue lean so the brand sneaks in. */}
                {/* Logo color tuned to read as a real brand mark, not
                    a washed-out accent. Deep slate (#0f172a) at rest +
                    slight warm shift on hover. The earlier blue-gray
                    was too pale and competed with the chrome rather
                    than anchoring it. */}
                <Logo className="w-7 text-slate-900 group-hover:text-slate-700 transition-colors duration-150" />
                {/* Wordmark hides on mobile — the mark + the drawer's own brand
                    header carry it there, leaving room for the workspace pill. */}
                <span
                    style={{ fontFamily: "var(--font-display)" }}
                    className={cn(
                        "wb-title font-extrabold text-[15.5px] tracking-tight text-slate-900",
                        navCollapsed ? "hidden" : "hidden md:inline",
                    )}
                >
                    Warmbly
                </span>
            </Link>

            {/* Breadcrumb: org switcher (always) > section > subpages. The
                section crumbs are redundant with each page's own title on a
                phone, so they only show on >=md. */}
            <div className="flex items-center gap-1.5 min-w-0 flex-1 pr-2 md:pr-4">
                <div className="min-w-0 md:shrink-0 max-w-32 lg:max-w-48">
                    <OrgSwitcher />
                </div>
                <nav aria-label="Breadcrumb" className="hidden md:block min-w-0">
                    <ol className="flex items-center gap-2 min-w-0">
                        {crumbs.map(({ label, to, current }, index) => (
                            <li key={to} className="flex items-center gap-2 min-w-0">
                                <ChevronRight aria-hidden="true" className="w-3.5 h-3.5 text-slate-300 shrink-0" />
                                {current ? (
                                    <span aria-current="page" title={label} className="text-[13px] font-medium text-slate-900 truncate">
                                        {label}
                                    </span>
                                ) : (
                                    <Link
                                        {...hrefTarget(to)}
                                        title={label}
                                        className={cn(
                                            "text-[13px] hover:text-slate-900 truncate transition-colors",
                                            index === crumbs.length - 1 ? "font-medium text-slate-900" : "text-slate-500",
                                        )}
                                    >
                                        {label}
                                    </Link>
                                )}
                            </li>
                        ))}
                    </ol>
                </nav>
            </div>

            <div className="flex items-center gap-1 sm:gap-2 px-2 sm:px-4 shrink-0">
                {/* Outside the sm-only group on purpose: once the dialog is
                    dismissed this pill is the only way back to it, and a phone
                    is exactly where someone dismisses it fastest. */}
                <BetaPill />
                <div className="hidden sm:flex items-center gap-2">
                    <div className="hidden lg:contents"><PlanPill /></div>
                    <VersionPill />
                    {!restricted && <div className="hidden lg:contents"><CreditsMeter /></div>}
                    <div className="hidden lg:block h-4 w-px bg-slate-200/80" />
                </div>
                <OutboxIndicator />
                <PresenceAvatars />
                <ConnectionIndicator />
                <NotificationBell />
                <AssistantButton />
                <button
                    type="button"
                    aria-label="Search"
                    onClick={() => setCommandPaletteOpen(true)}
                    className="flex items-center gap-2 px-2 h-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-200/60 transition-colors text-[12.5px]"
                >
                    <Search className="w-3.5 h-3.5" />
                    <span className="hidden xl:inline">Search</span>
                    <kbd className="hidden xl:inline-flex h-4 items-center px-1 rounded border border-slate-300/70 bg-white/60 font-mono text-[10px] text-slate-500 ml-0.5">
                        ⌘K
                    </kbd>
                </button>
            </div>
        </div>
    );
}

// Remie's toggle. Working, waiting on an approval and finishing all show on the
// blob itself, the dot marks an unread reply, and tips hang off it (RemieTip).
function AssistantButton() {
    const open = useAppStore((s) => s.aiAssistantOpen);
    const minimized = useAppStore((s) => s.agentMinimized);
    const setOpen = useAppStore((s) => s.setAIAssistantOpen);
    const setMinimized = useAppStore((s) => s.setAgentMinimized);
    const tabs = useAppStore((s) => s.agentTabs);
    const canAI = usePermission("USE_AI");
    const lastRunOk = useAppStore((s) => s.agentLastRunOk);
    const anchor = useRef<HTMLDivElement>(null);

    if (!canAI) return null;

    const running = tabs.some((t) => t.running);
    const pending = tabs.some((t) => t.pending);
    const unseen = tabs.some((t) => t.unseen);

    return (
        // Relative so Remie's tip bubble can hang off the blob.
        <div ref={anchor} className="relative">
        <ShortcutTooltip label="Ask Remie" combo="mod+I" side="bottom">
        <button
            onClick={() => {
                if (open && minimized) {
                    // Docked: bring the panel back instead of closing it.
                    setMinimized(false);
                } else if (open) {
                    setOpen(false);
                } else {
                    setMinimized(false);
                    setOpen(true);
                }
            }}
            aria-label="Ask Remie"
            className="group relative flex items-center justify-center size-7 rounded-md hover:bg-sky-50 transition-colors"
        >
            <AgentMark
                size={20}
                celebrate={lastRunOk}
                state={pending ? "attention" : running ? "thinking" : "idle"}
            />
            {/* Working and waiting show on the mark itself; the dot is an unread reply. */}
            {unseen && !running && !pending && (
                <span className="absolute top-0.5 right-0.5 size-1.5 rounded-full bg-sky-500 ring-2 ring-white" />
            )}
        </button>
        </ShortcutTooltip>
        <RemieTip anchor={anchor} />
        </div>
    );
}
