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

                    {/* Content panel — pure white work surface that meets
                        the bottom and right edges of the viewport. Only the
                        inner corner is softened (rounded-tl-2xl), since
                        that's the only seam where chrome meets content.
                        A single hairline border on the top + left edges
                        defines the panel without a heavy shadow. */}
                    <main className="flex-1 min-w-0 rounded-tl-2xl bg-white overflow-hidden border-t border-l border-slate-200/70">
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
