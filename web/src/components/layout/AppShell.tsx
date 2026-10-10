// The new app shell.
//
// Layout:
//   ┌──────────────────────────────────────────────────────┐
//   │  [logo]  >  [org]  >  [section]               [⌘K ●] │  AppHeader
//   ├──────────┬───────────────────────────────────────────┤
//   │          │ ╭─── content ──────────────────────────╮  │
//   │  AppNav  │ │                                      │  │
//   │          │ │                                      │  │
//   │          │ │                                      │  │
//   └──────────┴───────────────────────────────────────────┘
//
// The header + sidebar share one sky-coloured chrome layer (SkyChrome).
// The content panel sits in the bottom-right with a rounded top-left
// where it meets the chrome's inner corner. Reads as one continuous
// frame around a clean work surface.

import { Suspense, useEffect, useRef, useState } from "react";
import { Outlet, useLocation } from "@tanstack/react-router";
import SubscriptionGate from "./SubscriptionGate";
import { SkyChrome } from "./SkyChrome";
import { AppHeader } from "./AppHeader";
import { AppNav } from "./AppNav";
import PendingDeletionBar from "./PendingDeletionBar";
import SendingRestrictedBar from "./SendingRestrictedBar";
import { RouteBoundary } from "./ErrorBoundary";
import { ShortcutsModal } from "@/components/shared/ShortcutsModal";
import { CommandPalette } from "@/components/shared/CommandPalette";
import { useKeyboardShortcuts } from "@/hooks/useKeyboardShortcuts";
import { GlobalCursorsProvider } from "@/components/app/presence/GlobalCursors";
import AgentPanel from "@/components/app/agent/AgentPanel";
import ProductTourHost from "@/components/app/tour/ProductTourHost";
import { useRouteKey } from "@/hooks/useRouteKey";
import { useScrollMemory, type ScrollStore } from "@/hooks/useScrollMemory";
import { RouteFallback } from "./RouteStates";

// Per history entry, kept apart so ordinary browsing never evicts a list's remembered offset.
const entryOffsets: ScrollStore = new Map();

export function AppShell() {
    useKeyboardShortcuts();

    // Mobile nav drawer. On >=md the sidebar is a static column and this is
    // ignored; below md it's an off-canvas drawer toggled from the header.
    const [navOpen, setNavOpen] = useState(false);
    const { pathname } = useLocation();
    // Close the drawer whenever the route changes (tapping a nav link).
    useEffect(() => setNavOpen(false), [pathname]);

    // The page content's scroll container, anchor for the global cursor layer.
    const scrollRef = useRef<HTMLDivElement>(null);

    // A new page starts at the top and Back lands where that entry was left; in-page URL state keeps its place.
    const routeKey = useRouteKey();
    const entryKey = useLocation({ select: (l) => l.state.__TSR_key ?? l.href });
    const [scrollFor, setScrollFor] = useState({ routeKey, key: `${routeKey}@${entryKey}` });
    if (scrollFor.routeKey !== routeKey) setScrollFor({ routeKey, key: `${routeKey}@${entryKey}` });
    useScrollMemory(scrollRef, scrollFor.key, entryOffsets);

    return (
        <div className="fixed inset-0 flex flex-col">
            <SkyChrome />

            <div className="relative z-10 flex flex-col h-full">
                {/* Sits above the header so it can't be missed. Only
                    renders when the current workspace or the user's
                    own account is scheduled for deletion. */}
                <SendingRestrictedBar />
                <PendingDeletionBar />

                <AppHeader onMenu={() => setNavOpen(true)} />

                <div className="flex-1 flex min-h-0">
                    <AppNav open={navOpen} onClose={() => setNavOpen(false)} />

                    {/* Content panel — pure white work surface. The inner
                        corner is softened (rounded-tl-2xl) only on >=md, where
                        the sidebar sits beside it; on mobile the panel is
                        full-bleed with just a top hairline. */}
                    <main className="wb-panel flex-1 min-w-0 bg-white overflow-hidden border-t border-slate-200/70 md:rounded-tl-2xl md:border-l">
                        {/* Dark theme only: a soft light from the top edge, laid over every page whatever it paints. */}
                        <div aria-hidden className="pointer-events-none relative z-20 hidden h-0 dark:block">
                            <div className="wb-panel-light absolute inset-x-0 top-0 h-[420px]" />
                        </div>
                        <GlobalCursorsProvider scrollRef={scrollRef}>
                            <div ref={scrollRef} className="h-full overflow-auto">
                                <RouteBoundary>
                                    {/* Catches a suspend above the route's own
                                        boundary (SubscriptionGate). */}
                                    <Suspense fallback={<RouteFallback />}>
                                        <SubscriptionGate>
                                            <Outlet />
                                        </SubscriptionGate>
                                    </Suspense>
                                </RouteBoundary>
                            </div>
                        </GlobalCursorsProvider>
                    </main>
                </div>
            </div>

            <ShortcutsModal />
            <CommandPalette />
            {/* Right-side AI assistant, persistent across routes. */}
            <AgentPanel />
            <ProductTourHost />
        </div>
    );
}
