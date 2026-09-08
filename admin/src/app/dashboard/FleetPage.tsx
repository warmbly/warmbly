// Fleet: placement as the operator sees it. Capacity (per-worker load vs.
// effective capacity with the last hour's outcome counters), the decision log
// the control loops write, and the dedicated worker bindings. The tab lives
// in ?tab= so links deep-link.

import { useSearchParams } from "react-router-dom";
import { Gauge, ListChecks, Lock } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { PageTabs } from "@/components/layout/PageTabs";
import { CapacityTab } from "./fleet/CapacityTab";
import { DecisionsTab } from "./fleet/DecisionsTab";
import { DedicatedTab } from "./fleet/DedicatedTab";

const TABS = [
    { id: "capacity", label: "Capacity", icon: Gauge },
    { id: "decisions", label: "Decisions", icon: ListChecks },
    { id: "dedicated", label: "Dedicated", icon: Lock },
] as const;

type TabId = (typeof TABS)[number]["id"];

function isTab(v: string | null): v is TabId {
    return TABS.some((t) => t.id === v);
}

export default function FleetPage() {
    const [params, setParams] = useSearchParams();
    const raw = params.get("tab");
    const tab: TabId = isTab(raw) ? raw : "capacity";

    function setTab(id: string) {
        setParams(
            (p) => {
                p.set("tab", id);
                return p;
            },
            { replace: true },
        );
    }

    return (
        <div>
            <PageHeader
                title="Fleet"
                description="How mailboxes are spread across workers: load against capacity, what the placement loops decided, and which workers are reserved for one workspace."
            />
            <PageTabs tabs={[...TABS]} value={tab} onChange={setTab} />
            {tab === "capacity" && <CapacityTab />}
            {tab === "decisions" && <DecisionsTab />}
            {tab === "dedicated" && <DedicatedTab />}
        </div>
    );
}
