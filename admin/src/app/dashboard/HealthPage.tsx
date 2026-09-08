// Setup and health: two views of "is this instance ok". Findings are the
// backend's own verdicts about configuration and state; services are live
// probes of what it runs on. ?tab= deep-links either one.

import { Link, useSearchParams } from "react-router-dom";
import { Activity, ListChecks } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { PageTabs } from "@/components/layout/PageTabs";
import { Button } from "@/components/ui/button";
import { useInstanceHealth } from "@/hooks/useInstanceHealth";
import { FindingsTab } from "./health/FindingsTab";
import { ServicesTab } from "./health/ServicesTab";

const TAB_IDS = ["findings", "services"] as const;
type TabId = (typeof TAB_IDS)[number];

function isTabId(v: string | null): v is TabId {
    return TAB_IDS.includes(v as TabId);
}

export default function HealthPage() {
    const [params, setParams] = useSearchParams();
    const raw = params.get("tab");
    const tab: TabId = isTabId(raw) ? raw : "findings";

    // The findings count comes from the shared cache the sidebar badge reads,
    // so the tab badge and the nav badge never disagree.
    const healthQ = useInstanceHealth();
    const findings = healthQ.data?.checks?.length ?? 0;

    function setTab(next: string) {
        setParams(
            (prev) => {
                const p = new URLSearchParams(prev);
                if (next === "findings") p.delete("tab");
                else p.set("tab", next);
                return p;
            },
            { replace: true },
        );
    }

    return (
        <div>
            <PageHeader
                title="Setup and health"
                description="What the backend thinks is wrong with this instance, and whether the services it depends on are answering."
            >
                <Button size="sm" variant="outline" asChild>
                    <Link to="/configuration">Configuration</Link>
                </Button>
            </PageHeader>

            <PageTabs
                tabs={[
                    {
                        id: "findings",
                        label: "Findings",
                        icon: ListChecks,
                        badge: findings > 0 ? findings : undefined,
                    },
                    { id: "services", label: "Services", icon: Activity },
                ]}
                value={tab}
                onChange={setTab}
            />

            {tab === "findings" ? <FindingsTab /> : <ServicesTab />}
        </div>
    );
}
