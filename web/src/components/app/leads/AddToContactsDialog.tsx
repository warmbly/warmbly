import { useRef, useState } from "react";
import toast from "react-hot-toast/headless";
import { Link } from "@tanstack/react-router";
import { ArrowRightIcon, BuildingIcon, FolderTreeIcon, Loader2Icon, type LucideIcon, MegaphoneIcon, TagsIcon, UserPlusIcon, UsersIcon } from "lucide-react";
import CategoryPicker from "@/components/app/contacts/CategoryPicker";
import { CampaignMultiPicker, SegmentMultiPicker } from "@/components/app/segments/SegmentPickers";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { Checkbox } from "@/components/ui/checkbox";
import type { AppError } from "@/lib/api/client/normalizeError";
import { useAddLeadsToContacts } from "@/lib/api/hooks/app/leads/useLeads";
import type { AddLeadsToContactsResult, LeadContactsWho, LeadSelection } from "@/lib/api/models/app/leads/Lead";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import LeadDialog from "./LeadDialog";

const WHO: { id: LeadContactsWho; label: string; hint: string; icon: LucideIcon }[] = [
    { id: "business", label: "The business", hint: "Each business's own address", icon: BuildingIcon },
    { id: "people", label: "The people", hint: "Owners, managers and staff found there", icon: UsersIcon },
    { id: "both", label: "Both", hint: "People first, then the business", icon: UserPlusIcon },
];

export default function AddToContactsDialog({
    open,
    onClose,
    selection,
    count,
    suggestedSegment,
}: {
    open: boolean;
    onClose: () => void;
    selection: LeadSelection;
    count: number;
    // Offered as the name of a new segment, usually the search's own name.
    suggestedSegment?: string;
}) {
    const add = useAddLeadsToContacts();
    const [who, setWho] = useState<LeadContactsWho>("business");
    const [segmentIds, setSegmentIds] = useState<string[]>([]);
    const [categoryIds, setCategoryIds] = useState<string[]>([]);
    const [campaignIds, setCampaignIds] = useState<string[]>([]);
    const [skipActive, setSkipActive] = useState(true);
    const [verifiedOnly, setVerifiedOnly] = useState(false);
    const [includeGuessed, setIncludeGuessed] = useState(false);
    const [keepExisting, setKeepExisting] = useState(false);
    const [result, setResult] = useState<AddLeadsToContactsResult | null>(null);

    // Bumped on close, so an import that finishes after it does not show its result.
    const attempt = useRef(0);
    const close = () => {
        attempt.current++;
        setResult(null);
        onClose();
    };

    const submit = async () => {
        const mine = ++attempt.current;
        try {
            const res = await add.mutateAsync({
                ...selection,
                who,
                segment_ids: segmentIds,
                category_ids: categoryIds,
                campaign_ids: campaignIds,
                skip_active_in_other_campaigns: campaignIds.length > 0 && skipActive,
                verified_only: verifiedOnly,
                include_guessed: includeGuessed,
                keep_existing: keepExisting,
            });
            if (mine === attempt.current) setResult(res);
            const n = res.imported + res.updated;
            if (n > 0) toast.success(`${n.toLocaleString()} contact${n === 1 ? "" : "s"} added`);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    return (
        <LeadDialog
            open={open}
            onClose={close}
            width={600}
            icon={<UserPlusIcon className="size-3" />}
            eyebrow="Add to contacts"
            title={`${count.toLocaleString()} lead${count === 1 ? "" : "s"}`}
            footer={
                result ? (
                    <>
                        <Link to="/app/contacts" onClick={close} className="h-7 px-3 rounded-md text-[12px] text-slate-600 hover:text-slate-900 inline-flex items-center gap-1">
                            Open contacts
                            <ArrowRightIcon className="size-3" />
                        </Link>
                        <button type="button" onClick={close} className="h-7 px-3 rounded-md bg-slate-900 text-white text-[12px] font-medium">
                            Done
                        </button>
                    </>
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
                            {add.isPending ? <Loader2Icon className="size-3.5 animate-spin" /> : <UserPlusIcon className="size-3.5" />}
                            Add to contacts
                        </button>
                    </>
                )
            }
        >
            {result ? (
                <Outcome result={result} who={who} keepExisting={keepExisting} />
            ) : (
                <div className="px-4 py-4 space-y-4">
                    <section>
                        <SectionLabel>Who to add</SectionLabel>
                        <div className="grid grid-cols-3 gap-2">
                            {WHO.map((w) => {
                                const on = who === w.id;
                                return (
                                    <button
                                        key={w.id}
                                        type="button"
                                        onClick={() => setWho(w.id)}
                                        aria-pressed={on}
                                        className={cn(
                                            "rounded-md border px-2.5 py-2 text-left transition-colors",
                                            on ? "border-sky-300 bg-sky-50/70 ring-2 ring-sky-100" : "border-slate-200 hover:border-slate-300",
                                        )}
                                    >
                                        <w.icon className={cn("size-3.5", on ? "text-sky-600" : "text-slate-400")} />
                                        <div className={cn("mt-1 text-[12px] font-medium", on ? "text-sky-900" : "text-slate-900")}>{w.label}</div>
                                        <div className="text-[10.5px] leading-snug text-slate-500">{w.hint}</div>
                                    </button>
                                );
                            })}
                        </div>
                        <p className="mt-2 text-[11.5px] leading-relaxed text-slate-500">
                            {who === "business"
                                ? "The business name becomes the company, with its website, address, rating and socials as fields."
                                : "Each person comes in with their name, title and LinkedIn, and the business as their company."}{" "}
                            Leads without an email stay in the finder.
                        </p>
                    </section>

                    <section className="rounded-lg border border-slate-200 divide-y divide-slate-100">
                        <div className="px-4 pt-3 pb-2">
                            <h2 className="text-[12.5px] font-medium text-slate-900">Add them to</h2>
                            <p className="text-[11.5px] text-slate-500">Optional. Every contact made from this selection gets these.</p>
                        </div>
                        <Row icon={FolderTreeIcon} label="Segments" hint="A list you can send a campaign to">
                            <SegmentMultiPicker value={segmentIds} onChange={setSegmentIds} allowCreate suggestedName={suggestedSegment} placeholder="Pick or create a segment…" />
                        </Row>
                        <Row icon={TagsIcon} label="Labels" hint="To filter and segment by">
                            <CategoryPicker value={categoryIds} onChange={setCategoryIds} />
                        </Row>
                        <Row icon={MegaphoneIcon} label="Campaigns" hint="Enrolled as leads; an active campaign starts emailing them">
                            <CampaignMultiPicker value={campaignIds} onChange={setCampaignIds} />
                            {campaignIds.length > 0 && (
                                <label className="mt-2 flex items-start gap-1.5 text-[11.5px] text-slate-700 cursor-pointer select-none">
                                    <Checkbox tone="slate" className="mt-px" checked={skipActive} onChange={(e) => setSkipActive(e.target.checked)} />
                                    <span>
                                        Skip contacts already running in another campaign
                                        <span className="block text-slate-500">They are still added, just not enrolled while another campaign is emailing them.</span>
                                    </span>
                                </label>
                            )}
                        </Row>
                    </section>

                    <section className="rounded-lg border border-slate-200 divide-y divide-slate-100">
                        <Option title="Only verified addresses" hint="Leave out every address that did not check as valid.">
                            <Toggle value={verifiedOnly} onChange={setVerifiedOnly} ariaLabel="Only verified addresses" />
                        </Option>
                        {who !== "business" && !verifiedOnly && (
                            <Option title="Include unconfirmed guesses" hint="Addresses worked out from a name that the mail server could not confirm. They can bounce.">
                                <Toggle value={includeGuessed} onChange={setIncludeGuessed} ariaLabel="Include unconfirmed guesses" />
                            </Option>
                        )}
                        <Option
                            title="Contacts you already have"
                            hint={keepExisting ? "Left as they are. They still get the segments, labels and campaigns above." : "Empty details are filled in from the lead. Nothing they already have is erased."}
                        >
                            <div className="inline-flex shrink-0 rounded-md border border-slate-200 p-0.5">
                                {(
                                    [
                                        [false, "Update details"],
                                        [true, "Leave as is"],
                                    ] as const
                                ).map(([keep, label]) => (
                                    <button
                                        key={label}
                                        type="button"
                                        aria-pressed={keepExisting === keep}
                                        onClick={() => setKeepExisting(keep)}
                                        className={cn("h-6 px-2 rounded text-[11.5px] whitespace-nowrap", keepExisting === keep ? "bg-slate-100 text-slate-900 font-medium" : "text-slate-500 hover:text-slate-900")}
                                    >
                                        {label}
                                    </button>
                                ))}
                            </div>
                        </Option>
                    </section>
                </div>
            )}
        </LeadDialog>
    );
}

function Outcome({ result, who, keepExisting }: { result: AddLeadsToContactsResult; who: LeadContactsWho; keepExisting: boolean }) {
    const added = result.imported + result.updated;
    return (
        <div className="px-4 py-4">
            <div className="flex items-center gap-3 rounded-lg border border-emerald-200 bg-emerald-50/60 px-3.5 py-3">
                <div className="size-8 rounded-full bg-emerald-100 text-emerald-700 inline-flex items-center justify-center">
                    <UserPlusIcon className="size-4" />
                </div>
                <div>
                    <div className="text-[13px] font-semibold text-slate-900">
                        {added.toLocaleString()} contact{added === 1 ? "" : "s"} added
                    </div>
                    <div className="text-[11.5px] text-slate-600">
                        {result.imported.toLocaleString()} new, {result.updated.toLocaleString()} updated
                    </div>
                </div>
            </div>
            <dl className="mt-3 grid grid-cols-2 sm:grid-cols-3 gap-2">
                <Stat label="No email yet" value={result.no_email} />
                <Stat label="Not verified" value={result.unverified} />
                {who !== "business" && <Stat label="Unconfirmed guesses left out" value={result.guessed} />}
                <Stat label="Suppressed" value={result.suppressed} />
                <Stat label={keepExisting ? "Already contacts, left as is" : "Skipped"} value={result.skipped} />
                {result.failed > 0 && <Stat label="Failed" value={result.failed} tone="text-rose-700" />}
            </dl>
            {result.no_email > 0 && (
                <p className="mt-3 text-[11.5px] text-slate-500">Anyone without an email stays in the finder. Run Find people and emails on them, then add them again.</p>
            )}
        </div>
    );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
    return <div className="mb-1.5 text-[10px] uppercase tracking-[0.14em] font-medium text-slate-400">{children}</div>;
}

function Row({ icon: Icon, label, hint, children }: { icon: LucideIcon; label: string; hint: string; children: React.ReactNode }) {
    return (
        <div className="px-4 py-3 grid grid-cols-1 sm:grid-cols-[160px_minmax(0,1fr)] gap-x-4 gap-y-1.5 items-start">
            <div className="flex items-start gap-2 pt-1">
                <Icon className="size-3.5 text-slate-400 mt-px shrink-0" />
                <div className="min-w-0">
                    <p className="text-[12px] font-medium text-slate-800">{label}</p>
                    <p className="text-[11px] text-slate-400 leading-snug">{hint}</p>
                </div>
            </div>
            <div className="min-w-0">{children}</div>
        </div>
    );
}

function Option({ title, hint, children }: { title: string; hint: string; children: React.ReactNode }) {
    return (
        <div className="flex items-center justify-between gap-3 px-4 py-2.5">
            <div className="min-w-0">
                <div className="text-[12px] font-medium text-slate-800">{title}</div>
                <div className="text-[11px] text-slate-500">{hint}</div>
            </div>
            {children}
        </div>
    );
}

function Stat({ label, value, tone }: { label: string; value: number; tone?: string }) {
    return (
        <div className="rounded-md border border-slate-200 px-3 py-2">
            <dt className="text-[11px] text-slate-500">{label}</dt>
            <dd className={cn("text-[15px] font-semibold tabular-nums", tone ?? "text-slate-900")}>{value.toLocaleString()}</dd>
        </div>
    );
}
