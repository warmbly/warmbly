import type { ReactNode } from 'react';
import { cn } from '@/lib/cn';

// The shared frame every Learn diagram sits in: a hairline panel and an optional caption.
export function Frame({ children, caption, className }: { children: ReactNode; caption?: string; className?: string }) {
  return (
    <figure className="not-prose my-8">
      <div className={cn('overflow-x-auto rounded-[10px] border border-fd-border bg-fd-card p-5 md:p-6', className)}>{children}</div>
      {caption && <figcaption className="mt-2.5 text-[13px] text-fd-muted-foreground">{caption}</figcaption>}
    </figure>
  );
}

export const tone = {
  good: 'bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-500/10 dark:text-emerald-300 dark:ring-emerald-500/30',
  watch: 'bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-500/10 dark:text-amber-300 dark:ring-amber-500/30',
  bad: 'bg-rose-50 text-rose-700 ring-rose-200 dark:bg-rose-500/10 dark:text-rose-300 dark:ring-rose-500/30',
  info: 'bg-sky-50 text-sky-700 ring-sky-200 dark:bg-sky-500/10 dark:text-sky-300 dark:ring-sky-500/30',
  plain: 'bg-fd-muted text-fd-muted-foreground ring-fd-border',
} as const;
export type Tone = keyof typeof tone;

export const fill = {
  good: 'bg-emerald-500',
  watch: 'bg-amber-400',
  bad: 'bg-rose-500',
  info: 'bg-sky-500',
  plain: 'bg-fd-muted-foreground/30',
} as const;

export function Pill({ children, t = 'plain', className }: { children: ReactNode; t?: Tone; className?: string }) {
  return <span className={cn('inline-flex items-center rounded-full px-2 py-0.5 text-[11.5px] font-medium ring-1 ring-inset', tone[t], className)}>{children}</span>;
}

export function Label({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn('text-[11px] font-medium uppercase tracking-[0.12em] text-fd-muted-foreground', className)}>{children}</div>;
}

/** A right-pointing connector between boxes in a row; turns downward on narrow screens. */
export function Arrow() {
  return (
    <div aria-hidden="true" className="flex shrink-0 items-center justify-center text-fd-muted-foreground/60 max-md:py-1">
      <svg width="28" height="14" viewBox="0 0 28 14" fill="none" className="max-md:rotate-90">
        <path d="M0 7h25m0 0-5-5m5 5-5 5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    </div>
  );
}

/** The short summary that opens a Learn page. */
export function ShortVersion({ children }: { children: ReactNode }) {
  return (
    <div className="wd-short not-prose my-6 rounded-[10px] border border-fd-border bg-fd-muted/50 px-5 py-4">
      <Label>The short version</Label>
      <div className="wd-short-body mt-2 text-[15px] leading-[1.65] text-fd-foreground">{children}</div>
    </div>
  );
}
