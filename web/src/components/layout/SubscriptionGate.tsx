// A hosted workspace without a subscription can manage mailboxes, its
// Warmbly Cloud links and settings, and link a Slack account (whose workspace
// need not be the selected one); every other page shows the full-screen
// plan chooser until a plan is active.

import React from "react";
import { useLocation } from "@tanstack/react-router";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import SubscriptionLockedScreen from "./SubscriptionLockedScreen";

const OPEN_PREFIXES = ["/app/emails", "/app/settings", "/app/select-org", "/app/slack"];

const FEATURE_BY_PREFIX: [string, string][] = [
    ["/app/unibox", "The unified inbox"],
    ["/app/campaigns", "Campaigns"],
    ["/app/contacts", "Contacts"],
    ["/app/analytics", "Analytics"],
    ["/app/deliverability", "Deliverability"],
    ["/app/crm", "The CRM"],
    ["/app/templates", "Templates"],
    ["/app/integrations", "Integrations"],
    ["/app/automations", "Automations"],
    ["/app/api-keys", "API keys"],
    ["/app/audit", "The audit log"],
];

export default function SubscriptionGate({ children }: { children: React.ReactNode }) {
    const access = useFeatureAccess();
    const pathname = useLocation({ select: (l) => l.pathname });
    const open = OPEN_PREFIXES.some((p) => pathname === p || pathname.startsWith(p + "/"));
    if (!access.locked || open) return <>{children}</>;
    const feature = FEATURE_BY_PREFIX.find(([p]) => pathname === p || pathname.startsWith(p + "/"))?.[1] ?? "This page";
    return <SubscriptionLockedScreen feature={feature} />;
}
