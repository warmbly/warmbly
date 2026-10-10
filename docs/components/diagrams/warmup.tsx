import { Frame, Label, Pill, fill, type Tone } from './frame';
import { cn } from '@/lib/cn';

// Email warmup, warmup pools and reputation recovery.

/** Warmbly's default ramp (10 a day, +1 a day, ceiling 40) against a sudden jump. */
export function WarmupRamp() {
  const W = 560, H = 220, pad = { l: 34, r: 12, t: 14, b: 26 };
  const days = 40, max = 110;
  const x = (d: number) => pad.l + (d / days) * (W - pad.l - pad.r);
  const y = (v: number) => H - pad.b - (v / max) * (H - pad.t - pad.b);
  const good = Array.from({ length: days + 1 }, (_, d) => `${x(d)},${y(Math.min(40, 10 + d))}`).join(' ');
  const bad = `${x(0)},${y(10)} ${x(6)},${y(10)} ${x(7)},${y(100)} ${x(14)},${y(100)}`;
  return (
    <Frame caption="Good warmup climbs slowly and levels off. A jump in volume looks like a bot.">
      <svg viewBox={`0 0 ${W} ${H}`} className="h-auto w-full min-w-[300px]" role="img" aria-label="Warmup volume per day: a gradual ramp from 10 to 40 against a jump to 100">
        {[0, 40, 80].map((v) => (
          <g key={v}>
            <line x1={pad.l} x2={W - pad.r} y1={y(v)} y2={y(v)} className="stroke-fd-border" strokeDasharray={v ? '3 4' : undefined} />
            <text x={pad.l - 6} y={y(v) + 4} textAnchor="end" className="fill-fd-muted-foreground text-[10px]">{v}</text>
          </g>
        ))}
        {[0, 10, 20, 30, 40].map((d) => (
          <text key={d} x={x(d)} y={H - 8} textAnchor="middle" className="fill-fd-muted-foreground text-[10px]">day {d}</text>
        ))}
        <polyline points={bad} fill="none" strokeWidth="2" strokeDasharray="5 4" className="stroke-rose-500" />
        <polyline points={good} fill="none" strokeWidth="2.5" strokeLinejoin="round" className="stroke-emerald-500" />
        <circle cx={x(30)} cy={y(40)} r="3.5" className="fill-emerald-500" />
        <text x={x(31)} y={y(40) - 8} className="fill-emerald-600 text-[11px] font-medium dark:fill-emerald-300">40 a day ceiling</text>
        <text x={x(14) + 6} y={y(100) + 4} className="fill-rose-600 text-[11px] font-medium dark:fill-rose-300">jump to 100 a day</text>
      </svg>
      <div className="mt-3 flex flex-wrap gap-x-5 gap-y-1.5 text-[12.5px] text-fd-muted-foreground">
        <span className="inline-flex items-center gap-2"><span className="h-0.5 w-5 bg-emerald-500" />Start at 10, add 1 a day, stop at 40</span>
        <span className="inline-flex items-center gap-2"><span className="h-0.5 w-5 border-t-2 border-dashed border-rose-500" />Climbing to 100 a day looks unnatural</span>
      </div>
    </Frame>
  );
}

const PARTNERS = ['Gmail', 'Outlook', 'Yahoo', 'iCloud'];

/** Your mailbox trading short messages with other pool members, who open and reply. */
export function WarmupExchange() {
  return (
    <Frame caption="Warmup looks like a real person starting to use a new inbox: small volumes, opens and replies.">
      <div className="grid items-center gap-4 md:grid-cols-[1fr_auto_1fr]">
        <div className="rounded-lg border border-fd-border bg-fd-background px-4 py-4 text-center">
          <div className="text-[14px] font-medium text-fd-foreground">Your mailbox</div>
          <div className="mt-1 text-[12.5px] text-fd-muted-foreground">Sends short, plain-text messages</div>
        </div>
        <div className="flex flex-col items-center gap-2 text-[12px] text-fd-muted-foreground">
          <span className="inline-flex items-center gap-1.5"><Dir />sends</span>
          <span className="inline-flex items-center gap-1.5"><Dir back />opens, replies about 30% of the time</span>
        </div>
        <div className="grid grid-cols-2 gap-2">
          {PARTNERS.map((p) => (
            <div key={p} className="rounded-lg border border-fd-border bg-fd-background px-3 py-2.5 text-center">
              <div className="text-[13px] font-medium text-fd-foreground">{p}</div>
              <div className="text-[11.5px] text-fd-muted-foreground">pool mailbox</div>
            </div>
          ))}
        </div>
      </div>
    </Frame>
  );
}

function Dir({ back }: { back?: boolean }) {
  return (
    <svg width="34" height="10" viewBox="0 0 34 10" fill="none" className={cn('text-fd-muted-foreground/70', back && 'rotate-180')} aria-hidden="true">
      <path d="M0 5h31m0 0-4-4m4 4-4 4" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

const HEALTH: { name: string; t: Tone; when: string; does: string }[] = [
  { name: 'Watch', t: 'info', when: 'Spam placement 10% or more, or complaints 0.03% or more', does: 'Lower volume, more spacing' },
  { name: 'Throttled', t: 'watch', when: 'Spam placement 20% or more', does: 'Warmup and cold sending at half volume; lifts by itself' },
  { name: 'Quarantine', t: 'bad', when: 'Complaints 0.10%, bounces 5%, or repeated tampering', does: 'Out of the paid pool for 7 days' },
  { name: 'Block', t: 'bad', when: 'Complaints 0.30% or bounces 10%', does: 'Blocked for 30 days' },
];

/** The four health bands, mildest first. */
export function HealthBands() {
  return (
    <Frame caption="Placement only ever slows a mailbox down. Complaints and bounces are what remove it.">
      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
        {HEALTH.map((h, i) => (
          <div key={h.name} className="rounded-lg border border-fd-border bg-fd-background p-3.5">
            <div className="flex items-center justify-between">
              <Pill t={h.t}>{h.name}</Pill>
              <span className="text-[11px] tabular-nums text-fd-muted-foreground">{i + 1} of 4</span>
            </div>
            <Label className="mt-3">When</Label>
            <div className="mt-1 text-[12.5px] leading-[1.45] text-fd-foreground">{h.when}</div>
            <Label className="mt-2.5">What happens</Label>
            <div className="mt-1 text-[12.5px] leading-[1.45] text-fd-foreground/80">{h.does}</div>
          </div>
        ))}
      </div>
    </Frame>
  );
}

/** Free and premium pools stay apart; the one crossing is mirrored. */
export function PoolTiers() {
  return (
    <Frame caption="The pools are separate. A premium mailbox short of partners borrows proven free mailboxes, and they write back.">
      <div className="grid items-stretch gap-3 md:grid-cols-[1fr_auto_1fr]">
        <Tier name="Premium pool" t="info" items={['Only warmed mailboxes that meet reputation criteria', 'Stricter quarantine thresholds']} />
        <div className="flex flex-col items-center justify-center gap-2 px-2 text-center text-[12px] text-fd-muted-foreground">
          <span className="inline-flex items-center gap-1.5"><Dir />borrows proven free mailboxes</span>
          <span className="inline-flex items-center gap-1.5"><Dir back />they write back</span>
        </div>
        <Tier name="Free pool" t="plain" items={['Useful for testing', 'Includes higher-risk members', 'Never mixed silently into premium']} />
      </div>
    </Frame>
  );
}

function Tier({ name, t, items }: { name: string; t: Tone; items: string[] }) {
  return (
    <div className="rounded-lg border border-fd-border bg-fd-background p-4">
      <Pill t={t}>{name}</Pill>
      <ul className="mt-3 space-y-1.5 text-[12.5px] text-fd-foreground/85">
        {items.map((i) => (
          <li key={i} className="flex gap-2"><span className={cn('mt-1.5 size-1.5 shrink-0 rounded-full', fill[t === 'plain' ? 'plain' : t])} />{i}</li>
        ))}
      </ul>
    </div>
  );
}

const RECOVERY: { days: string; warmup: string; cold: string; t: Tone }[] = [
  { days: 'Days 1 to 7', warmup: 'Warmup only, 5 a day', cold: 'No cold sends', t: 'bad' },
  { days: 'Days 8 to 14', warmup: 'Warmup at 10 a day', cold: 'No cold sends', t: 'bad' },
  { days: 'Days 15 to 28', warmup: '15 to 25 a day, +1 a day', cold: 'A small, soft sequence to safe recipients', t: 'watch' },
  { days: 'Days 29 to 42', warmup: '30 to 40 a day', cold: 'Cold at half the normal budget', t: 'info' },
  { days: 'Day 43 on', warmup: 'Evaluate', cold: 'Above 90% placement and below 0.03% complaints: back to normal', t: 'good' },
];

/** The recovery ramp, treating the mailbox like a brand-new one. */
export function RecoveryRamp() {
  return (
    <Frame caption="Recovery is mostly patience: six weeks of a slow ramp before the mailbox goes back to normal.">
      <ol className="grid gap-2 md:grid-cols-5">
        {RECOVERY.map((r) => (
          <li key={r.days} className="flex flex-col rounded-lg border border-fd-border bg-fd-background p-3">
            <span className={cn('h-1 w-full rounded-full', fill[r.t])} />
            <div className="mt-2.5 text-[13px] font-medium text-fd-foreground">{r.days}</div>
            <div className="mt-1.5 text-[12.5px] text-fd-foreground/85">{r.warmup}</div>
            <div className="mt-1 text-[12px] leading-[1.45] text-fd-muted-foreground">{r.cold}</div>
          </li>
        ))}
      </ol>
    </Frame>
  );
}
