// Stripe invoices for the workspace: the plan, money added, automatic reloads.

import { ExternalLinkIcon, FileDownIcon } from "lucide-react";
import { useBalanceInvoices } from "@/lib/api/hooks/app/subscription/useBalance";
import type { BalanceInvoice } from "@/lib/api/models/app/subscription/Credits";
import { Section, TableSurface } from "../_components/SectionShell";

const KIND: Record<string, string> = {
    subscription: "Plan",
    top_up: "Money added",
    auto_reload: "Automatic reload",
};

export default function InvoicesSection({ description }: { description?: string }) {
    const invoices = useBalanceInvoices();
    const rows = invoices.data?.data ?? [];
    return (
        <Section eyebrow="Invoices" description={description ?? "Receipts for the plan and for money added."}>
            {invoices.isPending ? (
                <div className="h-16 rounded bg-slate-100 animate-pulse" />
            ) : invoices.isError ? (
                <p className="text-[11.5px] text-slate-500">Invoices could not be loaded right now.</p>
            ) : rows.length === 0 ? (
                <p className="text-[11.5px] text-slate-500 leading-relaxed">No invoices yet.</p>
            ) : (
                <TableSurface>
                    <table className="w-full text-[12px]">
                        <thead>
                            <tr className="text-left text-[10.5px] uppercase tracking-[0.08em] text-slate-400 border-b border-slate-200">
                                <th className="font-medium px-3 py-2">Date</th>
                                <th className="font-medium px-3 py-2">For</th>
                                <th className="font-medium px-3 py-2 text-right">Amount</th>
                                <th className="font-medium px-3 py-2 hidden sm:table-cell">Status</th>
                                <th className="font-medium px-3 py-2 text-right">
                                    <span className="sr-only">Links</span>
                                </th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-slate-100">
                            {rows.map((inv) => (
                                <InvoiceRow key={inv.id} inv={inv} />
                            ))}
                        </tbody>
                    </table>
                </TableSurface>
            )}
        </Section>
    );
}

function InvoiceRow({ inv }: { inv: BalanceInvoice }) {
    const amount = new Intl.NumberFormat("en-US", { style: "currency", currency: (inv.currency || "usd").toUpperCase() }).format(
        inv.amount_cents / 100,
    );
    return (
        <tr className="text-slate-700">
            <td className="px-3 py-2 text-slate-500 tabular-nums whitespace-nowrap">
                {new Date(inv.created_at).toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" })}
            </td>
            <td className="px-3 py-2">
                <span className="text-slate-900">{KIND[inv.kind] ?? "Invoice"}</span>
                {inv.number && <span className="ml-1.5 text-[10.5px] text-slate-400 font-mono">{inv.number}</span>}
            </td>
            <td className="px-3 py-2 text-right font-mono tabular-nums">{amount}</td>
            <td className="px-3 py-2 hidden sm:table-cell">
                <StatusPill status={inv.status} />
            </td>
            <td className="px-3 py-2">
                <div className="flex items-center justify-end gap-1">
                    {inv.hosted_url && (
                        <a
                            href={inv.hosted_url}
                            target="_blank"
                            rel="noreferrer"
                            aria-label="Open invoice"
                            className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                        >
                            <ExternalLinkIcon className="w-3.5 h-3.5" />
                        </a>
                    )}
                    {inv.pdf_url && (
                        <a
                            href={inv.pdf_url}
                            target="_blank"
                            rel="noreferrer"
                            aria-label="Download PDF"
                            className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                        >
                            <FileDownIcon className="w-3.5 h-3.5" />
                        </a>
                    )}
                </div>
            </td>
        </tr>
    );
}

function StatusPill({ status }: { status: string }) {
    const s = (status ?? "").toLowerCase();
    const cls =
        s === "paid"
            ? "bg-emerald-50 text-emerald-700 border-emerald-100"
            : s === "open"
              ? "bg-amber-50 text-amber-700 border-amber-100"
              : "bg-slate-100 text-slate-500 border-slate-200";
    return (
        <span className={`inline-flex items-center text-[10px] uppercase tracking-[0.08em] font-semibold rounded-sm px-1.5 py-0.5 border ${cls}`}>
            {status || "unknown"}
        </span>
    );
}
