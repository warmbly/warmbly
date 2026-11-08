// The agency view: its own header and sidebar around the agency pages, apart
// from any one workspace's chrome. It renders with no workspace picked.
//
//   ┌──────────────────────────────────────────────────────┐
//   │  [mark] Agency name              [pool] [AI] [🔔] [me] │  AgencyHeader
//   ├──────────┬───────────────────────────────────────────┤
//   │ AgencyNav│ ╭─── page ─────────────────────────────╮  │
//   │ CLIENTS  │ │                                      │  │
//   └──────────┴───────────────────────────────────────────┘

import { Suspense, useEffect, useRef, useState } from "react";
import { Outlet, useLocation } from "@tanstack/react-router";
import { Loader2Icon } from "lucide-react";
import { SkyChrome } from "../SkyChrome";
import { RouteBoundary } from "../ErrorBoundary";
import { RouteFallback } from "../RouteStates";
import { EmptyBlock, Page, PageTopbar } from "../Page";
import { AgencyHeader } from "./AgencyHeader";
import { AgencyNav } from "./AgencyNav";
import { CreateAgencyCard } from "@/components/app/agency/CreateAgencyCard";
import { useCurrentAgency } from "@/lib/api/hooks/app/agency/useAgency";
import { rememberView } from "@/lib/agencyView";
import { useRouteKey } from "@/hooks/useRouteKey";
import { useScrollMemory, type ScrollStore } from "@/hooks/useScrollMemory";

const entryOffsets: ScrollStore = new Map();

export function AgencyShell() {
    const [navOpen, setNavOpen] = useState(false);
    const { pathname } = useLocation();
    useEffect(() => setNavOpen(false), [pathname]);

    const { agency, agencies, isLoading, isError } = useCurrentAgency();
    // Reopening the app comes back here only once there is an agency to come back to.
    const agencyId = agency?.id;
    useEffect(() => {
        if (agencyId) rememberView("agency");
    }, [agencyId]);

    const scrollRef = useRef<HTMLDivElement>(null);
    const routeKey = useRouteKey();
    const entryKey = useLocation({ select: (l) => l.state.__TSR_key ?? l.href });
    const [scrollFor, setScrollFor] = useState({ routeKey, key: `${routeKey}@${entryKey}` });
    if (scrollFor.routeKey !== routeKey) setScrollFor({ routeKey, key: `${routeKey}@${entryKey}` });
    useScrollMemory(scrollRef, scrollFor.key, entryOffsets);

    return (
        <div className="fixed inset-0 flex flex-col">
            <SkyChrome />
            <div className="relative z-10 flex flex-col h-full">
                <AgencyHeader agency={agency} agencies={agencies} onMenu={agency ? () => setNavOpen(true) : undefined} />
                <div className="flex-1 flex min-h-0">
                    {agency && <AgencyNav agency={agency} open={navOpen} onClose={() => setNavOpen(false)} />}
                    <main className="wb-panel flex-1 min-w-0 bg-white overflow-hidden border-t border-slate-200/70 md:rounded-tl-2xl md:border-l">
                        <div aria-hidden className="pointer-events-none relative z-20 hidden h-0 dark:block">
                            <div className="wb-panel-light absolute inset-x-0 top-0 h-[420px]" />
                        </div>
                        <div ref={scrollRef} className="h-full overflow-auto">
                            {isLoading ? (
                                <div className="h-full flex items-center justify-center">
                                    <Loader2Icon className="w-4 h-4 animate-spin text-slate-400" />
                                </div>
                            ) : isError && !agency ? (
                                <EmptyBlock title="Could not load your agency" body="Check your connection and reload the page." />
                            ) : !agency ? (
                                <Page>
                                    <PageTopbar eyebrow="Agency" subtitle="Set up" />
                                    <CreateAgencyCard />
                                </Page>
                            ) : (
                                <RouteBoundary>
                                    <Suspense fallback={<RouteFallback />}>
                                        <Outlet />
                                    </Suspense>
                                </RouteBoundary>
                            )}
                        </div>
                    </main>
                </div>
            </div>
        </div>
    );
}
