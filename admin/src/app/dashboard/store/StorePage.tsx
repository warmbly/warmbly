// The domain and mailbox store, instance-wide: health, every order, domain
// and mailbox, and the economics against GraphKit's wholesale prices.
// The tab and its filters live in the query string so links deep-link.

import { useSearchParams } from "react-router-dom";
import { Globe, HeartPulse, Inbox, PiggyBank, Receipt } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { PageTabs } from "@/components/layout/PageTabs";
import { DomainsTab, MailboxesTab, OrdersTab } from "./StoreLists";
import { HealthTab } from "./HealthTab";
import { useStoreHealth } from "./useStoreHealth";
import { EconomicsTab } from "./EconomicsTab";
import type { StoreTab } from "./format";

const TABS: { id: StoreTab; label: string; icon: typeof Globe }[] = [
    { id: "health", label: "Health", icon: HeartPulse },
    { id: "orders", label: "Orders", icon: Receipt },
    { id: "domains", label: "Domains", icon: Globe },
    { id: "mailboxes", label: "Mailboxes", icon: Inbox },
    { id: "economics", label: "Economics", icon: PiggyBank },
];

function isTab(v: string | null): v is StoreTab {
    return TABS.some((t) => t.id === v);
}

export default function StorePage() {
    const [params, setParams] = useSearchParams();
    const raw = params.get("tab");
    const tab: StoreTab = isTab(raw) ? raw : "health";
    const health = useStoreHealth();
    const c = health.data?.counts;

    // A tab starts unfiltered: one list's filters mean nothing on another.
    const setTab = (next: string) => setParams({ tab: next }, { replace: true });

    const tabs = TABS.map((t) => ({
        ...t,
        badge: !c
            ? undefined
            : t.id === "orders"
              ? c.orders_stuck
              : t.id === "domains"
                ? c.domains_renewal_failed
                : t.id === "mailboxes"
                  ? c.connects_failed + c.mailboxes_billing_failed
                  : undefined,
    }));

    return (
        <div>
            <PageHeader title="Store" description="Domains and mailboxes workspaces buy, fulfilled by GraphKit. Refresh, retry, refund and renew from the drawers." />
            <PageTabs tabs={tabs} value={tab} onChange={setTab} />
            {tab === "health" && <HealthTab />}
            {tab === "orders" && <OrdersTab />}
            {tab === "domains" && <DomainsTab />}
            {tab === "mailboxes" && <MailboxesTab />}
            {tab === "economics" && <EconomicsTab />}
        </div>
    );
}
