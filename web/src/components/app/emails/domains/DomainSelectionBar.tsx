// The floating bar over selected sending domains: set tracking and redirects in
// bulk, re-check DNS, copy or export what is still to add, and clear either.
import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import { useQueryClient } from "@tanstack/react-query";
import {
    CheckIcon,
    ClipboardCopyIcon,
    DownloadIcon,
    GlobeIcon,
    Loader2Icon,
    MoreHorizontalIcon,
    MousePointerClickIcon,
    RefreshCwIcon,
    Trash2Icon,
    XIcon,
} from "lucide-react";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuSeparator, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import type { SendingDomain } from "@/lib/api/models/app/emails/SendingDomain";
import {
    deleteDomainRedirect,
    getTrackingSuggestion,
    setDomainTracking,
    setDomainVendorForwarding,
    verifyDomainRedirect,
} from "@/lib/api/client/app/emails/sendingDomains";
import { SENDING_DOMAINS_KEY } from "@/lib/api/hooks/app/emails/useSendingDomains";
import { vendorLabel } from "@/lib/api/models/app/emails/MailboxSources";
import { redirectState, trackingState } from "./rules";

/** Runs fn over items a few at a time and reports how many failed. */
async function eachLimited<T>(items: T[], fn: (item: T) => Promise<unknown>, limit = 4): Promise<number> {
    let failed = 0;
    let next = 0;
    const worker = async () => {
        while (next < items.length) {
            const item = items[next++];
            try {
                await fn(item);
            } catch {
                failed++;
            }
        }
    };
    await Promise.all(Array.from({ length: Math.min(limit, items.length) }, worker));
    return failed;
}

const n = (count: number, one: string, many: string) => `${count.toLocaleString()} ${count === 1 ? one : many}`;

function csvCell(v: string | number | boolean): string {
    const s = String(v);
    return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

function exportCsv(list: SendingDomain[]) {
    const head = ["domain", "mailboxes", "authentication", "spf", "dkim", "dmarc", "tracking_host", "tracking_verified", "redirect_to", "redirect_status", "vendor"];
    const lines = list.map((d) => {
        const t = d.tracking_domains[0];
        const r = redirectState(d);
        return [
            d.domain,
            d.mailboxes,
            d.auth_state,
            d.auth_spf,
            d.auth_dkim,
            d.auth_dmarc,
            d.tracking_domains.map((x) => x.host).join(" "),
            t ? d.tracking_domains.every((x) => x.verified) : "",
            d.redirect?.target_url || d.vendor_domain?.forwarding || "",
            r,
            vendorLabel(d.vendor_domain?.vendor),
        ]
            .map(csvCell)
            .join(",");
    });
    const blob = new Blob([[head.join(","), ...lines].join("\n") + "\n"], { type: "text/csv" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `sending-domains-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    // Some browsers start the download after click() returns, so the URL has to outlive this tick.
    const href = a.href;
    window.setTimeout(() => URL.revokeObjectURL(href), 1000);
}

export default function DomainSelectionBar({
    selected,
    onTracking,
    onRedirect,
    onClear,
}: {
    selected: SendingDomain[];
    onTracking: () => void;
    onRedirect: () => void;
    onClear: () => void;
}) {
    const qc = useQueryClient();
    const confirm = useConfirm();
    const [busy, setBusy] = React.useState("");
    const count = selected.length;

    const refresh = () => qc.invalidateQueries({ queryKey: SENDING_DOMAINS_KEY });

    async function checkDns() {
        // A lone host waiting for DNS is re-applied as is, which re-checks its CNAME; the redirect is verified.
        const hosts = selected.filter((d) => d.tracking_domains.length === 1 && trackingState(d) === "pending");
        const redirects = selected.filter((d) => d.redirect && !d.redirect.verified);
        if (hosts.length + redirects.length === 0) {
            toast.success("Nothing is waiting for DNS on these domains");
            return;
        }
        setBusy("check");
        const failed =
            (await eachLimited(hosts, (d) => setDomainTracking(d.domain, d.tracking_domains[0].host))) +
            (await eachLimited(redirects, (d) => verifyDomainRedirect(d.domain)));
        await refresh();
        setBusy("");
        if (failed) toast.error(`${n(failed, "check", "checks")} could not run. Try again in a moment.`);
        else toast.success(`Checked ${n(hosts.length + redirects.length, "record set", "record sets")}`);
    }

    async function copyRecords() {
        const pendingHosts = selected.flatMap((d) => d.tracking_domains.filter((t) => !t.verified).map((t) => ({ d, host: t.host })));
        const redirectRows = selected.flatMap((d) =>
            d.redirect && !d.redirect.verified ? d.redirect.records.filter((r) => !r.ok).map((r) => ({ d, r })) : [],
        );
        if (pendingHosts.length + redirectRows.length === 0) {
            toast.success("Nothing left to add on these domains");
            return;
        }
        setBusy("copy");
        try {
            let target = "";
            if (pendingHosts.length > 0) target = (await getTrackingSuggestion(pendingHosts[0].d.domain)).cname_target;
            const lines = [
                ["Domain", "Type", "Name", "Value"].join("\t"),
                ...pendingHosts.map(({ d, host }) => [d.domain, "CNAME", host, target].join("\t")),
                ...redirectRows.map(({ d, r }) => [d.domain, r.type, r.name, r.value].join("\t")),
            ];
            await navigator.clipboard.writeText(lines.join("\n"));
            toast.success(`Copied ${n(lines.length - 1, "record", "records")}, ready to paste into a sheet`);
        } catch {
            toast.error("Could not copy the records. Open a domain to copy them one by one.");
        } finally {
            setBusy("");
        }
    }

    function clearTracking() {
        const withHost = selected.filter((d) => d.tracking_domains.length > 0);
        if (withHost.length === 0) {
            toast.success("None of these domains has a tracking domain");
            return;
        }
        const boxes = withHost.reduce((s, d) => s + d.mailboxes, 0);
        confirm.show(
            `Clear the tracking domain on ${n(withHost.length, "domain", "domains")}? Links from their ${n(boxes, "mailbox", "mailboxes")} go back to the shared tracking host.`,
            async () => {
                const failed = await eachLimited(withHost, (d) => setDomainTracking(d.domain, ""));
                await refresh();
                qc.invalidateQueries({ queryKey: ["emails", "list"] });
                if (failed) toast.error(`${n(failed, "domain", "domains")} could not be cleared`);
                else toast.success(`Tracking domain cleared on ${n(withHost.length, "domain", "domains")}`);
            },
        );
    }

    function removeRedirects() {
        const dns = selected.filter((d) => d.redirect);
        const forwarded = selected.filter((d) => !d.redirect && d.vendor_domain?.forwarding && d.vendor_domain.can_unforward);
        const stuck = selected.filter((d) => !d.redirect && d.vendor_domain?.forwarding && !d.vendor_domain.can_unforward);
        const total = dns.length + forwarded.length;
        if (total === 0) {
            toast(stuck.length ? `${vendorLabel(stuck[0].vendor_domain?.vendor)} does not remove forwarding through its API. Remove it in their dashboard.` : "None of these domains redirects anywhere");
            return;
        }
        confirm.show(
            `Stop redirecting ${n(total, "domain", "domains")}? Visitors to ${total === 1 ? "it" : "them"} get an empty page again.${stuck.length ? ` ${n(stuck.length, "domain is", "domains are")} forwarded by a vendor that cannot remove it through its API and stay as they are.` : ""}`,
            async () => {
                const failed =
                    (await eachLimited(dns, (d) => deleteDomainRedirect(d.domain))) +
                    (await eachLimited(forwarded, (d) => setDomainVendorForwarding(d.domain, "")));
                await refresh();
                if (failed) toast.error(`${n(failed, "redirect", "redirects")} could not be removed`);
                else toast.success(`Removed ${n(total, "redirect", "redirects")}`);
            },
        );
    }

    const hasTracking = selected.some((d) => d.tracking_domains.length > 0);
    const hasRedirect = selected.some((d) => redirectState(d) !== "none");

    return (
        <AnimatePresence>
            {count > 0 && (
                <motion.div
                    initial={{ y: 12, opacity: 0 }}
                    animate={{ y: 0, opacity: 1 }}
                    exit={{ y: 12, opacity: 0 }}
                    transition={{ duration: 0.16 }}
                    className="fixed bottom-[max(1.25rem,env(safe-area-inset-bottom))] left-1/2 -translate-x-1/2 z-30 flex flex-wrap justify-center max-w-[calc(100vw-1rem)] items-center gap-1.5 rounded-md border border-slate-200 bg-white shadow-[0_6px_20px_-4px_rgba(15,23,42,0.12),0_2px_4px_rgba(15,23,42,0.04)] px-2 py-1.5"
                >
                    <div className="inline-flex items-center gap-1.5 px-2 h-7 rounded bg-sky-50 text-sky-700 text-[12px] font-medium">
                        <CheckIcon className="w-3 h-3" />
                        <span>{count.toLocaleString()} selected</span>
                    </div>
                    <button
                        type="button"
                        onClick={onTracking}
                        className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded text-[12px] font-medium text-sky-700 hover:bg-sky-50 transition-colors"
                    >
                        <MousePointerClickIcon className="w-3.5 h-3.5" />
                        Tracking domain
                    </button>
                    <button
                        type="button"
                        onClick={onRedirect}
                        className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded text-[12px] font-medium text-slate-600 hover:bg-slate-100 transition-colors"
                    >
                        <GlobeIcon className="w-3.5 h-3.5" />
                        Redirect
                    </button>
                    <PopoverMenu side="top" align="center">
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                disabled={!!busy}
                                className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded text-[12px] font-medium text-slate-600 hover:bg-slate-100 transition-colors disabled:opacity-60"
                            >
                                {busy ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <MoreHorizontalIcon className="w-3.5 h-3.5" />}
                                More
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={220}>
                            <PopoverMenuItem icon={<RefreshCwIcon className="w-3 h-3" />} onSelect={() => void checkDns()}>
                                Check DNS now
                            </PopoverMenuItem>
                            <PopoverMenuItem icon={<ClipboardCopyIcon className="w-3 h-3" />} onSelect={() => void copyRecords()}>
                                Copy DNS records to add
                            </PopoverMenuItem>
                            <PopoverMenuItem icon={<DownloadIcon className="w-3 h-3" />} onSelect={() => exportCsv(selected)}>
                                Export as CSV
                            </PopoverMenuItem>
                            <PopoverMenuSeparator />
                            <PopoverMenuItem danger disabled={!hasTracking} icon={<Trash2Icon className="w-3 h-3" />} onSelect={clearTracking}>
                                Clear tracking domain
                            </PopoverMenuItem>
                            <PopoverMenuItem danger disabled={!hasRedirect} icon={<Trash2Icon className="w-3 h-3" />} onSelect={removeRedirects}>
                                Remove redirect
                            </PopoverMenuItem>
                        </PopoverMenuContent>
                    </PopoverMenu>
                    <div className="w-px h-4 bg-slate-200 mx-0.5" />
                    <button
                        type="button"
                        onClick={onClear}
                        className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded text-[12px] text-slate-500 hover:bg-slate-100 transition-colors"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                        Clear
                    </button>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
