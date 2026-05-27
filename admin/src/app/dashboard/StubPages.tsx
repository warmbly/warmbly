// Placeholder pages for nav entries we haven't wired backends for yet.
// They exist so the sidebar never 404s and so it's obvious to a dev
// which surface they should fill in next.

import { ComingSoon, PageHeader } from "@/components/layout/PageHeader";

interface StubProps {
    title: string;
    description: string;
    coming: string;
}

function Stub({ title, description, coming }: StubProps) {
    return (
        <div>
            <PageHeader title={title} description={description} />
            <ComingSoon label={coming} />
        </div>
    );
}

export function MailboxesPage() {
    return (
        <Stub
            title="Mailboxes"
            description="Every connected mailbox across all organizations. Health, send budget, warmup state, last sync."
            coming="Mailbox table backed by /admin/users/:id/emails coming in the next iteration."
        />
    );
}

export function UsersPage() {
    return (
        <Stub
            title="Users"
            description="Platform-wide user search. Ban, unban, view subscription, override rate limits."
            coming="Search + detail pages backed by /admin/users coming in the next iteration."
        />
    );
}

export function OrganizationsPage() {
    return (
        <Stub
            title="Organizations"
            description="Workspaces and team membership. Plan, seat usage, billing surface."
            coming="Endpoint pending — backend exposes per-user data today, org-level rollups are next."
        />
    );
}

export function PlansPage() {
    return (
        <Stub
            title="Plans & Billing"
            description="Catalog of pricing tiers and the active subscriptions on each."
            coming="CRUD over /admin/plans coming in the next iteration."
        />
    );
}

export function WarmupPage() {
    return (
        <Stub
            title="Warmup pools"
            description="Health of the warmup pools, blocked accounts, appeal queue. Per CLAUDE.md, free and premium pools stay isolated."
            coming="Pools + appeals view backed by /admin/warmup/* coming in the next iteration."
        />
    );
}

export function CampaignsPage() {
    return (
        <Stub
            title="Campaigns"
            description="Cross-organization campaign view. Inspect, pause, or stop any running campaign."
            coming="Backed by /admin/campaigns — coming in the next iteration."
        />
    );
}

export function AnalyticsPage() {
    return (
        <Stub
            title="Analytics"
            description="Time-series view of platform-wide send, bounce, complaint, and worker-load metrics."
            coming="Charts backed by /admin/analytics/* (overview already feeds the Overview page) — full chart pack coming in the next iteration."
        />
    );
}

export function NotFoundPage() {
    return (
        <div>
            <PageHeader title="Page not found" description="That route isn't part of the admin app." />
            <a href="/" className="text-sm underline text-muted-foreground">
                Back to overview
            </a>
        </div>
    );
}
