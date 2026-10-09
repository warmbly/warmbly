// Class strings the use-case scenes share, copied from the dashboard tables.

/** A table header cell (web/src/components/layout/Page.tsx tables). */
export const TH = 'px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] text-left truncate';
/** The unticked row checkbox. */
export const CHECK = 'block size-3.5 rounded-[4px] border border-slate-300 bg-white';
/** A round initials avatar. */
export const AVATAR = 'inline-flex shrink-0 items-center justify-center rounded-full bg-slate-100 font-semibold text-slate-600';
/** The h-7 search box in a section bar. */
export const SEARCH = 'h-7 pl-2 pr-1 rounded-md border border-slate-200 bg-white flex items-center gap-1.5 text-[12.5px] text-slate-400';
/** The uppercase status label used in tables (Active, Warming, Replied). */
export const STATUS = 'inline-flex items-center gap-1.5 text-[10.5px] font-medium uppercase tracking-[0.08em]';
/** A popover menu panel (web/src/components/ui/popover-menu.tsx). */
export const MENU = 'rounded-md border border-slate-200 bg-white shadow-[0_4px_12px_-2px_rgba(15,23,42,0.08),0_2px_4px_rgba(15,23,42,0.04)] py-1';
export const MENU_LABEL = 'px-3 pt-2 pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium';
export const MENU_ITEM = 'h-7 px-3 flex items-center gap-2 text-[12.5px] text-slate-700';

/** Initials from a name: "Dana Reyes" -> "DR". */
export const initials = (name: string) => name.split(/\s+/).map((p) => p[0]).join('').slice(0, 2).toUpperCase();

/** A small coloured tag chip, as mailbox tags render on the Accounts page. */
export const tagStyle = (color: string) => `background-color:${color}1a;color:${color};`;
