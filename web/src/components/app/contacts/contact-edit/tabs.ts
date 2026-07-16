// Tab definitions for the contact slide-over. Kept in a tiny module
// so the panel + each tab component can import the same enum without
// circular deps.
//
// Order matches the visible tab strip in the slide-over header.

import type { LucideIcon } from "lucide-react";
import {
    GaugeIcon,
    ActivityIcon,
    StickyNoteIcon,
    SlidersHorizontalIcon,
    SparklesIcon,
} from "lucide-react";

export type ContactSlideTab = "overview" | "activity" | "notes" | "details" | "research";

export const CONTACT_SLIDE_TABS: { id: ContactSlideTab; label: string; icon: LucideIcon }[] = [
    { id: "overview", label: "Overview", icon: GaugeIcon },
    { id: "activity", label: "Activity", icon: ActivityIcon },
    { id: "notes", label: "Notes", icon: StickyNoteIcon },
    { id: "research", label: "Research", icon: SparklesIcon },
    { id: "details", label: "Details", icon: SlidersHorizontalIcon },
];
