// Entry point: TanStack Router (code-based routes) + TanStack Query, mounted under the shell the portal
// service serves at /connect/<token> and /r/<token>.

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router";

import { ConnectPage } from "./ConnectPage";
import { startLook } from "./look";
import { NotFound } from "./NotFound";
import { ReportPage } from "./ReportPage";
import "./styles.css";

const rootRoute = createRootRoute({
    component: () => <Outlet />,
    notFoundComponent: NotFound,
});

// A client connects their mailboxes to the workspace that sent the link.
const connectRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/connect/$token",
    component: ConnectPage,
});

// A shared client report.
const reportRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/r/$token",
    component: ReportPage,
});

const router = createRouter({ routeTree: rootRoute.addChildren([connectRoute, reportRoute]) });

declare module "@tanstack/react-router" {
    interface Register {
        router: typeof router;
    }
}

const queryClient = new QueryClient();

startLook();

createRoot(document.getElementById("root")!).render(
    <StrictMode>
        <QueryClientProvider client={queryClient}>
            <RouterProvider router={router} />
        </QueryClientProvider>
    </StrictMode>,
);
