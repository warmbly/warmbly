// Connects an inbox vendor account to the agency, or replaces the key of one it already holds.

import React from "react";
import { ExternalLinkIcon, KeyRoundIcon, Loader2Icon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { AgencyDialog } from "@/components/app/agency/AgencyDialog";
import { ghostBtn, primaryBtn } from "@/components/app/agency/styles";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { Label, TextInput } from "@/components/ui/field";
import { useSaveAgencyVendor } from "@/lib/api/hooks/app/agency/useAgencyMailboxes";
import type { AgencyVendorAccount } from "@/lib/api/models/app/agency/AgencyMailboxes";
import type { VendorCatalogEntry } from "@/lib/api/models/app/emails/MailboxSources";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

export default function VendorAccountDialog({
    open,
    onClose,
    agencyId,
    catalog,
    account,
}: {
    open: boolean;
    onClose: () => void;
    agencyId: string;
    catalog: VendorCatalogEntry[];
    /** The account whose name or key is being changed; absent to connect a new one. */
    account?: AgencyVendorAccount | null;
}) {
    const save = useSaveAgencyVendor(agencyId);
    const [vendorId, setVendorId] = React.useState("");
    const [label, setLabel] = React.useState("");
    const [fields, setFields] = React.useState<Record<string, string>>({});

    // Reset when it opens or moves to another account; a refetch of the same account keeps what was typed.
    const accountId = account?.id;
    const start = React.useRef(account);
    start.current = account;
    // One key per opening and per changed form, so a retry of the same submission connects one account.
    const attempt = React.useRef("");
    React.useEffect(() => {
        attempt.current = crypto.randomUUID();
    }, [open, accountId, vendorId, fields]);
    React.useEffect(() => {
        if (!open) return;
        setVendorId(start.current?.vendor ?? "");
        setLabel(start.current?.label ?? "");
        setFields({});
    }, [open, accountId]);

    // A save in flight belongs to this dialog: it cannot be closed, and reopened for another account, under it.
    const close = () => {
        if (!save.isPending) onClose();
    };

    const vendor = catalog.find((v) => v.id === vendorId);
    const typed = Object.values(fields).some((v) => v.trim() !== "");
    const missing = vendor?.fields.find((f) => f.required && !(fields[f.key] ?? "").trim());
    // A new account needs its key; an existing one keeps it unless a new one is typed.
    const problem = !vendor
        ? "Pick the vendor you bought the mailboxes from."
        : (!account || typed) && missing
          ? `Enter the ${missing.label.toLowerCase()}.`
          : null;

    const submit = async () => {
        if (problem || !vendor) return;
        try {
            await save.mutateAsync({
                id: account?.id,
                idempotencyKey: attempt.current,
                input: { vendor: vendor.id, label: label.trim() || undefined, fields: !account || typed ? fields : undefined },
            });
            toast.success(account ? (typed ? "Key updated for every client that uses it" : "Saved") : `${vendor.label} connected`);
            onClose();
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    return (
        <AgencyDialog
            open={open}
            onClose={close}
            eyebrow="Inbox vendor"
            title={account ? `Update ${account.label}` : "Connect a vendor account"}
            icon={<KeyRoundIcon className="w-3 h-3" />}
            footer={
                <>
                    {problem && <span className="text-[11.5px] text-slate-500 truncate">{problem}</span>}
                    <button type="button" onClick={close} disabled={save.isPending} className={cn(ghostBtn, "ml-auto disabled:opacity-60")}>
                        Cancel
                    </button>
                    <button type="button" onClick={() => void submit()} disabled={!!problem || save.isPending} className={primaryBtn}>
                        {save.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        {save.isPending ? "Checking with the vendor…" : account ? "Save" : "Connect"}
                    </button>
                </>
            }
        >
            <form
                className="px-4 py-4 space-y-4 max-h-[70vh] overflow-auto"
                onSubmit={(e) => {
                    e.preventDefault();
                    void submit();
                }}
            >
                {!account && (
                    <div>
                        <Label>Vendor</Label>
                        <div role="radiogroup" aria-label="Vendor" className="grid grid-cols-2 gap-1.5">
                            {catalog.map((v) => (
                                <button
                                    key={v.id}
                                    type="button"
                                    role="radio"
                                    aria-checked={vendorId === v.id}
                                    onClick={() => {
                                        setVendorId(v.id);
                                        setFields({});
                                    }}
                                    className={cn(
                                        "h-9 px-2 rounded-md border text-[12.5px] inline-flex items-center gap-2 text-left transition-colors",
                                        vendorId === v.id ? "border-sky-400 bg-sky-50 text-sky-800" : "border-slate-200 text-slate-700 hover:border-slate-300",
                                    )}
                                >
                                    <ProviderLogo id={v.id} size="sm" title="" />
                                    <span className="truncate">{v.label}</span>
                                </button>
                            ))}
                        </div>
                    </div>
                )}
                {vendor && (
                    <>
                        {vendor.fields.map((f) => (
                            <div key={f.key}>
                                <Label htmlFor={`agency-vendor-${f.key}`}>{f.label}</Label>
                                <TextInput
                                    id={`agency-vendor-${f.key}`}
                                    value={fields[f.key] ?? ""}
                                    onChange={(v) => setFields((cur) => ({ ...cur, [f.key]: v }))}
                                    type={f.secret ? "password" : "text"}
                                    autoComplete="off"
                                    placeholder={account ? "Leave empty to keep the current one" : undefined}
                                />
                                {f.help && <p className="mt-1 text-[11.5px] leading-snug text-slate-500">{f.help}</p>}
                            </div>
                        ))}
                        <div>
                            <Label htmlFor="agency-vendor-label">Name, for your team</Label>
                            <TextInput id="agency-vendor-label" value={label} onChange={setLabel} placeholder={vendor.label} maxLength={80} />
                        </div>
                        <p className="text-[11.5px] leading-relaxed text-slate-500">
                            The key is checked with {vendor.label}, stored encrypted and never shown again. It is used only to list and connect
                            the mailboxes you pick for a client.{" "}
                            {vendor.key_help_url && (
                                <a href={vendor.key_help_url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-0.5 text-sky-700 hover:underline">
                                    Where to find it <ExternalLinkIcon className="w-3 h-3" />
                                </a>
                            )}
                        </p>
                    </>
                )}
            </form>
        </AgencyDialog>
    );
}
