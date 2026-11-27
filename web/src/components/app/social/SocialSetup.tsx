// "Connect a WhatsApp account": shown wherever a social account is missing. It opens Settings > Social accounts
// with the connect dialog already on that channel.
import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { ArrowRightIcon, PlusIcon } from "lucide-react";
import { usePermission } from "@/hooks/usePermission";
import type { SocialChannel } from "@/lib/api/models/app/social/Social";
import { channelLabel } from "@/lib/social/channels";
import { cn } from "@/lib/utils";
import ChannelMark from "./ChannelMark";

/** An inline link. Without a channel it opens the dialog at the channel step. */
export function SocialSetupLink({ channel, className, children, onClick }: { channel?: SocialChannel | null; className?: string; children?: ReactNode; onClick?: () => void }) {
    const canManage = usePermission("MANAGE_EMAILS");
    const text = children ?? (channel ? `Connect a ${channelLabel(channel)} account` : "Connect a social account");
    if (!canManage) return <span className={cn("text-slate-500", className)}>{channel ? `A teammate who manages accounts can connect a ${channelLabel(channel)} account.` : "A teammate who manages accounts can connect one."}</span>;
    return (
        <Link
            to="/app/settings/social"
            search={channel ? { connect: channel } : { connect: "any" }}
            onClick={(e) => {
                e.stopPropagation();
                onClick?.();
            }}
            className={cn("inline-flex items-center gap-1 font-medium text-sky-700 hover:text-sky-800 hover:underline underline-offset-2", className)}
        >
            {text}
        </Link>
    );
}

/** A dashed card saying what is missing, with the setup link as its button. */
export function SocialSetupCard({
    channel,
    title,
    description,
    action,
    className,
}: {
    channel?: SocialChannel | null;
    title?: string;
    description?: ReactNode;
    /** A second action beside the setup link, such as adding an existing account to a campaign. */
    action?: ReactNode;
    className?: string;
}) {
    const canManage = usePermission("MANAGE_EMAILS");
    const name = channel ? channelLabel(channel) : "social";
    return (
        <div className={cn("flex flex-wrap items-center gap-3 rounded-md border border-dashed border-slate-200 bg-white px-3.5 py-3", className)}>
            {channel ? (
                <ChannelMark channel={channel} className="size-5" />
            ) : (
                <span className="inline-flex size-5 items-center justify-center rounded bg-slate-100 text-slate-500">
                    <PlusIcon className="size-3" />
                </span>
            )}
            <div className="min-w-0 flex-1 basis-48">
                <p className="text-[12.5px] font-medium text-slate-900">{title ?? (channel ? `No ${name} account yet` : "No social account yet")}</p>
                {description && <p className="mt-0.5 text-[11.5px] leading-snug text-slate-500">{description}</p>}
            </div>
            <div className="flex shrink-0 flex-wrap items-center gap-1.5">
                {action}
                {canManage ? (
                    <Link
                        to="/app/settings/social"
                        search={channel ? { connect: channel } : { connect: "any" }}
                        onClick={(e) => e.stopPropagation()}
                        className="inline-flex h-7 items-center gap-1.5 rounded-md border border-slate-200 bg-white px-2.5 text-[12px] font-medium text-slate-700 transition-colors hover:bg-slate-50"
                    >
                        {channel ? `Connect a ${name} account` : "Connect an account"}
                        <ArrowRightIcon className="size-3" />
                    </Link>
                ) : (
                    <span className="text-[11.5px] text-slate-500">Ask a teammate who manages accounts to connect one.</span>
                )}
            </div>
        </div>
    );
}
