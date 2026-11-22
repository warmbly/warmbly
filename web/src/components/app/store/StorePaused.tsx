// The notice on a workspace's purchases while the instance has no GraphKit connection: they keep working, their upkeep waits.
import { ArrowUpRightIcon, PauseCircleIcon } from "lucide-react";
import type { StoreStatus } from "@/lib/api/models/app/store/Store";
import { cn } from "@/lib/utils";

export default function StorePaused({ status, compact = false, className }: { status: StoreStatus; compact?: boolean; className?: string }) {
    const link = status.can_configure ? status.configure_url : undefined;
    return (
        <div
            role="status"
            className={cn(
                "rounded-md border border-rose-200 bg-rose-50/70 flex items-start gap-2 text-rose-900 leading-relaxed",
                compact ? "px-3 py-2.5 text-[11.5px]" : "px-3.5 py-3 text-[12.5px]",
                className,
            )}
        >
            <PauseCircleIcon className={cn("text-rose-600 shrink-0", compact ? "w-3.5 h-3.5 mt-0.5" : "w-4 h-4 mt-0.5")} />
            <p className="min-w-0 flex-1">
                {status.can_configure
                    ? "Renewals, monthly billing and Google mailbox connections are paused until a GraphKit API key is added to this instance."
                    : "Renewals, monthly billing and Google mailbox connections are paused until your instance operator adds a GraphKit API key."}
                {link && (
                    <>
                        {" "}
                        <a href={link} target="_blank" rel="noreferrer" className="font-medium text-rose-900 underline underline-offset-2 inline-flex items-center gap-0.5">
                            Add the API key
                            <ArrowUpRightIcon className="w-3 h-3" />
                        </a>
                    </>
                )}
            </p>
        </div>
    );
}
