// Static Tailwind classes matching each dither tone (legend dots etc.);
// lives outside the component files so fast refresh stays intact.

import type { DitherTone } from "@/components/ui/dither";

export const TONE_DOT: Record<DitherTone, string> = {
    sky: "bg-sky-600",
    emerald: "bg-emerald-600",
    violet: "bg-violet-600",
    amber: "bg-amber-600",
    rose: "bg-rose-600",
    slate: "bg-slate-900",
};
