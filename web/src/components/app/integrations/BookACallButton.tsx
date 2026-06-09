// BookACallButton — a contextual "Book a call" affordance. It only renders when
// the org has a connected scheduling integration (Calendly / Cal.com) with a
// saved booking link, and opens that link prefilled with the contact's email +
// name. With several scheduling links it offers a picker. This is the contextual
// counterpart to the Integrations settings: configure the link once, book from
// anywhere a contact is in view (Unibox threads, contact detail).

"use client";

import { CalendarPlusIcon } from "lucide-react";

import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import useIntegrationConnections from "@/lib/api/hooks/app/integrations/useIntegrationConnections";
import {
    bookingURL,
    prefilledBookingURL,
    PROVIDER_LABELS,
} from "@/lib/api/models/app/integrations/Integration";
import { cn } from "@/lib/utils";

const TRIGGER_CLASS =
    "h-7 px-2 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors text-[12px]";

export default function BookACallButton({
    email,
    name,
    contactId,
    className,
}: {
    email?: string;
    name?: string;
    /** When set, embedded in the link so the booking webhook attributes the
     *  meeting to this exact contact (even if they book with another email). */
    contactId?: string;
    className?: string;
}) {
    const { data } = useIntegrationConnections();
    const targets = (data?.connections ?? [])
        .map((conn) => ({ conn, url: bookingURL(conn) }))
        .filter((t): t is { conn: (typeof t)["conn"]; url: string } => !!t.url);

    if (targets.length === 0) return null;

    const open = (url: string) =>
        window.open(prefilledBookingURL(url, email, name, contactId), "_blank", "noopener,noreferrer");

    // One link → open directly; several → let the user pick which calendar.
    if (targets.length === 1) {
        return (
            <button
                type="button"
                onClick={() => open(targets[0].url)}
                className={cn(TRIGGER_CLASS, className)}
                title="Open your scheduling page (prefilled) so the prospect can pick a time"
            >
                <CalendarPlusIcon className="w-3.5 h-3.5" />
                Booking link
            </button>
        );
    }

    return (
        <PopoverMenu align="end" side="bottom">
            <PopoverMenuTrigger asChild>
                <button type="button" className={cn(TRIGGER_CLASS, className)}>
                    <CalendarPlusIcon className="w-3.5 h-3.5" />
                    Booking link
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent>
                <PopoverMenuLabel>Send a scheduling link via</PopoverMenuLabel>
                {targets.map((t) => (
                    <PopoverMenuItem key={t.conn.id} onSelect={() => open(t.url)}>
                        {PROVIDER_LABELS[t.conn.provider]}
                        {t.conn.label && t.conn.label.toLowerCase() !== t.conn.provider
                            ? ` · ${t.conn.label}`
                            : ""}
                    </PopoverMenuItem>
                ))}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
