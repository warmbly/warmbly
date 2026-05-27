// Sticky top bar. Holds the env pill, a search slot (unused for now —
// reserved for the cmd-K palette we'll add later), and the user menu.
// The 3px admin stripe sits *above* this bar in AppShell so it's the
// first thing the eye lands on.

import { EnvPill } from "./EnvPill";
import { UserMenu } from "./UserMenu";

export function Topbar() {
    return (
        <header className="h-14 shrink-0 border-b border-border bg-background/80 backdrop-blur sticky top-0 z-30">
            <div className="h-full flex items-center gap-3 px-4">
                <div className="flex items-center gap-2">
                    <EnvPill />
                    <span className="text-xs text-muted-foreground hidden sm:inline">
                        Connected to <code className="text-foreground font-mono">/admin/*</code>
                    </span>
                </div>
                <div className="flex-1" />
                <UserMenu />
            </div>
        </header>
    );
}
