import { CircleCheckIcon, CircleHelpIcon, CircleXIcon, ShieldAlertIcon, SparklesIcon, TriangleAlertIcon } from "lucide-react";
import type { LeadEmail, LeadPerson, LeadStatus } from "@/lib/api/models/app/leads/Lead";

export const STATUS_LABEL: Record<LeadStatus, string> = {
    new: "New",
    saved: "Saved",
    contacted: "Contacted",
    dismissed: "Dismissed",
};

export function emailVerdict(e?: LeadEmail): { label: string; tone: string; icon: React.ReactNode } {
    switch (e?.status) {
        case "valid":
            return { label: "Valid", tone: "text-emerald-700", icon: <CircleCheckIcon className="size-3" /> };
        case "risky":
            return { label: "Risky", tone: "text-amber-700", icon: <TriangleAlertIcon className="size-3" /> };
        case "invalid":
            return { label: "Invalid", tone: "text-rose-700", icon: <CircleXIcon className="size-3" /> };
        case "suppressed":
            return { label: "Suppressed", tone: "text-slate-500", icon: <ShieldAlertIcon className="size-3" /> };
        default:
            return { label: "Unchecked", tone: "text-slate-500", icon: <CircleHelpIcon className="size-3" /> };
    }
}

// What we know about a person's address, in words a prospector acts on.
export function personVerdict(p: LeadPerson): { label: string; tone: string; icon: React.ReactNode; hint: string } {
    switch (p.email_status) {
        case "valid":
            return {
                label: "Confirmed",
                tone: "text-emerald-700",
                icon: <CircleCheckIcon className="size-3" />,
                hint: p.email_source === "pattern" ? "Worked out from the name and confirmed by their mail server" : "Their mail server confirmed this address",
            };
        case "risky":
            return { label: "Risky", tone: "text-amber-700", icon: <TriangleAlertIcon className="size-3" />, hint: "The server accepts it but may not deliver" };
        case "invalid":
            return { label: "Not deliverable", tone: "text-rose-700", icon: <CircleXIcon className="size-3" />, hint: "Their mail server refused this address" };
        case "suppressed":
            return { label: "Suppressed", tone: "text-slate-500", icon: <ShieldAlertIcon className="size-3" />, hint: "On your suppression list, so it is never mailed" };
        case "guessed":
            return {
                label: p.email_confidence >= 50 ? "Likely" : "Best guess",
                tone: p.email_confidence >= 50 ? "text-sky-700" : "text-amber-700",
                icon: <SparklesIcon className="size-3" />,
                hint:
                    p.email_confidence >= 50
                        ? "Follows the address shape this business uses; their server could not confirm it"
                        : "The most common address shape; nothing could confirm it",
            };
        case "unknown":
            return { label: "Unconfirmed", tone: "text-slate-500", icon: <CircleHelpIcon className="size-3" />, hint: "Their mail server would not say" };
    }
    return {
        label: p.email_source === "manual" ? "Typed in" : "Unchecked",
        tone: "text-slate-500",
        icon: <CircleHelpIcon className="size-3" />,
        hint: "Not checked yet",
    };
}
