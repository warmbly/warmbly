// Picks the shell: agency pages get the agency's own chrome, everything else
// the workspace's. The theme is applied app-wide in RootLayout.

import { useLocation } from "@tanstack/react-router";
import { AppShell } from "./AppShell";
import { AgencyShell } from "./agency/AgencyShell";
import { isAgencyPath } from "@/lib/agencyView";

export function AppLayout() {
    const agency = useLocation({ select: (l) => isAgencyPath(l.pathname) });
    return agency ? <AgencyShell /> : <AppShell />;
}
