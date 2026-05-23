import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './global.css'
import { createBrowserRouter, Outlet, RouterProvider } from 'react-router-dom';
import RootLayout from './app/layout';

import "@fontsource/inter/400.css";
import "@fontsource/inter/600.css";
import "@fontsource/poppins/400.css";
import "@fontsource/poppins/600.css";
import "@fontsource/poppins/700.css";
import RootAppLayout from './app/app/layout';
import AddressesPage from './app/app/emails/page';
import ContactsPage from './app/app/contacts/page';
import CampaignsPage from './app/app/campaigns/page';
import CampaignLayout from './app/app/campaigns/[id]/layout';
import CampaignPreview from './app/app/campaigns/[id]/page';
import CampaignLeads from './app/app/campaigns/[id]/leads/page';
import CampaignPreferences from './app/app/campaigns/[id]/preferences/page';
import CampaignSchedule from './app/app/campaigns/[id]/schedule/page';
import CampaignSequences from './app/app/campaigns/[id]/sequences/page';
import AnalyticsPage from './app/app/analytics/page';
import PipelinesPage from './app/app/crm/pipelines/page';
import DealsPage from './app/app/crm/deals/page';
import TasksPage from './app/app/crm/tasks/page';
import TemplatesPage from './app/app/templates/page';
import APIKeysPage from './app/app/api-keys/page';
import SettingsPage from './app/app/settings/page';
import BillingPage from './app/app/billing/page';
import TeamPage from './app/app/team/page';
import UniboxPage from './app/app/unibox/page';

import { Toaster } from '@/components/ui/toaster';

import * as Sentry from "@sentry/react";

Sentry.init({
  dsn: "https://412466daced4b1d85ee040eef66efc95@o4510248538472448.ingest.us.sentry.io/4510248563113984",
  sendDefaultPii: true,
  environment: import.meta.env.MODE
})

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ReactQueryDevtools } from "@tanstack/react-query-devtools"
import Home from './app/page';
import AuthLayout from './app/auth/layout';
import RegisterLayout from './app/auth/register/layout';
import RegisterPage from './app/auth/register/page';
import RegisterConfirmPage from './app/auth/register/confirm/page';
import LoginLayout from './app/auth/login/layout';
import LoginPage from './app/auth/login/page';
import LoginConfirmPage from './app/auth/login/confirm/page';
import ResetPasswordLayout from './app/auth/reset-password/layout';
import ResetPasswordPage from './app/auth/reset-password/page';
import ResetPasswordConfirmPage from './app/auth/reset-password/confirm/page';
import OnboardingLayout from './app/onboarding/layout';
import OnboardingPage from './app/onboarding/page';
import AdminLayout from './app/app/admin/layout';
import AdminPage from './app/app/admin/page';
import AdminWorkersPage from './app/app/admin/workers/page';
import AdminAddWorkerPage from './app/app/admin/workers/new/page';
import AdminWorkerDetailPage from './app/app/admin/workers/[id]/page';
import AdminCredentialsPage from './app/app/admin/credentials/page';
import AdminAuditPage from './app/app/admin/audit/page';

const queryClient = new QueryClient();

const router = createBrowserRouter([
  {
    path: "/",
    element: <RootLayout />,
    children: [
      {
        index: true,
        element: <Home />
      },
      {
        path: "auth",
        element: <AuthLayout />,
        children: [
          {
            path: "register",
            element: <LoginLayout />,
            children: [
              {
                index: true,
                element: <LoginPage />,
              },
              {
                path: "confirm",
                element: <RegisterConfirmPage />,
              }
            ]
          },
          {
            path: "login",
            element: <LoginLayout />,
            children: [
              {
                index: true,
                element: <LoginPage />
              },
              {
                path: "confirm",
                element: <LoginConfirmPage />,
              }
            ]
          },
          {
            path: "reset-password",
            element: <ResetPasswordLayout />,
            children: [
              {
                index: true,
                element: <ResetPasswordPage />
              },
              {
                path: "confirm",
                element: <ResetPasswordConfirmPage />,
              }
            ]
          }
        ]
      },
      {
        path: "onboarding",
        element: <OnboardingLayout />,
        children: [
          {
            index: true,
            element: <OnboardingPage />,
          }
        ]
      },
      {
        path: "app",
        element: <RootAppLayout />,
        children: [
          {
            path: "emails",
            element: <AddressesPage />,
          },
          {
            path: "contacts",
            element: <ContactsPage />,
          },
          {
            path: "campaigns",
            children: [
              {
                index: true,
                element: <CampaignsPage />,
              },
              {
                path: ":id",
                element: <CampaignLayout />,
                children: [
                  {
                    index: true,
                    element: <CampaignPreview />,
                  },
                  {
                    path: "leads",
                    element: <CampaignLeads />,
                  },
                  {
                    path: "preferences",
                    element: <CampaignPreferences />,
                  },
                  {
                    path: "schedule",
                    element: <CampaignSchedule />,
                  },
                  {
                    path: "sequences",
                    element: <CampaignSequences />,
                  }
                ]
              }
            ]
          },
          {
            path: "analytics",
            element: <AnalyticsPage />,
          },
          {
            path: "crm",
            children: [
              {
                path: "pipelines",
                element: <PipelinesPage />,
              },
              {
                path: "deals",
                element: <DealsPage />,
              },
              {
                path: "tasks",
                element: <TasksPage />,
              }
            ]
          },
          {
            path: "templates",
            element: <TemplatesPage />,
          },
          {
            path: "api-keys",
            element: <APIKeysPage />,
          },
          {
            path: "settings",
            element: <SettingsPage />,
          },
          {
            path: "billing",
            element: <BillingPage />,
          },
          {
            path: "unibox",
            element: <UniboxPage />,
          },
          {
            path: "team",
            element: <TeamPage />,
          },
          {
            path: "admin",
            element: <AdminLayoutWithOutlet />,
            children: [
              { index: true, element: <AdminPage /> },
              { path: "workers", element: <AdminWorkersPage /> },
              { path: "workers/new", element: <AdminAddWorkerPage /> },
              { path: "workers/:id", element: <AdminWorkerDetailPage /> },
              { path: "credentials", element: <AdminCredentialsPage /> },
              { path: "audit", element: <AdminAuditPage /> },
            ],
          },
        ]
      }
    ],
  },
]);

// AdminLayout takes a children prop rather than rendering <Outlet/>; bridge it.
function AdminLayoutWithOutlet() {
  return <AdminLayout><Outlet /></AdminLayout>;
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
      <Toaster />
      {import.meta.env.DEV && <ReactQueryDevtools initialIsOpen={false} />}
    </QueryClientProvider>
  </StrictMode>,
)
