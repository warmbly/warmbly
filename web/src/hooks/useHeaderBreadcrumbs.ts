import { useMatches, useRouter } from "@tanstack/react-router";

const labelMap: Record<string, string> = {
    emails: "Accounts",
    domains: "Sending domains",
    unibox: "Inbox",
    contacts: "Contacts",
    segments: "Segments",
    labels: "Labels",
    campaigns: "Campaigns",
    analytics: "Analytics",
    crm: "CRM",
    pipelines: "Pipelines",
    deals: "Deals",
    tasks: "Tasks",
    templates: "Templates",
    "api-keys": "API Keys",
    settings: "Settings",
    billing: "Billing",
    team: "Team",
    admin: "Admin",
    workers: "Workers",
    credentials: "Credentials",
    audit: "Audit",
    slack: "Slack",
    link: "Link account",
    leads: "Leads",
    preferences: "Preferences",
    schedule: "Schedule",
    steps: "Steps",
    placement: "Placement tests",
    "ai-skills": "AI skills",
    "warmbly-cloud": "Warmbly Cloud",
    "oauth-apps": "OAuth apps",
    "inbox-tagging": "Inbox tagging",
    hubspot: "HubSpot",
    pipedrive: "Pipedrive",
    salesforce: "Salesforce",
};

const crumbTargets: Record<string, string> = {
    "/app/placement/batches": "/app/placement?tab=batches",
};

export function useHeaderBreadcrumbs() {
    const router = useRouter();
    const matches = useMatches();
    const match = matches[matches.length - 1];
    if (!match) return [];

    // Only static route segments are navigation; params describe the selected record or scope.
    const segments = match.fullPath.split("/").filter(Boolean);
    return segments.flatMap((segment, index) => {
        if (index === 0 || segment.includes("$")) return [];
        const template = `/${segments.slice(0, index + 1).join("/")}`;
        const to = router.buildLocation({ to: template, params: match.params }).pathname;
        const words = segment.replaceAll("-", " ");
        return [{
            label: labelMap[segment] ?? words.charAt(0).toUpperCase() + words.slice(1),
            to: crumbTargets[to] ?? to,
            current: to.replace(/\/$/, "") === match.pathname.replace(/\/$/, ""),
        }];
    });
}
