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

import { useEffect, useRef, useState } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { SkyChrome } from "./SkyChrome";
import { AppHeader } from "./AppHeader";
import { AppNav } from "./AppNav";
import PendingDeletionBar from "./PendingDeletionBar";
import { RouteBoundary } from "./ErrorBoundary";
import { ShortcutsModal } from "@/components/shared/ShortcutsModal";
import { CommandPalette } from "@/components/shared/CommandPalette";
import { useKeyboardShortcuts } from "@/hooks/useKeyboardShortcuts";
import { GlobalCursorsProvider } from "@/components/app/presence/GlobalCursors";
import AgentPanel from "@/components/app/agent/AgentPanel";

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

    return (
        <div className="fixed inset-0 flex flex-col">
            <SkyChrome />

            <div className="relative z-10 flex flex-col h-full">
                {/* Sits above the header so it can't be missed. Only
                    renders when the current workspace or the user's
                    own account is scheduled for deletion. */}
                <PendingDeletionBar />

                <AppHeader onMenu={() => setNavOpen(true)} />

                <div className="flex-1 flex min-h-0">
                    <AppNav open={navOpen} onClose={() => setNavOpen(false)} />

                    {/* Content panel — pure white work surface. The inner
                        corner is softened (rounded-tl-2xl) only on >=md, where
                        the sidebar sits beside it; on mobile the panel is
                        full-bleed with just a top hairline. */}
                    <main className="flex-1 min-w-0 bg-white overflow-hidden border-t border-slate-200/70 md:rounded-tl-2xl md:border-l">
                        <GlobalCursorsProvider scrollRef={scrollRef}>
                            <div ref={scrollRef} className="h-full overflow-auto">
                                <RouteBoundary>
                                    <Outlet />
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
        </div>
    );
}
