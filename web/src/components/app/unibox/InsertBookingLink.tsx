// InsertBookingLink drops the org's scheduling link (prefilled with the
// recipient's email) into the composer body. Renders only when a Calendly /
// Cal.com link is configured, so the action never shows as a dead button.
// Shared by the reply composer and the compose window.

import { CalendarPlusIcon } from "lucide-react";
import toast from "react-hot-toast";
import useIntegrationConnections from "@/lib/api/hooks/app/integrations/useIntegrationConnections";
import { bookingURL, prefilledBookingURL } from "@/lib/api/models/app/integrations/Integration";

function bareEmail(s: string): string {
    const m = s.match(/<([^>]+)>/);
    if (m) return m[1].trim();
    return s.trim();
}

export default function InsertBookingLink({
    email,
    onInsert,
}: {
    email?: string;
    onInsert: (text: string) => void;
}) {
    const { data } = useIntegrationConnections();
    const url = (data?.connections ?? [])
        .map((c) => bookingURL(c))
        .find((u): u is string => !!u);
    if (!url) return null;

    const cleanEmail = email ? bareEmail(email) : undefined;
    return (
        <button
            type="button"
            title="Insert your booking link, prefilled for this contact"
            onClick={() => {
                onInsert(prefilledBookingURL(url, cleanEmail));
                toast.success("Booking link added");
            }}
            className="h-7 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] inline-flex items-center gap-1 transition-colors"
        >
            <CalendarPlusIcon className="w-3 h-3" />
            Booking link
        </button>
    );
}
