// Tab definitions for the contact slide-over. Kept in a tiny module
// so the panel + each tab component can import the same enum without
// circular deps.
//
// Order matches the visible tab strip in the slide-over header.

export type ContactSlideTab = "overview" | "activity" | "notes" | "details";

export const CONTACT_SLIDE_TABS: { id: ContactSlideTab; label: string }[] = [
    { id: "overview", label: "Overview" },
    { id: "activity", label: "Activity" },
    { id: "notes", label: "Notes" },
    { id: "details", label: "Details" },
];
