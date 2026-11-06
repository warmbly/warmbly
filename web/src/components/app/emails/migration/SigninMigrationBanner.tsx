// The mailboxes page's notice that some mailboxes still sign in with Google on
// their own, with optional ways to switch their connection method.
// "Later" hides it for a week, per workspace.
import React from "react";
import { AlertTriangleIcon, ArrowRightIcon } from "lucide-react";
import { useCurrentOrg } from "@/stores/useAppStore";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { cn } from "@/lib/utils";

const LATER_MS = 7 * 24 * 60 * 60 * 1000;

function laterKey(orgId: string) {
    return `warmbly:signin-migration-later:${orgId}`;
}

function readLater(orgId: string): number {
    try {
        return Number(localStorage.getItem(laterKey(orgId)) ?? 0) || 0;
    } catch {
        return 0;
    }
}

export default function SigninMigrationBanner({ total, onOpen }: { total: number; onOpen: () => void }) {
    const orgId = useCurrentOrg()?.id ?? "";
    const [hiddenUntil, setHiddenUntil] = React.useState(() => readLater(orgId));
    React.useEffect(() => setHiddenUntil(readLater(orgId)), [orgId]);

    if (total <= 0 || hiddenUntil > Date.now()) return null;

    const later = () => {
        const until = Date.now() + LATER_MS;
        try {
            localStorage.setItem(laterKey(orgId), String(until));
        } catch {
            // Private mode: hidden for this visit only.
        }
        setHiddenUntil(until);
    };

    return (
        <div className="rounded-md border border-amber-200 bg-amber-50/70 px-3 py-2 flex flex-wrap items-center gap-x-3 gap-y-1.5">
            <div className="flex items-center gap-2 min-w-0 flex-1">
                <AlertTriangleIcon className="w-3.5 h-3.5 text-amber-600 shrink-0" />
                <p className="text-[12px] text-amber-900 min-w-0">
                    {total === 1 ? "1 mailbox connects" : `${total.toLocaleString()} mailboxes connect`} with the older Google sign-in.
                    You can reconnect with the new Google sign-in once your Workspace admin trusts Warmbly, or switch to an app
                    password, without losing history.
                </p>
            </div>
            <div className="flex items-center gap-1 shrink-0 ml-auto">
                <button
                    type="button"
                    onClick={later}
                    className="h-7 px-2.5 rounded-md text-[12px] text-amber-800 hover:bg-amber-100 transition-colors"
                >
                    Later
                </button>
                <button
                    type="button"
                    onClick={onOpen}
                    className="h-7 px-2.5 rounded-md bg-amber-600 hover:bg-amber-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                >
                    Review mailboxes
                    <ArrowRightIcon className="w-3 h-3" />
                </button>
            </div>
        </div>
    );
}

/** A mailbox row's marker; opens the migration at that mailbox's domain. */
export function SigninRetiringChip({ onClick, className }: { onClick: () => void; className?: string }) {
    return (
        <button
            type="button"
            onClick={(e) => {
                e.stopPropagation();
                onClick();
            }}
            title="This mailbox uses the older Google sign-in. You can reconnect it with the new Google sign-in or switch to an app password."
            aria-label="Google sign-in: switch connection method"
            className={cn(
                "inline-flex items-center gap-1 h-[18px] pl-0.5 pr-0.5 md:pr-1.5 rounded-full border border-amber-200 bg-amber-50 text-amber-700 hover:bg-amber-100 transition-colors shrink-0",
                className,
            )}
        >
            <ProviderLogo id="google" size="xs" framed={false} title="" />
            <span className="hidden md:inline text-[10.5px] font-medium whitespace-nowrap">Google sign-in</span>
        </button>
    );
}
