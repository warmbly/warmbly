// Class strings shared by the landing page scenes. The ones named after a
// dashboard primitive copy web/src/components/layout/Page.tsx.

/** A dashboard window hung from its top-left corner, running off the far edges. */
export const WINDOW =
  'absolute overflow-hidden bg-white rounded-tl-[12px] ring-1 ring-slate-900/[0.08] shadow-[0_40px_90px_-40px_rgba(15,23,42,0.6)]';
/** A whole card floating over the window or the photo. */
export const FLOAT =
  'float-layer absolute rounded-[10px] bg-white ring-1 ring-slate-900/[0.07] shadow-[0_24px_60px_-26px_rgba(15,23,42,0.5)]';

export const EYEBROW = 'text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium';
export const TOPBAR = 'h-12 px-5 border-b border-slate-200 flex items-center gap-3 bg-white';
export const SECTIONBAR = 'h-9 px-5 border-b border-slate-200/60 flex items-center gap-2.5';
export const ACTION = 'h-7 px-2.5 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium';
export const ACTION_PRIMARY = `${ACTION} bg-sky-600 text-white`;
export const ACTION_GHOST = `${ACTION} border border-slate-200 text-slate-700 bg-white`;
export const STAT = 'px-5 py-4 border-r border-slate-200';
export const STAT_VALUE = 'text-[26px] text-slate-900 font-light leading-none mt-2 tabular-nums';
export const STAT_SUB = 'text-[10px] text-slate-400 mt-1.5 font-mono truncate';
export const TAB = 'relative h-10 px-2.5 inline-flex items-center gap-1.5 text-[12.5px]';
