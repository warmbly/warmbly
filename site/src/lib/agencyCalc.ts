// Per-client cost of an agency plan against the cheapest regular plans that send as much.
export type Billing = 'monthly' | 'annual';

export interface RegularPlan {
  name: string;
  price: number;
  annual: number;
  perDay: number;
}

export interface CalcPlan {
  name: string;
  price: number;
  annual: number;
  pool: number;
}

export interface CalcResult {
  perClient: string;
  poolEach: string;
  regularLabel: string;
  regularTotal: string;
  agencyTotal: string;
  verdict: string;
  saving: boolean;
}

// An agency plan is for more than one client; one workspace is what the regular plans are for.
export const CLIENTS_MIN = 2;
export const CLIENTS_MAX = 50;
export const CLIENTS_DEFAULT = 10;
// A regular plan's daily cap counted on every day of the month, which favours the regular plans.
export const DAYS_A_MONTH = 30;

const usd = (n: number, cents = false) =>
  `$${n.toLocaleString('en-US', { minimumFractionDigits: cents ? 2 : 0, maximumFractionDigits: cents ? 2 : 0 })}`;
const num = (n: number) => n.toLocaleString('en-US');

// The cheapest regular plan whose daily cap covers one client's even share of the pool, if any does.
function regularFor(regular: RegularPlan[], pool: number, clients: number): RegularPlan | undefined {
  const perDay = Math.ceil(pool / clients / DAYS_A_MONTH);
  return [...regular].sort((a, b) => a.perDay - b.perDay).find((r) => r.perDay >= perDay);
}

function costs(p: CalcPlan, regular: RegularPlan[], clients: number, mode: Billing) {
  const agency = mode === 'annual' ? p.annual : p.price;
  const match = regularFor(regular, p.pool, clients);
  const total = match ? (mode === 'annual' ? match.annual : match.price) * clients : undefined;
  return { agency, match, total };
}

export function calc(p: CalcPlan, regular: RegularPlan[], clients: number, mode: Billing): CalcResult {
  const { agency, match, total } = costs(p, regular, clients, mode);
  const label = clients === 1 ? 'client' : 'clients';
  const base = { perClient: usd(agency / clients, true), poolEach: num(Math.floor(p.pool / clients)), agencyTotal: usd(agency) };
  if (!match || total === undefined) {
    return {
      ...base,
      regularLabel: 'Regular plans',
      regularTotal: 'Enterprise',
      saving: false,
      verdict: `No regular plan sends ${num(p.pool / clients)} a month from one workspace, so the comparison is with Enterprise.`,
    };
  }
  const regularLabel = `${match.name} for each client, ${num(match.perDay)} a day`;
  const diff = total - agency;
  if (diff > 0) {
    return { ...base, regularLabel, regularTotal: usd(total), saving: true, verdict: `${usd(diff)} a month less with ${p.name}, ${usd(diff * 12)} a year` };
  }
  // The first client count from which the agency plan stays cheaper, up to the control's limit.
  let from: number | undefined;
  for (let n = CLIENTS_MAX; n > clients; n--) {
    const c = costs(p, regular, n, mode);
    if (c.total === undefined || c.total <= c.agency) break;
    from = n;
  }
  const gap = diff === 0 ? `${match.name} costs the same` : `${match.name} costs ${usd(-diff)} a month less`;
  return {
    ...base,
    regularLabel,
    regularTotal: usd(total),
    saving: false,
    verdict: `${gap} for ${clients} ${label}.${from ? ` ${p.name} costs less from ${from} clients.` : ''}`,
  };
}
