import type { ColorBy } from "./LeadMap";

// What each pin color means, per coloring mode; matches the expressions in LeadMap.
export const LEGEND: Record<ColorBy, { color: string; label: string }[]> = {
    reach: [
        { color: "#10b981", label: "Email" },
        { color: "#0ea5e9", label: "Website" },
        { color: "#f59e0b", label: "Phone only" },
        { color: "#94a3b8", label: "No contact" },
    ],
    status: [
        { color: "#0ea5e9", label: "New" },
        { color: "#8b5cf6", label: "Saved" },
        { color: "#10b981", label: "Contacted" },
        { color: "#94a3b8", label: "Dismissed" },
    ],
    score: [
        { color: "#cbd5e1", label: "0" },
        { color: "#7dd3fc", label: "35" },
        { color: "#0284c7", label: "70" },
        { color: "#0c4a6e", label: "100" },
    ],
    rating: [
        { color: "#059669", label: "4.5+" },
        { color: "#10b981", label: "4+" },
        { color: "#f59e0b", label: "3+" },
        { color: "#f43f5e", label: "Under 3" },
        { color: "#94a3b8", label: "No rating" },
    ],
};
