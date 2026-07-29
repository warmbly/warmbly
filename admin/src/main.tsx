import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./global.css";
import {
    createBrowserRouter,
    Navigate,
    Outlet,
    RouterProvider,
} from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";

import "@fontsource/inter/400.css";
import "@fontsource/inter/600.css";
import "@fontsource/poppins/600.css";
import "@fontsource/poppins/700.css";

import { Toaster } from "@/components/ui/sonner";
import { AppShell } from "@/components/layout/AppShell";
import { RequireAdmin } from "@/components/layout/RequireAdmin";

import LoginPage from "@/app/auth/LoginPage";
import OverviewPage from "@/app/dashboard/OverviewPage";
import WorkersPage from "@/app/dashboard/WorkersPage";
import WorkerDetailPage from "@/app/dashboard/WorkerDetailPage";
import AuditPage from "@/app/dashboard/AuditPage";
import OrganizationsPage from "@/app/dashboard/OrganizationsPage";
import OrganizationDetailPage from "@/app/dashboard/OrganizationDetailPage";
import UsersPage from "@/app/dashboard/UsersPage";
import UserDetailPage from "@/app/dashboard/UserDetailPage";
import WarmupPage from "@/app/dashboard/WarmupPage";
import WarmupAppealsPage from "@/app/dashboard/WarmupAppealsPage";
import WarmupContentLayout from "@/app/dashboard/warmup-content/WarmupContentLayout";
import WarmupContentOverviewPage from "@/app/dashboard/warmup-content/OverviewPage";
import WarmupContentLibraryPage from "@/app/dashboard/warmup-content/LibraryPage";
import WarmupContentJobsPage from "@/app/dashboard/warmup-content/JobsPage";
import CampaignsPage from "@/app/dashboard/CampaignsPage";
import LimitRequestsPage from "@/app/dashboard/LimitRequestsPage";
import OutreachPage from "@/app/dashboard/OutreachPage";
import AnalyticsPage from "@/app/dashboard/AnalyticsPage";
import MailboxesPage from "@/app/dashboard/MailboxesPage";
import EventsPage from "@/app/dashboard/EventsPage";
import SystemStatusPage from "@/app/dashboard/SystemStatusPage";
import RealtimeManager from "@/lib/realtime/RealtimeManager";
import { NotFoundPage } from "@/app/dashboard/StubPages";

// Mirror of web/src/main.tsx's tuned defaults. The admin app sees less
// traffic than the dashboard, so the staleness window is a touch wider:
//   - staleTime: 60s — counters and lists are fine for a minute
//   - gcTime: 5min  — keep navigation snappy on tab-back
//   - refetchOnWindowFocus: false — admin tabs sit in background all
//     day; we don't want a thundering herd of refetches on focus
//   - retry: 1 — same reasoning as the dashboard
const queryClient = new QueryClient({
    defaultOptions: {
        queries: {
            staleTime: 60_000,
            gcTime: 5 * 60_000,
            refetchOnWindowFocus: false,
            refetchOnReconnect: "always",
            retry: 1,
        },
        mutations: { retry: 0 },
    },
});

const router = createBrowserRouter([
    {
        path: "/auth/login",
        element: <LoginPage />,
    },
    {
        path: "/",
        element: <RequireAdmin />,
        children: [
            {
                element: <AppShellWithKey />,
                children: [
                    { index: true, element: <OverviewPage /> },
                    { path: "workers", element: <WorkersPage /> },
                    { path: "workers/:id", element: <WorkerDetailPage /> },
                    { path: "mailboxes", element: <MailboxesPage /> },
                    { path: "users", element: <UsersPage /> },
                    { path: "users/:id", element: <UserDetailPage /> },
                    { path: "organizations", element: <OrganizationsPage /> },
                    { path: "organizations/:id", element: <OrganizationDetailPage /> },
                    { path: "warmup", element: <WarmupPage /> },
                    { path: "warmup/appeals", element: <WarmupAppealsPage /> },
                    {
                        path: "warmup-content",
                        element: <WarmupContentLayout />,
                        children: [
                            {
                                index: true,
                                element: (
                                    <Navigate to="/warmup-content/overview" replace />
                                ),
                            },
                            { path: "overview", element: <WarmupContentOverviewPage /> },
                            { path: "library", element: <WarmupContentLibraryPage /> },
                            { path: "jobs", element: <WarmupContentJobsPage /> },
                        ],
                    },
                    { path: "campaigns", element: <CampaignsPage /> },
                    { path: "limit-requests", element: <LimitRequestsPage /> },
                    { path: "outreach", element: <OutreachPage /> },
                    { path: "analytics", element: <AnalyticsPage /> },
                    { path: "events", element: <EventsPage /> },
                    { path: "system", element: <SystemStatusPage /> },
                    { path: "audit", element: <AuditPage /> },
                    { path: "*", element: <NotFoundPage /> },
                ],
            },
        ],
    },
]);

// AppShell renders an <Outlet/> for the page. RealtimeManager is mounted
// here (inside QueryClientProvider, only for authenticated admins) so the
// admin:platform socket connects once and survives route changes.
function AppShellWithKey() {
    return (
        <>
            <RealtimeManager />
            <AppShell />
        </>
    );
}

// Tiny outlet helper exported so React-Router's typing is happy when
// we need a passthrough.
export { Outlet };

createRoot(document.getElementById("root")!).render(
    <StrictMode>
        <QueryClientProvider client={queryClient}>
            <RouterProvider router={router} />
            <Toaster position="top-center" />
            {import.meta.env.DEV && <ReactQueryDevtools initialIsOpen={false} />}
        </QueryClientProvider>
    </StrictMode>,
);
