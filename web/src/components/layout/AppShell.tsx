// App shell — Linear-style.
//
//   ┌────────────────────────────────────────────────────────┐
//   │ AppNav │ AppHeader (slim toolbar)                       │
//   │        ├────────────────────────────────────────────────┤
//   │        │                                                │
//   │        │ content                                        │
//   │        │                                                │
//   └────────┴────────────────────────────────────────────────┘
//
// Sidebar is full-height on the left with a hairline right border.
// Header is a slim top strip in the content column. Everything is
// white-on-white with #e5e7eb hairlines — no chrome, no rounded
// content panel, no decorative background.

import { Outlet } from "react-router-dom";
import { AppHeader } from "./AppHeader";
import { AppNav } from "./AppNav";
import { ShortcutsModal } from "@/components/shared/ShortcutsModal";
import { CommandPalette } from "@/components/shared/CommandPalette";
import { useKeyboardShortcuts } from "@/hooks/useKeyboardShortcuts";

export function AppShell() {
    useKeyboardShortcuts();

    return (
        <div className="fixed inset-0 flex bg-white text-slate-900">
            <AppNav />

            <div className="flex-1 min-w-0 flex flex-col">
                <AppHeader />
                <main className="flex-1 min-h-0 overflow-auto">
                    <Outlet />
                </main>
            </div>

            <ShortcutsModal />
            <CommandPalette />
        </div>
    );
}
