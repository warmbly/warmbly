// The five channels' own marks, drawn inline in their brand colours. Size comes from className (default 1em).
import { useId } from "react";
import type { SocialChannel } from "@/lib/api/models/app/social/Social";
import { channelLabel, isChannel } from "@/lib/social/channels";
import { cn } from "@/lib/utils";

export default function ChannelMark({ channel, className, title }: { channel: SocialChannel | string; className?: string; title?: string }) {
    const gradient = useId();
    const label = title ?? channelLabel(channel);
    const common = { viewBox: "0 0 24 24", className: cn("size-[1em] shrink-0", className), role: "img" as const, "aria-label": label };
    if (!isChannel(channel)) {
        return (
            <svg {...common}>
                <circle cx="12" cy="12" r="11" fill="#94a3b8" />
            </svg>
        );
    }
    switch (channel) {
        case "linkedin":
            return (
                <svg {...common}>
                    <rect width="24" height="24" rx="4" fill="#0A66C2" />
                    <path
                        fill="#fff"
                        d="M7.1 9.4H4.5V19h2.6V9.4ZM5.8 5a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3Zm13.7 8.6c0-2.7-.6-4.4-3.6-4.4-1.5 0-2.4.8-2.8 1.5h-.1V9.4h-2.5V19h2.6v-4.8c0-1.3.3-2.5 1.8-2.5s1.6 1.4 1.6 2.6V19h2.6l.4-5.4Z"
                    />
                </svg>
            );
        case "instagram":
            return (
                <svg {...common}>
                    <defs>
                        <radialGradient id={gradient} cx="0.3" cy="1.07" r="1.15">
                            <stop offset="0" stopColor="#FFDD55" />
                            <stop offset="0.1" stopColor="#FFDD55" />
                            <stop offset="0.5" stopColor="#FF543E" />
                            <stop offset="1" stopColor="#C837AB" />
                        </radialGradient>
                    </defs>
                    <rect width="24" height="24" rx="6" fill={`url(#${gradient})`} />
                    <rect x="5" y="5" width="14" height="14" rx="4" fill="none" stroke="#fff" strokeWidth="1.8" />
                    <circle cx="12" cy="12" r="3.3" fill="none" stroke="#fff" strokeWidth="1.8" />
                    <circle cx="16.3" cy="7.7" r="1" fill="#fff" />
                </svg>
            );
        case "whatsapp":
            return (
                <svg {...common}>
                    <path fill="#25D366" d="M12 1.5A10.5 10.5 0 0 0 2.9 17.2L1.5 22.5l5.4-1.4A10.5 10.5 0 1 0 12 1.5Z" />
                    <path
                        fill="#fff"
                        d="M8.6 6.9c.2-.4.4-.4.6-.4h.5c.2 0 .4 0 .6.4l.8 1.9c.1.2.1.4 0 .6l-.4.6c-.1.2-.2.3 0 .6.5.9 1.3 1.7 2.2 2.2.3.2.5.1.6 0l.6-.7c.2-.2.3-.2.6-.1l1.8.8c.3.1.4.2.4.5 0 .6-.2 1.3-.7 1.7-.6.5-1.4.7-2.2.5-1.6-.4-3-1.3-4.2-2.4-1.1-1.1-2-2.5-2.4-4-.2-.8 0-1.6.6-2.2Z"
                    />
                </svg>
            );
        case "telegram":
            return (
                <svg {...common}>
                    <circle cx="12" cy="12" r="12" fill="#229ED9" />
                    <path
                        fill="#fff"
                        d="M5.4 11.7 16.9 7.3c.5-.2 1 .1.8.9l-1.9 9.2c-.1.6-.5.8-1 .5l-2.9-2.1-1.4 1.4c-.2.2-.3.3-.6.3l.2-3 5.5-5c.2-.2 0-.3-.3-.1l-6.8 4.3-2.9-.9c-.6-.2-.6-.6.1-.9Z"
                    />
                </svg>
            );
        case "facebook":
            return (
                <svg {...common}>
                    <circle cx="12" cy="12" r="12" fill="#0866FF" />
                    <path fill="#fff" d="M13.4 24v-8.6h2.9l.4-3.4h-3.3V9.9c0-1 .3-1.6 1.7-1.6h1.8v-3a24 24 0 0 0-2.6-.1c-2.6 0-4.3 1.6-4.3 4.4V12H7v3.4h3V24h3.4Z" />
                </svg>
            );
    }
}
