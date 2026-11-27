// Small icon links to a contact's profiles, one per channel they have a handle on.
import type { ContactSocial, SocialChannel } from "@/lib/api/models/app/social/Social";
import { CHANNELS, channelLabel, profileUrl } from "@/lib/social/channels";
import { cn } from "@/lib/utils";
import ChannelMark from "./ChannelMark";

export function SocialProfileLink({ channel, handle, label, className }: { channel: SocialChannel; handle: string | null | undefined; label: string; className?: string }) {
    const href = profileUrl(channel, handle);
    if (!href) return null;
    return (
        <a
            href={href}
            target="_blank"
            rel="noopener noreferrer nofollow"
            aria-label={`${label} on ${channelLabel(channel)}`}
            title={`Open on ${channelLabel(channel)}`}
            onClick={(e) => e.stopPropagation()}
            onMouseDown={(e) => e.stopPropagation()}
            className={cn("inline-flex items-center justify-center rounded hover:opacity-80", className)}
        >
            <ChannelMark channel={channel} className="size-3.5" />
        </a>
    );
}

/** Every profile a contact has a link for, or nothing. */
export function SocialLinks({ social, label, className }: { social: ContactSocial | null | undefined; label: string; className?: string }) {
    const linked = CHANNELS.filter((c) => profileUrl(c, social?.[c]?.handle));
    if (linked.length === 0) return null;
    return (
        <span className={cn("inline-flex items-center gap-1", className)}>
            {linked.map((c) => (
                <SocialProfileLink key={c} channel={c} handle={social?.[c]?.handle} label={label} />
            ))}
        </span>
    );
}
