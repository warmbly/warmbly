// Shared button styles and number formatting for the agency screens.

export const primaryBtn =
    "h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60";
export const ghostBtn =
    "h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors";

export function formatNumber(n: number): string {
    return n.toLocaleString();
}
