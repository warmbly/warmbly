// Dollar formatting and parsing for the workspace balance and AI costs.

/** Adding money and automatic reload take $5 to $5,000 at a time. */
export const MIN_TOP_UP_USD = 5;
export const MAX_TOP_UP_USD = 5000;
/** The amounts offered first; any other is typed. */
export const TOP_UP_PRESETS = [10, 25, 50, 100, 250] as const;
/** The amount picked first when nothing else says. */
export const DEFAULT_TOP_UP_USD = 25;
/** Below this no metered call can start, so AI is paused. */
export const OUT_OF_BALANCE_BELOW = 0.01;

const two = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 2, maximumFractionDigits: 2 });
const whole = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 0, maximumFractionDigits: 0 });

/** "$4.80". Balances and amounts the user types. */
export function fmtUsd(n: number): string {
    return two.format(Object.is(n, -0) ? 0 : n);
}

/** "$25" for whole dollars, "$25.50" otherwise. Presets and button labels. */
export function fmtUsdTrim(n: number): string {
    return Number.isInteger(Math.round(n * 100) / 100) ? whole.format(n) : two.format(n);
}

/** What one action cost: "$0.0094", "$0.42". Keeps two significant digits under a cent. */
export function fmtUsdCost(n: number): string {
    const abs = Math.abs(n);
    if (abs === 0) return "$0.00";
    if (abs >= 0.01) return two.format(n);
    const digits = Math.min(6, Math.max(3, 1 - Math.floor(Math.log10(abs)) + 1));
    return `${n < 0 ? "-" : ""}$${abs.toFixed(digits).replace(/0+$/, "")}`;
}

/** A rough estimate: "about $0.02", "under $0.01". */
export function fmtUsdAbout(n: number): string {
    if (n > 0 && n < 0.01) return "under $0.01";
    return `about ${fmtUsd(n)}`;
}

/** "$1,250.50", "1250.5" or "$25" to a number of dollars; null when it is not an amount. */
export function parseUsd(text: string): number | null {
    const t = text.trim().replace(/^\$/, "").replace(/[,\s]/g, "");
    if (!/^\d+(\.\d{0,2})?$/.test(t)) return null;
    return Math.round(Number(t) * 100) / 100;
}

/** An amount the server accepts for adding money or a reload. */
export function validTopUp(usd: number | null): usd is number {
    return usd !== null && usd >= MIN_TOP_UP_USD && usd <= MAX_TOP_UP_USD;
}

/** Rounds to whole cents, the unit a card is charged in. */
export function roundCents(n: number): number {
    return Math.round(n * 100) / 100;
}
