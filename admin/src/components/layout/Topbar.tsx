// Sticky top bar. Holds the hamburger (mobile only), the env pill, the
// version pill (which becomes the update indicator), the search button that
// opens the command palette, and the user menu.
// The 3px admin stripe sits *above* this bar in AppShell so it's the
// first thing the eye lands on.

import { useState } from "react";
import { Menu, Search } from "lucide-react";
import { Button } from "@/components/ui/button";
import { IS_MAC, openCommandPalette } from "./CommandPalette";
import { EnvPill } from "./EnvPill";
import { MobileNav } from "./MobileNav";
import { UpdatePill } from "./UpdatePill";
import { UserMenu } from "./UserMenu";

export function Topbar() {
    const [mobileOpen, setMobileOpen] = useState(false);

    return (
        <header className="h-14 shrink-0 border-b border-border bg-background/80 backdrop-blur sticky top-0 z-30">
            <div className="h-full flex items-center gap-3 px-4">
                <Button
                    variant="ghost"
                    size="icon-sm"
                    className="md:hidden -ml-1"
                    aria-label="Open navigation"
                    onClick={() => setMobileOpen(true)}
                >
                    <Menu className="size-5" />
                </Button>
                <MobileNav open={mobileOpen} onOpenChange={setMobileOpen} />

                <div className="flex items-center gap-2">
                    <EnvPill />
                    <UpdatePill />
                </div>

                <div className="flex-1 flex justify-center">
                    <button
                        type="button"
                        onClick={openCommandPalette}
                        aria-label="Search and go to"
                        className="group inline-flex h-8 w-full max-w-md items-center gap-2 rounded-md border border-border bg-muted/40 px-2.5 text-[12.5px] text-muted-foreground transition-colors hover:border-[var(--admin-accent)] hover:bg-muted/60 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--admin-accent-soft)]"
                    >
                        <Search className="size-3.5 shrink-0" />
                        <span className="hidden sm:inline truncate">Search or go to</span>
                        <kbd className="ml-auto hidden sm:inline-flex items-center gap-0.5 rounded border border-border bg-background px-1.5 font-mono text-[10px] leading-4 text-muted-foreground">
                            {IS_MAC ? "⌘" : "Ctrl"}
                            <span>K</span>
                        </kbd>
                    </button>
                </div>

                <UserMenu />
            </div>
        </header>
    );
}
