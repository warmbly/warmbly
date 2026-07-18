// formatUsage renders what an AI call actually cost, from the usage-based
// settle: "2 credits · 1.4k tok". Empty on unmetered (local model) setups so
// the UI shows nothing instead of "0 credits".
export default function formatUsage(charged: number, tokens: number): string {
    if (charged <= 0) return "";
    const t = tokens >= 1000 ? `${(tokens / 1000).toFixed(1)}k` : String(tokens);
    const credits = `${charged} credit${charged === 1 ? "" : "s"}`;
    return tokens > 0 ? `${credits} · ${t} tok` : credits;
}
