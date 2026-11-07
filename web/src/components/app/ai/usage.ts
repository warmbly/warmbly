import { fmtUsdCost } from "@/lib/money";

// formatUsage renders what an AI call actually cost, from the usage-based
// settle: "$0.0094 · 1.4k tok". Empty on unmetered (local model) setups so
// the UI shows nothing instead of "$0.00".
export default function formatUsage(chargedUsd: number | null | undefined, tokens: number): string {
    if (!chargedUsd || chargedUsd <= 0) return "";
    const t = tokens >= 1000 ? `${(tokens / 1000).toFixed(1)}k` : String(tokens);
    const cost = fmtUsdCost(chargedUsd);
    return tokens > 0 ? `${cost} · ${t} tok` : cost;
}
