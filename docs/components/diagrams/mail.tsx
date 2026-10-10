import { Arrow, Frame, Label, Pill, fill, type Tone } from './frame';
import { cn } from '@/lib/cn';

// Deliverability and inbox placement: what happens to a message after you send it.

const GATES = [
  { n: 1, name: 'Acceptance', by: 'The receiving server says 250', checks: 'Valid message, rate limit, basic IP block' },
  { n: 2, name: 'Filtering', by: 'A spam score', checks: 'Authentication, content, reputation' },
  { n: 3, name: 'Placement', by: 'Where it lands', checks: 'Inbox, Promotions, Spam, or a rule' },
];

export function MailJourney() {
  return (
    <Frame caption="Every message passes three gates. A delivery rate only sees the first one.">
      <div className="flex min-w-0 flex-col items-stretch md:flex-row md:items-center">
        <div className="rounded-lg border border-fd-border bg-fd-background px-4 py-3 text-center md:w-28">
          <div className="text-[13px] font-medium text-fd-foreground">You send</div>
        </div>
        {GATES.map((g) => (
          <div key={g.n} className="flex flex-col items-stretch md:flex-1 md:flex-row md:items-center">
            <Arrow />
            <div className="flex-1 rounded-lg border border-fd-border bg-fd-background px-4 py-3">
              <div className="flex items-center gap-2">
                <span className="inline-flex size-5 items-center justify-center rounded-full bg-fd-foreground text-[11px] font-medium text-fd-background">{g.n}</span>
                <span className="text-[14px] font-medium text-fd-foreground">{g.name}</span>
              </div>
              <div className="mt-1.5 text-[12.5px] text-fd-muted-foreground">{g.by}</div>
              <div className="mt-0.5 text-[12.5px] text-fd-foreground/80">{g.checks}</div>
            </div>
          </div>
        ))}
      </div>
      <div className="mt-4 grid grid-cols-3 gap-2 md:ml-[calc(7rem+28px)]">
        <div className="col-span-1 rounded-md border border-dashed border-fd-border px-3 py-2 text-center text-[12px] text-fd-muted-foreground">
          Delivery rate is measured here
        </div>
        <div className="col-span-2 rounded-md border border-dashed border-emerald-300 px-3 py-2 text-center text-[12px] text-emerald-700 dark:border-emerald-500/40 dark:text-emerald-300">
          Deliverability is what happens here: does it reach the inbox?
        </div>
      </div>
    </Frame>
  );
}

/** 100 sends as dots: accepted almost always, in the inbox far less often. */
export function DeliveryVsPlacement() {
  const dots = Array.from({ length: 100 }, (_, i) => (i < 30 ? 'good' : i < 99 ? 'watch' : 'plain') as Tone);
  return (
    <Frame caption="99% delivery and 30% placement is not unusual on a poorly authenticated cold mailbox.">
      <div className="grid gap-6 md:grid-cols-[auto_1fr] md:items-center">
        <div className="grid w-fit grid-cols-10 gap-1.5">
          {dots.map((t, i) => (
            <span key={i} className={cn('size-3.5 rounded-full', fill[t])} />
          ))}
        </div>
        <div className="space-y-3 text-[13.5px]">
          <Row tone="good" big="30" text="landed in the primary inbox" />
          <Row tone="watch" big="69" text="were accepted, then filtered to Spam or a tab" />
          <Row tone="plain" big="1" text="was rejected" />
          <div className="border-t border-fd-border pt-3 text-fd-muted-foreground">
            A delivery rate reports <b className="font-medium text-fd-foreground">99%</b>. Placement is <b className="font-medium text-fd-foreground">30%</b>.
          </div>
        </div>
      </div>
    </Frame>
  );
}

function Row({ tone: t, big, text }: { tone: Tone; big: string; text: string }) {
  return (
    <div className="flex items-center gap-3">
      <span className={cn('size-3 shrink-0 rounded-full', fill[t])} />
      <span className="w-7 text-[17px] font-medium tabular-nums text-fd-foreground">{big}</span>
      <span className="text-fd-muted-foreground">{text}</span>
    </div>
  );
}

const BANDS = [
  { from: 0, to: 60, t: 'bad' as Tone, name: 'Below 60%', text: 'Stop campaigns from this mailbox' },
  { from: 60, to: 80, t: 'watch' as Tone, name: '60 to 80%', text: 'Something is wrong: audit auth, content, volume' },
  { from: 80, to: 90, t: 'info' as Tone, name: '80 to 90%', text: 'Acceptable, watch for drift' },
  { from: 90, to: 100, t: 'good' as Tone, name: 'Above 90%', text: 'Healthy mailbox' },
];

export function PlacementScale() {
  return (
    <Frame caption="Inbox placement targets.">
      <div className="flex h-3 overflow-hidden rounded-full">
        {BANDS.map((b) => (
          <div key={b.name} className={fill[b.t]} style={{ width: `${b.to - b.from}%` }} />
        ))}
      </div>
      <div className="relative mt-1.5 h-4 text-[11px] tabular-nums text-fd-muted-foreground">
        {[0, 60, 80, 90, 100].map((v) => (
          <span key={v} className="absolute -translate-x-1/2 first:translate-x-0 last:-translate-x-full" style={{ left: `${v}%` }}>
            {v}%
          </span>
        ))}
      </div>
      <div className="mt-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
        {BANDS.map((b) => (
          <div key={b.name} className="rounded-lg border border-fd-border bg-fd-background px-3 py-2.5">
            <Pill t={b.t}>{b.name}</Pill>
            <div className="mt-1.5 text-[12.5px] leading-[1.45] text-fd-foreground/80">{b.text}</div>
          </div>
        ))}
      </div>
    </Frame>
  );
}

const SIGNALS: { name: string; weight: string; w: number; t: Tone }[] = [
  { name: 'Authentication', weight: 'Mandatory', w: 100, t: 'info' },
  { name: 'Sender reputation', weight: 'High', w: 80, t: 'good' },
  { name: 'Recipient engagement', weight: 'High', w: 80, t: 'good' },
  { name: 'Volume and pattern', weight: 'High', w: 80, t: 'good' },
  { name: 'Content', weight: 'Medium', w: 50, t: 'watch' },
];

export function SignalWeights() {
  return (
    <Frame caption="What decides placement. Weights are directional: providers do not publish formulas.">
      <div className="space-y-2.5">
        {SIGNALS.map((s) => (
          <div key={s.name} className="grid grid-cols-[150px_1fr_80px] items-center gap-3 text-[13px] max-sm:grid-cols-[110px_1fr_70px]">
            <span className="text-fd-foreground">{s.name}</span>
            <span className="h-2 rounded-full bg-fd-muted">
              <span className={cn('block h-2 rounded-full', fill[s.t])} style={{ width: `${s.w}%` }} />
            </span>
            <span className="text-right text-fd-muted-foreground">{s.weight}</span>
          </div>
        ))}
      </div>
      <Label className="mt-4">Authentication is mandatory. The others are weighed together</Label>
    </Frame>
  );
}
