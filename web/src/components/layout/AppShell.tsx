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

import { Outlet } from "react-router-dom";
import { SkyChrome } from "./SkyChrome";
import { AppHeader } from "./AppHeader";
import { AppNav } from "./AppNav";
import { ShortcutsModal } from "@/components/shared/ShortcutsModal";
import { CommandPalette } from "@/components/shared/CommandPalette";
import { useKeyboardShortcuts } from "@/hooks/useKeyboardShortcuts";

export function AppShell() {
    useKeyboardShortcuts();

    return (
        <div className="fixed inset-0 flex flex-col">
            <SkyChrome />

            <div className="relative z-10 flex flex-col h-full">
                <AppHeader />

                <div className="flex-1 flex min-h-0">
                    <AppNav />

                    {/* Content panel — white surface tucked into the inner
                        corner of the L-shape with a generous top-left
                        radius. The 4px right/bottom inset gives the chrome a
                        sliver of visible sky at every edge except the corner
                        seam. */}
                    <main className="flex-1 min-w-0 mr-1 mb-1 rounded-tl-2xl bg-white overflow-hidden">
                        <div className="h-full overflow-auto">
                            <Outlet />
                        </div>
                    </main>
                </div>
            </div>

            <ShortcutsModal />
            <CommandPalette />
        </div>
    );
}
