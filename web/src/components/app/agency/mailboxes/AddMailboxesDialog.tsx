// Agency > Mailboxes > Add mailboxes: pick the client, then how its mailboxes get connected.
// Whoever runs the client's inboxes can do it here, or the client can do it from a link.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { ArrowLeftIcon, ChevronRightIcon, FileSpreadsheetIcon, Link2Icon, MailPlusIcon, PackageIcon, PlusIcon } from "lucide-react";

import { AgencyDialog } from "@/components/app/agency/AgencyDialog";
import { ClientAvatar } from "@/components/app/agency/home/AgencyHome";
import { ghostBtn } from "@/components/app/agency/styles";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { SearchInput } from "@/components/ui/field";
import type { AgencyVendorAccount } from "@/lib/api/models/app/agency/AgencyMailboxes";
import type { AgencyOverviewClient } from "@/lib/api/models/app/agency/AgencyHome";
import { vendorLabel } from "@/lib/api/models/app/emails/MailboxSources";
import { cn } from "@/lib/utils";

export type AddMailboxesChoice =
    | { how: "vendor"; orgId: string; accountId?: string }
    | { how: "file"; orgId: string }
    | { how: "single"; orgId: string }
    | { how: "link"; orgId: string };

function Option({
    icon,
    title,
    body,
    onClick,
    disabled,
}: {
    icon: React.ReactNode;
    title: string;
    body: string;
    onClick: () => void;
    disabled?: boolean;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            disabled={disabled}
            className="w-full px-3 py-2.5 rounded-md border border-slate-200 hover:border-slate-300 hover:bg-slate-50/80 flex items-center gap-3 text-left transition-colors disabled:opacity-60"
        >
            <span className="size-7 rounded-md bg-slate-100 text-slate-600 flex items-center justify-center shrink-0">{icon}</span>
            <span className="min-w-0 flex-1">
                <span className="block text-[12.5px] font-medium text-slate-900">{title}</span>
                <span className="block text-[11.5px] leading-snug text-slate-500">{body}</span>
            </span>
            <ChevronRightIcon className="w-3.5 h-3.5 text-slate-300 shrink-0" />
        </button>
    );
}

export default function AddMailboxesDialog({
    open,
    onClose,
    clients,
    accounts,
    presetAccountId,
    busy,
    onChoose,
}: {
    open: boolean;
    onClose: () => void;
    clients: AgencyOverviewClient[];
    accounts: AgencyVendorAccount[];
    /** Set when the dialog was opened from one vendor account: the client is all that is left to pick. */
    presetAccountId?: string;
    busy: boolean;
    onChoose: (choice: AddMailboxesChoice) => void;
}) {
    const [orgId, setOrgId] = React.useState<string | null>(null);
    const [search, setSearch] = React.useState("");
    // Reset when it opens or is opened for another account; a refetch of the clients keeps the step it is on.
    const latest = React.useRef(clients);
    latest.current = clients;
    React.useEffect(() => {
        if (!open) return;
        const list = latest.current;
        // One client needs no picking.
        setOrgId(!presetAccountId && list.length === 1 && list[0].manage_mailboxes ? list[0].organization_id : null);
        setSearch("");
    }, [open, presetAccountId]);

    // A client that left the list, or that the caller can no longer add to, is not a choice any more.
    const client = clients.find((c) => c.organization_id === orgId && c.manage_mailboxes);
    const preset = accounts.find((a) => a.id === presetAccountId);
    const shown = clients.filter((c) => c.name.toLowerCase().includes(search.trim().toLowerCase()));
    const step = client ? "how" : "client";

    const pickClient = (id: string) => {
        if (preset) onChoose({ how: "vendor", orgId: id, accountId: preset.id });
        else setOrgId(id);
    };

    return (
        <AgencyDialog
            open={open}
            onClose={onClose}
            eyebrow="Mailboxes"
            title={preset ? `Add mailboxes from ${preset.label}` : client ? `Add mailboxes to ${client.name}` : "Add mailboxes"}
            icon={<PlusIcon className="w-3 h-3" />}
            footer={
                <>
                    {step === "how" && clients.length > 1 && (
                        <button type="button" onClick={() => setOrgId(null)} className={cn(ghostBtn, "inline-flex items-center gap-1.5")}>
                            <ArrowLeftIcon className="w-3 h-3" />
                            Another client
                        </button>
                    )}
                    <button type="button" onClick={onClose} className={cn(ghostBtn, "ml-auto")}>
                        Cancel
                    </button>
                </>
            }
        >
            <div className="overflow-hidden">
                <AnimatePresence mode="wait" initial={false}>
                    <motion.div
                        key={step}
                        initial={{ x: step === "how" ? 24 : -24, opacity: 0 }}
                        animate={{ x: 0, opacity: 1 }}
                        exit={{ x: step === "how" ? -24 : 24, opacity: 0 }}
                        transition={{ duration: 0.14 }}
                        className="px-4 py-4"
                    >
                        {step === "client" ? (
                            <div className="space-y-2.5">
                                <p className="text-[12px] text-slate-600">
                                    {preset ? "Which client are these mailboxes for?" : "Which client are you adding mailboxes for?"}
                                </p>
                                {clients.length > 6 && <SearchInput value={search} onChange={setSearch} placeholder="Search clients…" autoFocus />}
                                <div className="max-h-[46vh] overflow-auto -mx-1 px-1 space-y-1">
                                    {shown.map((c) => {
                                        // Adding mailboxes needs the permission to manage them there, not only access to the workspace.
                                        const limited = !c.manage_mailboxes;
                                        return (
                                            <button
                                                key={c.organization_id}
                                                type="button"
                                                disabled={limited || busy}
                                                onClick={() => pickClient(c.organization_id)}
                                                title={limited ? "You cannot manage mailboxes in this client's workspace." : undefined}
                                                className="w-full h-10 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 hover:bg-slate-50/80 flex items-center gap-2.5 text-left transition-colors disabled:opacity-50 disabled:hover:bg-transparent disabled:hover:border-slate-200"
                                            >
                                                <ClientAvatar name={c.name} avatar={c.avatar_url} />
                                                <span className="flex-1 min-w-0 truncate text-[12.5px] text-slate-900">{c.name}</span>
                                                <span className="text-[11.5px] tabular-nums text-slate-500 shrink-0">
                                                    {limited ? "No mailbox access" : `${c.mailboxes?.total ?? 0} mailboxes`}
                                                </span>
                                            </button>
                                        );
                                    })}
                                    {shown.length === 0 && <p className="py-6 text-center text-[12px] text-slate-500">No client matches.</p>}
                                </div>
                            </div>
                        ) : (
                            client && (
                                <div className="space-y-2">
                                    {accounts.map((a) => (
                                        <Option
                                            key={a.id}
                                            icon={<ProviderLogo id={a.vendor} size="sm" framed={false} title="" />}
                                            title={`From ${a.label}`}
                                            body={`Pick mailboxes from your agency's ${vendorLabel(a.vendor)} account. No key to paste again.`}
                                            disabled={busy || a.status === "invalid"}
                                            onClick={() => onChoose({ how: "vendor", orgId: client.organization_id, accountId: a.id })}
                                        />
                                    ))}
                                    <Option
                                        icon={<PackageIcon className="w-3.5 h-3.5" />}
                                        title={accounts.length ? "From another vendor account" : "From an inbox vendor"}
                                        body="Zapmail, InboxKit, Mailforge and others: paste an API key and pick the mailboxes."
                                        disabled={busy}
                                        onClick={() => onChoose({ how: "vendor", orgId: client.organization_id })}
                                    />
                                    <Option
                                        icon={<FileSpreadsheetIcon className="w-3.5 h-3.5" />}
                                        title="Import a file or a list"
                                        body="A CSV or spreadsheet from a vendor or another tool, or addresses and passwords pasted in."
                                        disabled={busy}
                                        onClick={() => onChoose({ how: "file", orgId: client.organization_id })}
                                    />
                                    <Option
                                        icon={<MailPlusIcon className="w-3.5 h-3.5" />}
                                        title="Connect one yourself"
                                        body="Google or Microsoft sign-in, or IMAP and SMTP details, one mailbox at a time."
                                        disabled={busy}
                                        onClick={() => onChoose({ how: "single", orgId: client.organization_id })}
                                    />
                                    <Option
                                        icon={<Link2Icon className="w-3.5 h-3.5" />}
                                        title="Let the client do it"
                                        body="Send a link. The client connects its own mailboxes with no account and no password shared with you."
                                        disabled={busy}
                                        onClick={() => onChoose({ how: "link", orgId: client.organization_id })}
                                    />
                                </div>
                            )
                        )}
                    </motion.div>
                </AnimatePresence>
            </div>
        </AgencyDialog>
    );
}
