// Thin wrapper: pulls in the theme provider and renders the new shell.
// All the actual layout lives in AppShell — sidebar + header + content.

import { AppShell } from "./AppShell";
import { ThemeProvider } from "./ThemeProvider";

export function AppLayout() {
    return (
        <ThemeProvider>
            <AppShell />
        </ThemeProvider>
    );
}
