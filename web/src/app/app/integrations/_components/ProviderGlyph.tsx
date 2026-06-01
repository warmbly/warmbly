// A small branded glyph for each provider. We don't ship third-party logo
// assets, so we render the provider initial on a per-brand tinted tile —
// distinct enough to scan a directory quickly while staying on-theme.

import { cn } from "@/lib/utils";

const BRAND: Record<string, { bg: string; ring: string; text: string }> = {
    hubspot: { bg: "bg-orange-50", ring: "ring-orange-200", text: "text-orange-600" },
    salesforce: { bg: "bg-sky-50", ring: "ring-sky-200", text: "text-sky-600" },
    pipedrive: { bg: "bg-slate-100", ring: "ring-slate-300", text: "text-slate-700" },
    close: { bg: "bg-indigo-50", ring: "ring-indigo-200", text: "text-indigo-600" },
    zapier: { bg: "bg-orange-50", ring: "ring-orange-200", text: "text-orange-600" },
    make: { bg: "bg-violet-50", ring: "ring-violet-200", text: "text-violet-600" },
    n8n: { bg: "bg-rose-50", ring: "ring-rose-200", text: "text-rose-600" },
    slack: { bg: "bg-fuchsia-50", ring: "ring-fuchsia-200", text: "text-fuchsia-600" },
    discord: { bg: "bg-indigo-50", ring: "ring-indigo-200", text: "text-indigo-600" },
    calendly: { bg: "bg-sky-50", ring: "ring-sky-200", text: "text-sky-600" },
    cal_com: { bg: "bg-slate-100", ring: "ring-slate-300", text: "text-slate-800" },
    google_sheets: { bg: "bg-emerald-50", ring: "ring-emerald-200", text: "text-emerald-600" },
};

export default function ProviderGlyph({
    provider,
    name,
    size = 9,
}: {
    provider: string;
    name: string;
    size?: 7 | 9 | 10;
}) {
    const brand = BRAND[provider] ?? { bg: "bg-sky-50", ring: "ring-sky-100", text: "text-sky-700" };
    const dim = size === 7 ? "w-7 h-7 text-[12px]" : size === 10 ? "w-10 h-10 text-[15px]" : "w-9 h-9 text-[13px]";
    return (
        <div
            className={cn(
                "rounded-md ring-1 inline-flex items-center justify-center font-semibold uppercase shrink-0",
                dim,
                brand.bg,
                brand.ring,
                brand.text,
            )}
        >
            {name.charAt(0)}
        </div>
    );
}
