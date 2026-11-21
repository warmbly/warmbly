// Tab definitions for the contact slide-over. Kept in a tiny module
// so the panel + each tab component can import the same enum without
// circular deps.
//
// Order matches the visible tab strip in the slide-over header.

import type { ComponentType } from "react";
import { RiLinkedinBoxFill } from "@remixicon/react";
import {
    GaugeIcon,
    ActivityIcon,
    StickyNoteIcon,
    SlidersHorizontalIcon,
    SparklesIcon,
    CircleDollarSignIcon,
} from "lucide-react";

export type ContactSlideTab = "overview" | "activity" | "linkedin" | "deals" | "notes" | "details" | "research";

// linkedin shows only for a contact with a LinkedIn profile.
export const CONTACT_SLIDE_TABS: { id: ContactSlideTab; label: string; icon: ComponentType<{ className?: string }> }[] = [
    { id: "overview", label: "Overview", icon: GaugeIcon },
    { id: "activity", label: "Activity", icon: ActivityIcon },
    { id: "linkedin", label: "LinkedIn", icon: RiLinkedinBoxFill },
    { id: "deals", label: "Deals", icon: CircleDollarSignIcon },
    { id: "notes", label: "Notes", icon: StickyNoteIcon },
    { id: "research", label: "Research", icon: SparklesIcon },
    { id: "details", label: "Details", icon: SlidersHorizontalIcon },
];
