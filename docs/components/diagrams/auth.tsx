import { Frame, Label, Pill, fill, type Tone } from './frame';
import { cn } from '@/lib/cn';

// SPF, DKIM and DMARC: three checks on one email, the DMARC rollout and the sub-domain pattern.

function Check({ t = 'good' }: { t?: Tone }) {
  return (
    <span className={cn('inline-flex size-5 shrink-0 items-center justify-center rounded-full text-white', fill[t])} aria-hidden="true">
      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3.2" strokeLinecap="round" strokeLinejoin="round"><path d="M20 6 9 17l-5-5" /></svg>
    </span>
  );
}

/** One message, and what each record checks on it. */
export function AuthChecks() {
  return (
    <Frame caption="SPF and DKIM prove where the mail came from. DMARC passes when either one matches the From address you see.">
      <div className="grid gap-5 lg:grid-cols-[0.95fr_1.05fr]">
        <div className="rounded-lg border border-fd-border bg-fd-background font-mono text-[12px]">
          <div className="border-b border-fd-border px-4 py-2 font-sans text-[11px] font-medium uppercase tracking-[0.12em] text-fd-muted-foreground">One email</div>
          <dl className="space-y-1.5 px-4 py-3">
            <Hdr k="From" v="maya@acme.com" hl />
            <Hdr k="MAIL FROM" v="bounce.acme.com" />
            <Hdr k="DKIM-Signature" v="d=acme.com; s=google" />
            <Hdr k="Subject" v="Quick question" />
          </dl>
        </div>
        <div className="space-y-2.5">
          <Step name="SPF" q="Is the server that sent it allowed to send for bounce.acme.com?" a="Checked against the domain's SPF record" />
          <Step name="DKIM" q="Is the signature valid for acme.com, and was the message changed in flight?" a="Checked with the public key published in DNS" />
          <Step name="DMARC" q="Does the SPF or DKIM domain match the From domain, acme.com?" a="If not, the receiver applies your policy: none, quarantine or reject" />
        </div>
      </div>
    </Frame>
  );
}

function Hdr({ k, v, hl }: { k: string; v: string; hl?: boolean }) {
  return (
    <div className="flex gap-3">
      <dt className="w-28 shrink-0 text-fd-muted-foreground">{k}</dt>
      <dd className={cn('min-w-0 break-all text-fd-foreground', hl && 'rounded bg-sky-50 px-1 text-sky-800 dark:bg-sky-500/15 dark:text-sky-200')}>{v}</dd>
    </div>
  );
}

function Step({ name, q, a }: { name: string; q: string; a: string }) {
  return (
    <div className="flex gap-3 rounded-lg border border-fd-border bg-fd-background px-4 py-3">
      <Check />
      <div className="min-w-0">
        <div className="text-[13.5px] font-medium text-fd-foreground">{name}</div>
        <div className="mt-0.5 text-[12.5px] text-fd-foreground/85">{q}</div>
        <div className="mt-0.5 text-[12px] text-fd-muted-foreground">{a}</div>
      </div>
    </div>
  );
}

const ROLLOUT: { p: string; text: string; t: Tone }[] = [
  { p: 'SPF and DKIM', text: 'Publish both for every service that sends as you', t: 'plain' },
  { p: 'p=none', text: 'Monitor only. Change nothing else for two weeks and read the reports', t: 'info' },
  { p: 'p=quarantine; pct=25', text: 'Bring every sender into alignment, then ramp pct to 100 over a month', t: 'watch' },
  { p: 'p=reject', text: 'When reports are clean: the strongest spoof protection', t: 'good' },
];

export function DmarcRollout() {
  return (
    <Frame caption="Roll DMARC out in steps, so mail you forgot about is found before it is rejected.">
      <ol className="grid gap-2 md:grid-cols-4">
        {ROLLOUT.map((r, i) => (
          <li key={r.p} className="relative rounded-lg border border-fd-border bg-fd-background p-3.5">
            <div className="text-[11px] tabular-nums text-fd-muted-foreground">Step {i + 1}</div>
            <div className="mt-1.5"><Pill t={r.t} className="font-mono">{r.p}</Pill></div>
            <div className="mt-2 text-[12.5px] leading-[1.45] text-fd-foreground/85">{r.text}</div>
          </li>
        ))}
      </ol>
    </Frame>
  );
}

const DOMAINS = [
  { d: 'acme.com', use: 'Transactional and human mail', note: 'DMARC reject after rollout', t: 'good' as Tone },
  { d: 'outreach.acme.com', use: 'Cold campaigns', note: 'Its own SPF, DKIM and DMARC', t: 'info' as Tone },
  { d: 'news.acme.com', use: 'Newsletters', note: 'Separate authentication and reputation', t: 'plain' as Tone },
];

export function SubdomainPattern() {
  return (
    <Frame caption="A cold reputation problem stays on its sub-domain and never reaches the inboxes you run the company from.">
      <div className="grid gap-2 md:grid-cols-3">
        {DOMAINS.map((d) => (
          <div key={d.d} className="rounded-lg border border-fd-border bg-fd-background p-4">
            <div className="font-mono text-[13px] font-medium text-fd-foreground">{d.d}</div>
            <div className="mt-2"><Pill t={d.t}>{d.use}</Pill></div>
            <div className="mt-2 text-[12.5px] text-fd-muted-foreground">{d.note}</div>
          </div>
        ))}
      </div>
    </Frame>
  );
}

const REGIONS: { name: string; law: string; consent: string; t: Tone; points: string[] }[] = [
  {
    name: 'United States',
    law: 'CAN-SPAM',
    consent: 'No prior consent for B2B',
    t: 'good',
    points: ['Truthful From and subject line', 'A physical postal address', 'Unsubscribe honoured within 10 business days'],
  },
  {
    name: 'EU and UK',
    law: 'GDPR and PECR',
    consent: 'B2B on legitimate interest; B2C needs consent',
    t: 'watch',
    points: ['A clear business reason to write', 'Opt-outs honoured immediately', 'Say where you got their details'],
  },
  {
    name: 'Australia',
    law: 'Spam Act',
    consent: 'Consent: express, inferred or implied',
    t: 'watch',
    points: ['Accurate sender identification', 'A working unsubscribe', 'Inferred consent is generous for B2B'],
  },
  {
    name: 'Canada',
    law: 'CASL',
    consent: 'Express or implied consent required',
    t: 'bad',
    points: ['The strictest of the four', 'Sender identification and contact info', 'Penalties up to CAD $10M per violation'],
  },
];

/** The four regimes side by side, least to most strict. */
export function ComplianceMap() {
  return (
    <Frame caption="General information, not legal advice. Ordered from least to most strict about consent.">
      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
        {REGIONS.map((r) => (
          <div key={r.name} className="flex flex-col rounded-lg border border-fd-border bg-fd-background p-4">
            <div className="text-[14px] font-medium text-fd-foreground">{r.name}</div>
            <div className="text-[12px] text-fd-muted-foreground">{r.law}</div>
            <div className="mt-3"><Pill t={r.t}>{r.consent}</Pill></div>
            <ul className="mt-3 space-y-1.5 text-[12.5px] text-fd-foreground/85">
              {r.points.map((p) => (
                <li key={p} className="flex gap-2"><span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-fd-muted-foreground/40" />{p}</li>
              ))}
            </ul>
          </div>
        ))}
      </div>
      <Label className="mt-4">Everywhere: a real sender, a clear opt-out, and suppression of anyone who opts out</Label>
    </Frame>
  );
}

const CONTACTS = [
  { name: 'Alex', company: 'Acme', out: 'Hi Alex, I saw Acme is hiring.' },
  { name: 'Sam', company: '', out: 'Hi Sam,' },
];

/** One template, rendered for two contacts. */
export function MergePreview() {
  return (
    <Frame caption="Each email is rendered for its own recipient when it sends. A missing field never leaves a gap.">
      <div className="grid gap-4 lg:grid-cols-[1fr_auto_1fr] lg:items-center">
        <div className="rounded-lg border border-fd-border bg-fd-background">
          <div className="border-b border-fd-border px-4 py-2 text-[11px] font-medium uppercase tracking-[0.12em] text-fd-muted-foreground">Template</div>
          <pre className="whitespace-pre-wrap px-4 py-3 font-mono text-[12.5px] leading-[1.7] text-fd-foreground">
            Hi <V>{'{{.FirstName}}'}</V>,<V>{'{{if .Company}}'}</V> I saw <V>{'{{.Company}}'}</V> is hiring.<V>{'{{end}}'}</V>
          </pre>
        </div>
        <svg width="28" height="14" viewBox="0 0 28 14" fill="none" className="mx-auto text-fd-muted-foreground/60 max-lg:rotate-90" aria-hidden="true">
          <path d="M0 7h25m0 0-5-5m5 5-5 5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        <div className="space-y-2">
          {CONTACTS.map((c) => (
            <div key={c.name} className="rounded-lg border border-fd-border bg-fd-background px-4 py-3">
              <div className="text-[11.5px] text-fd-muted-foreground">
                To {c.name} · Company: {c.company ? <span className="text-fd-foreground">{c.company}</span> : <span className="italic">blank</span>}
              </div>
              <div className="mt-1 text-[13.5px] text-fd-foreground">{c.out}</div>
            </div>
          ))}
        </div>
      </div>
    </Frame>
  );
}

function V({ children }: { children: string }) {
  return <span className="rounded bg-sky-50 px-0.5 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300">{children}</span>;
}
