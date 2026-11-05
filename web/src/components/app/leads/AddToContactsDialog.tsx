import { useState } from "react";
import toast from "react-hot-toast/headless";
import { Loader2Icon, UserPlusIcon } from "lucide-react";
import CampaignPicker from "@/components/app/campaigns/CampaignPicker";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import type { AppError } from "@/lib/api/client/normalizeError";
import { useAddLeadsToContacts } from "@/lib/api/hooks/app/leads/useLeads";
import type { AddLeadsToContactsResult, LeadContactsWho, LeadSelection } from "@/lib/api/models/app/leads/Lead";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import LeadDialog from "./LeadDialog";

const WHO: { id: LeadContactsWho; label: string; hint: string }[] = [
    { id: "people", label: "The people", hint: "Owners, managers and staff found at each business" },
    { id: "business", label: "The business", hint: "Each business's own address" },
    { id: "both", label: "Both", hint: "People first, then the business address" },
];

export default function AddToContactsDialog({
    open,
    onClose,
    selection,
    count,
}: {
    open: boolean;
    onClose: () => void;
    selection: LeadSelection;
    count: number;
}) {
    const add = useAddLeadsToContacts();
    const [campaign, setCampaign] = useState<{ id: string | null; name: string }>({ id: null, name: "" });
    const [verifiedOnly, setVerifiedOnly] = useState(false);
    const [who, setWho] = useState<LeadContactsWho>("business");
    const [includeGuessed, setIncludeGuessed] = useState(false);
    const [result, setResult] = useState<AddLeadsToContactsResult | null>(null);

    const close = () => {
        setResult(null);
        onClose();
    };

    const submit = async () => {
        try {
            const res = await add.mutateAsync({
                ...selection,
                campaign_ids: campaign.id ? [campaign.id] : [],
                verified_only: verifiedOnly,
                who,
                include_guessed: includeGuessed,
            });
            setResult(res);
            const n = res.imported + res.updated;
            if (n > 0) toast.success(`${n.toLocaleString()} contact${n === 1 ? "" : "s"} added${campaign.id ? ` to ${campaign.name}` : ""}`);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    return (
        <LeadDialog
            open={open}
            onClose={close}
            icon={<UserPlusIcon className="size-3" />}
            eyebrow="Add to contacts"
            title={`${count.toLocaleString()} lead${count === 1 ? "" : "s"}`}
            footer={
                result ? (
                    <button type="button" onClick={close} className="h-7 px-3 rounded-md bg-slate-900 text-white text-[12px] font-medium">
                        Done
                    </button>
                ) : (
                    <>
                        <button type="button" onClick={close} className="h-7 px-3 rounded-md text-[12px] text-slate-600 hover:text-slate-900">
                            Cancel
                        </button>
                        <button
                            type="button"
                            onClick={submit}
                            disabled={add.isPending}
                            className="h-7 px-3 rounded-md bg-sky-600 text-white text-[12px] font-medium inline-flex items-center gap-1.5 hover:bg-sky-700 disabled:opacity-60"
                        >
                            {add.isPending && <Loader2Icon className="size-3.5 animate-spin" />}
                            Add to contacts
                        </button>
                    </>
                )
            }
        >
            {result ? (
                <div className="px-4 py-4">
                    <dl className="grid grid-cols-2 gap-2">
                        <Stat label="New contacts" value={result.imported} tone="text-emerald-700" />
                        <Stat label="Updated" value={result.updated} />
                        <Stat label="No email yet" value={result.no_email} />
                        <Stat label="Not verified" value={result.unverified} />
                        {who !== "business" && <Stat label="Unconfirmed guesses left out" value={result.guessed} />}
                        <Stat label="Suppressed" value={result.suppressed} />
                        <Stat label="Skipped or failed" value={result.skipped + result.failed} />
                    </dl>
                    {result.no_email > 0 && (
                        <p className="mt-3 text-[11.5px] text-slate-500">
                            Anyone without an email stays in the finder. Run Find people and emails on them, then add them again.
                        </p>
                    )}
                </div>
            ) : (
                <div className="px-4 py-4 space-y-4">
                    <div>
                        <div className="mb-1.5 text-[11.5px] text-slate-500">Who to add</div>
                        <div className="grid grid-cols-3 gap-2">
                            {WHO.map((w) => (
                                <button
                                    key={w.id}
                                    type="button"
                                    onClick={() => setWho(w.id)}
                                    aria-pressed={who === w.id}
                                    className={cn(
                                        "rounded-md border px-2.5 py-2 text-left transition-colors",
                                        who === w.id ? "border-sky-300 bg-sky-50" : "border-slate-200 hover:border-slate-300",
                                    )}
                                >
                                    <div className={cn("text-[12px] font-medium", who === w.id ? "text-sky-800" : "text-slate-900")}>{w.label}</div>
                                    <div className="text-[10.5px] leading-snug text-slate-500">{w.hint}</div>
                                </button>
                            ))}
                        </div>
                    </div>
                    <p className="text-[12px] leading-relaxed text-slate-600">
                        {who === "business"
                            ? "Each lead with an email becomes a contact, with the business name as the company and its website, address, rating and socials as fields."
                            : "Each person with an email becomes a contact with their name, title and LinkedIn, and the business as their company."}{" "}
                        Someone already in your contacts is updated, never duplicated. Suppressed addresses are left out.
                    </p>
                    <div>
                        <div className="mb-1.5 text-[11.5px] text-slate-500">Also add them to a campaign</div>
                        <CampaignPicker campaignId={campaign.id} campaignName={campaign.name} onChange={(id, name) => setCampaign({ id, name })} noneLabel="No campaign" />
                    </div>
                    <div className="flex items-center justify-between gap-3 rounded-md border border-slate-200 px-3 py-2">
                        <div>
                            <div className="text-[12px] font-medium text-slate-800">Only verified addresses</div>
                            <div className="text-[11px] text-slate-500">Leave out every address that did not check as valid.</div>
                        </div>
                        <Toggle value={verifiedOnly} onChange={setVerifiedOnly} ariaLabel="Only verified addresses" />
                    </div>
                    {who !== "business" && !verifiedOnly && (
                        <div className="flex items-center justify-between gap-3 rounded-md border border-slate-200 px-3 py-2">
                            <div>
                                <div className="text-[12px] font-medium text-slate-800">Include unconfirmed guesses</div>
                                <div className="text-[11px] text-slate-500">Addresses worked out from a name that the mail server could not confirm. They can bounce.</div>
                            </div>
                            <Toggle value={includeGuessed} onChange={setIncludeGuessed} ariaLabel="Include unconfirmed guesses" />
                        </div>
                    )}
                </div>
            )}
        </LeadDialog>
    );
}

function Stat({ label, value, tone }: { label: string; value: number; tone?: string }) {
    return (
        <div className="rounded-md border border-slate-200 px-3 py-2">
            <dt className="text-[11px] text-slate-500">{label}</dt>
            <dd className={`text-[16px] font-semibold tabular-nums ${tone ?? "text-slate-900"}`}>{value.toLocaleString()}</dd>
        </div>
    );
}
