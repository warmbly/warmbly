// Per-client cost of an agency plan against buying a regular plan for each client.
export type Billing = 'monthly' | 'annual';

export interface CalcPlan {
  name: string;
  price: number;
  annual: number;
  pool: number;
  vs: { name: string; price: number; annual: number };
}

export interface CalcResult {
  perClient: string;
  poolEach: string;
  regularTotal: string;
  agencyTotal: string;
  verdict: string;
  saving: boolean;
}

export const CLIENTS_MIN = 1;
export const CLIENTS_MAX = 50;
export const CLIENTS_DEFAULT = 10;

const usd = (n: number, cents = false) =>
  `$${n.toLocaleString('en-US', { minimumFractionDigits: cents ? 2 : 0, maximumFractionDigits: cents ? 2 : 0 })}`;

export function calc(p: CalcPlan, clients: number, mode: Billing): CalcResult {
  const agency = mode === 'annual' ? p.annual : p.price;
  const each = mode === 'annual' ? p.vs.annual : p.vs.price;
  const regular = each * clients;
  const diff = regular - agency;
  const from = Math.floor(agency / each) + 1;
  const label = clients === 1 ? 'client' : 'clients';
  return {
    perClient: usd(agency / clients, true),
    poolEach: Math.floor(p.pool / clients).toLocaleString('en-US'),
    regularTotal: usd(regular),
    agencyTotal: usd(agency),
    saving: diff > 0,
    verdict: diff > 0
      ? `${usd(diff)} a month less with ${p.name}, ${usd(diff * 12)} a year`
      : `${p.vs.name} costs ${usd(-diff)} a month less for ${clients} ${label}. ${p.name} costs less from ${from} clients.`,
  };
}
