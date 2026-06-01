// Authenticated app shell: 3px admin stripe → sidebar + topbar + outlet.
// Route guard lives in RequireAdmin, which wraps this in main.tsx.

import { Outlet } from "react-router-dom";
import { Sidebar } from "./Sidebar";
import { Topbar } from "./Topbar";

export function AppShell() {
    return (
        <div className="min-h-screen flex flex-col bg-background text-foreground">
            {/* The 3px stripe across the very top of the viewport. Cheap,
                always-visible signal that the user is in the admin surface. */}
            <div className="h-[3px] w-full admin-stripe shrink-0" />

            <div className="flex flex-1 min-h-0">
                <Sidebar />
                <div className="flex-1 flex flex-col min-w-0">
                    <Topbar />
                    <main className="flex-1 overflow-y-auto">
                        <div className="w-full px-4 md:px-6 lg:px-8 py-6">
                            <Outlet />
                        </div>
                    </main>
                </div>
            </div>
        </div>
    );
}
