// Static Tailwind classes matching each dither tone (legend dots etc.);
// lives outside the component files so fast refresh stays intact.

import type { DitherTone } from "@/components/ui/dither";

export const TONE_DOT: Record<DitherTone, string> = {
    sky: "bg-sky-500",
    emerald: "bg-emerald-500",
    violet: "bg-violet-500",
    amber: "bg-amber-500",
    rose: "bg-rose-500",
    slate: "bg-slate-600",
};
