// A managed workspace's per-mailbox enablement, read-only: the partner that bills the workspace sets it.
import { Building2Icon } from "lucide-react";
import usePartnerManaged from "@/hooks/usePartnerManaged";
import usePartnerMailboxes from "@/lib/api/hooks/app/emails/usePartnerMailboxes";
import type { PartnerEnablement } from "@/lib/api/models/app/emails/PartnerMailbox";
import { cn } from "@/lib/utils";

const LABELS: Record<PartnerEnablement, string> = {
    sending_warmup: "Sending & warmup",
    warmup_only: "Warmup only",
    off: "Off",
};

const TONES: Record<PartnerEnablement, string> = {
    sending_warmup: "border-emerald-200 bg-emerald-50 text-emerald-700",
    warmup_only: "border-sky-200 bg-sky-50 text-sky-700",
    off: "border-slate-200 bg-slate-50 text-slate-500",
};

/** Mailboxes connected here and not turned on yet read as off, as the partner bills them. */
function useEnablement(emailAccountId: string): { enablement: PartnerEnablement; managed: boolean; partner: string } | null {
    const partner = usePartnerManaged();
    const rows = usePartnerMailboxes();
    if (!partner) return null;
    const row = rows.get(emailAccountId);
    return { enablement: row?.enablement ?? "off", managed: row?.managed ?? false, partner: partner.display_name };
}

export function PartnerEnablementChip({ emailAccountId, className }: { emailAccountId: string; className?: string }) {
    const e = useEnablement(emailAccountId);
    if (!e) return null;
    return (
        <span
            title={`Set in ${e.partner}`}
            className={cn("inline-flex items-center h-[18px] px-1.5 rounded-full border text-[10.5px] font-medium shrink-0", TONES[e.enablement], className)}
        >
            {LABELS[e.enablement]}
        </span>
    );
}

export function PartnerEnablementNote({ emailAccountId }: { emailAccountId: string }) {
    const e = useEnablement(emailAccountId);
    if (!e) return null;
    const what =
        e.enablement === "off"
            ? `This mailbox is off: no campaigns and no warmup. Turn it on in ${e.partner}.`
            : e.enablement === "warmup_only"
              ? `This mailbox warms up; campaigns skip it. ${e.partner} sets this.`
              : `This mailbox sends campaigns and warms up. ${e.partner} sets this.`;
    return (
        <div className="shrink-0 px-5 py-2 flex items-start gap-2 border-b border-slate-200 bg-slate-50/60 text-[11.5px] text-slate-600">
            <Building2Icon className="w-3.5 h-3.5 mt-0.5 text-slate-400 shrink-0" />
            <div className="min-w-0">
                <span className={cn("inline-flex items-center h-[18px] px-1.5 mr-1.5 rounded-full border text-[10.5px] font-medium align-middle", TONES[e.enablement])}>
                    {LABELS[e.enablement]}
                </span>
                {what}
                {e.managed && ` Managed by ${e.partner}, so it is removed or reconnected there.`}
            </div>
        </div>
    );
}
