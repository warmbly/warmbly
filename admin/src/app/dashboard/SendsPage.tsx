// The send outcome loop as the operator sees it. Four tabs, deep-linked
// through ?tab=: reservations still waiting on a worker result, tasks that
// exhausted their retries, recent task failures, and customer webhook health.

import { useSearchParams } from "react-router-dom";
import { AlertTriangle, Archive, Send, Webhook } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { PageTabs } from "@/components/layout/PageTabs";
import { InFlightTab } from "@/app/dashboard/sends/InFlightTab";
import { DeadLettersTab } from "@/app/dashboard/sends/DeadLettersTab";
import { FailuresTab } from "@/app/dashboard/sends/FailuresTab";
import { WebhooksTab } from "@/app/dashboard/sends/WebhooksTab";

const TABS = [
    { id: "in-flight", label: "In flight", icon: Send },
    { id: "dead-letters", label: "Dead letters", icon: Archive },
    { id: "failures", label: "Failures", icon: AlertTriangle },
    { id: "webhooks", label: "Webhooks", icon: Webhook },
];
const DEFAULT_TAB = "in-flight";

export default function SendsPage() {
    const [params, setParams] = useSearchParams();
    const raw = params.get("tab");
    const tab = TABS.some((t) => t.id === raw) ? (raw as string) : DEFAULT_TAB;

    function setTab(id: string) {
        const next = new URLSearchParams(params);
        if (id === DEFAULT_TAB) next.delete("tab");
        else next.set("tab", id);
        setParams(next, { replace: true });
    }

    return (
        <div>
            <PageHeader
                title="Sends"
                description="Every campaign send between its dispatch and the worker's answer, the tasks that gave up, what tasks reported when they failed, and whether customer webhooks are getting through."
            />
            <div className="mb-5">
                <PageTabs tabs={TABS} value={tab} onChange={setTab} />
            </div>
            {tab === "in-flight" && <InFlightTab />}
            {tab === "dead-letters" && <DeadLettersTab />}
            {tab === "failures" && <FailuresTab />}
            {tab === "webhooks" && <WebhooksTab />}
        </div>
    );
}
