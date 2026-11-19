// The way from any purchased view to the order history and to the store's lines in the balance ledger.
import { Link } from "@tanstack/react-router";
import { ReceiptTextIcon, ShoppingBagIcon } from "lucide-react";
import { usePermission } from "@/hooks/usePermission";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import usePartnerManaged from "@/hooks/usePartnerManaged";
import { storeState, useStoreOrders, useStoreStatus } from "@/lib/api/hooks/app/store/useStore";
import { cn } from "@/lib/utils";

const LINK = "h-7 px-2 rounded-md inline-flex items-center gap-1.5 text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors shrink-0";

export function OrdersLink({ className }: { className?: string }) {
    return (
        <Link to="/app/emails/buy/orders" className={cn(LINK, className)}>
            <ShoppingBagIcon className="w-3.5 h-3.5 text-slate-400" />
            Orders
        </Link>
    );
}

/** Store charges in Billing > Balance. Only for someone who can open it, on an instance that charges for the store. */
export function StoreChargesLink({ className }: { className?: string }) {
    const canBill = usePermission("MANAGE_BILLING");
    const billing = useAuthConfig().config.billing_enabled;
    const metered = useStoreStatus(canBill).data?.metered;
    if (!canBill || !billing || metered === false) return null;
    return (
        <Link to="/app/settings/billing/{-$tab}" params={{ tab: "balance" }} search={{ activity: "store" }} hash="activity" className={cn(LINK, className)}>
            <ReceiptTextIcon className="w-3.5 h-3.5 text-slate-400" />
            Store charges
        </Link>
    );
}

/** The Mailboxes page's way to the order history: shown while the store sells, or once the workspace has bought anything. */
export function MailboxesOrdersLink() {
    const canView = usePermission("MANAGE_EMAILS");
    const managed = usePartnerManaged() !== null;
    const status = useStoreStatus(canView && !managed);
    const store = storeState(status.data, managed);
    // Without GraphKit the history shows only while the workspace still holds live purchases.
    const live = store.kind === "locked" && !!store.status.disconnected;
    // Off for another reason: asked only then, to find orders placed while the store was on.
    const offOther = store.kind === "hidden" && !managed && status.data !== undefined;
    const orders = useStoreOrders(canView && offOther);
    const has = store.kind === "on" || live || (offOther && (orders.data?.pages[0]?.data.length ?? 0) > 0);
    if (!canView || !has) return null;
    return (
        <Link
            to="/app/emails/buy/orders"
            className="h-7 px-2.5 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium transition-colors border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 bg-white"
        >
            <ShoppingBagIcon className="w-3 h-3" />
            <span className="sr-only sm:not-sr-only">Orders</span>
        </Link>
    );
}
