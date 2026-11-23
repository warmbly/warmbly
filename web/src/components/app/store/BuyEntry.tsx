// Where buying mailboxes starts: the first item of the Mailboxes page's Add menu, and the empty state's lead button.
import { Link, useNavigate } from "@tanstack/react-router";
import { ChevronDownIcon, LockIcon, PlugIcon, PlusIcon, ReceiptTextIcon, ShoppingBagIcon } from "lucide-react";
import { motion, useReducedMotion } from "framer-motion";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuSeparator, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { TopbarAction } from "@/components/layout/Page";
import { storeSalesPaused, useStoreState } from "@/lib/api/hooks/app/store/useStore";
import useWorkspaceWritable from "@/hooks/useWorkspaceWritable";
import { fmtMonthly } from "@/lib/buy/money";
import { HOVER_LIFT, PRESS_SPRING } from "@/lib/buy/motion";

/** "Add account": a menu leading with buying when the store is on or still to be set up, the plain connect button otherwise. */
export function AddAccountMenu({ onConnect }: { onConnect: () => void }) {
    const navigate = useNavigate();
    const store = useStoreState();
    const workspace = useWorkspaceWritable();
    // A managed workspace never sells (the store answers hidden); a paused one takes no new mailbox either.
    if (store.kind === "hidden" || !workspace.writable) {
        return (
            <TopbarAction writes onClick={onConnect} icon={<PlusIcon className="w-3 h-3" />}>
                Add account
            </TopbarAction>
        );
    }
    return (
        <PopoverMenu align="end">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className="h-7 pl-2.5 pr-2 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium transition-colors bg-sky-600 hover:bg-sky-700 text-white outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                >
                    <PlusIcon className="w-3 h-3" />
                    Add account
                    <ChevronDownIcon className="w-3 h-3 opacity-80" />
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={260}>
                {store.kind === "on" ? (
                    <>
                        {storeSalesPaused(store.status) ? (
                            <BuyPausedRow />
                        ) : (
                            <PopoverMenuItem
                                onSelect={() => void navigate({ to: "/app/emails/buy" })}
                                icon={<ShoppingBagIcon className="w-3.5 h-3.5 text-sky-600" />}
                                description="A new domain with Google or Microsoft mailboxes, set up for you"
                            >
                                Buy new mailboxes · {fmtMonthly(store.status.mailbox_monthly_micros)}
                            </PopoverMenuItem>
                        )}
                        <PopoverMenuItem
                            onSelect={() => void navigate({ to: "/app/emails/buy/orders" })}
                            icon={<ReceiptTextIcon className="w-3.5 h-3.5" />}
                            description="What you bought and how its setup is going"
                        >
                            Orders
                        </PopoverMenuItem>
                    </>
                ) : (
                    <PopoverMenuItem
                        onSelect={() => void navigate({ to: "/app/emails/buy" })}
                        icon={<LockIcon className="w-3.5 h-3.5 text-slate-400" />}
                        trailing={<span className="ml-auto h-4 px-1.5 rounded-full bg-slate-100 text-slate-600 text-[10px] font-medium inline-flex items-center shrink-0">Set up</span>}
                        description="Not set up on this instance yet"
                    >
                        Buy new mailboxes
                    </PopoverMenuItem>
                )}
                <PopoverMenuSeparator />
                <PopoverMenuItem onSelect={onConnect} icon={<PlugIcon className="w-3.5 h-3.5" />} description="Gmail, Microsoft 365, SMTP/IMAP or an import">
                    Connect a mailbox you have
                </PopoverMenuItem>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

/** The buy row while new purchases are paused: it stays in the menu, focusable for arrow keys, and opens nothing. */
function BuyPausedRow() {
    return (
        <div
            role="menuitem"
            aria-disabled="true"
            tabIndex={-1}
            className="w-full px-3 min-h-7 py-1.5 flex items-center gap-2 text-[12.5px] text-left cursor-default outline-none focus-visible:bg-slate-50"
        >
            <ShoppingBagIcon className="w-3.5 h-3.5 text-slate-300 shrink-0" />
            <span className="flex-1 min-w-0">
                <span className="block truncate text-slate-400">Buy new mailboxes</span>
                <span className="block truncate text-[11px] text-slate-500">Will be available again soon</span>
            </span>
            <span className="h-4 px-1.5 rounded-full bg-amber-50 text-amber-700 text-[10px] font-medium inline-flex items-center shrink-0">Paused</span>
        </div>
    );
}

/** The empty state's lead: buy mailboxes ready to warm, priced up front. Renders nothing when the store is off. */
export function BuyMailboxesButton({ size = "md" }: { size?: "sm" | "md" }) {
    const reduced = useReducedMotion();
    // Locked and paused render nothing too: an empty Mailboxes page stays connect-first until the store sells.
    const store = useStoreState();
    if (store.kind !== "on" || storeSalesPaused(store.status)) return null;
    const { status } = store;
    return (
        <motion.span whileHover={reduced ? undefined : HOVER_LIFT} whileTap={reduced ? undefined : { scale: 0.97 }} transition={PRESS_SPRING} className="inline-flex rounded-md">
            <Link
                to="/app/emails/buy"
                className={
                    size === "sm"
                        ? "h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                        : "h-8 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-medium inline-flex items-center gap-1.5 transition-colors"
                }
            >
                <ShoppingBagIcon className="w-3.5 h-3.5" />
                Buy new mailboxes · {fmtMonthly(status.mailbox_monthly_micros)}
            </Link>
        </motion.span>
    );
}
