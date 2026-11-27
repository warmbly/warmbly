// The preview panel: click a row and the right side shows the whole person without leaving the list. Not modal.
import { AnimatePresence, motion } from "framer-motion";
import { BriefcaseIcon, Building2Icon, CheckIcon, ExternalLinkIcon, Loader2Icon, MapPinIcon, SearchIcon, UserPlusIcon, UserRoundIcon, XIcon } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Avatar } from "@/components/app/social/parts";
import { profileUrl } from "@/lib/social/channels";
import { useLinkedInProfile } from "@/lib/api/hooks/app/social/useSocial";
import type { FinderHit } from "@/lib/api/models/app/social/Social";
import { socialErrorMessage } from "@/lib/social/errors";
import { cn } from "@/lib/utils";
import { Badge, OutreachBadge, ReachMeter, ReachReasons, signalsOf, Spotlight, TONE } from "./bits";
import { apiAccount, degreeLabel, isSelectable, tenure } from "./state";
import { campaignsOf, contactOf, coverageLine, companyContacts } from "./view";

export function PreviewPanel({
    hit,
    accountId,
    selected,
    canSelect,
    onClose,
    onToggle,
    onAdd,
    onOpenContact,
    onFindAtCompany,
}: {
    hit: FinderHit | null;
    accountId: string;
    selected: boolean;
    canSelect: boolean;
    onClose: () => void;
    onToggle: () => void;
    onAdd: () => void;
    onOpenContact: (id: string) => void;
    onFindAtCompany?: () => void;
}) {
    useEffect(() => {
        if (!hit) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape" || e.defaultPrevented) return;
            if (document.querySelector("[data-floating], [role='alertdialog'], [aria-modal='true']")) return;
            onClose();
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [hit, onClose]);
    const p = hit?.person;
    return (
        <AnimatePresence>
            {hit && p && (
                <motion.aside
                    key="preview"
                    initial={{ x: 24, opacity: 0 }}
                    animate={{ x: 0, opacity: 1 }}
                    exit={{ x: 24, opacity: 0 }}
                    transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                    aria-label={`Preview: ${p.full_name}`}
                    className="fixed inset-y-0 right-0 z-[60] flex w-full flex-col border-l border-slate-200 bg-white shadow-[0_0_32px_-8px_rgba(15,23,42,0.18)] md:w-[26rem]"
                >
                    <Body key={hit.key} hit={hit} accountId={accountId} selected={selected} canSelect={canSelect} onClose={onClose} onToggle={onToggle} onAdd={onAdd} onOpenContact={onOpenContact} onFindAtCompany={onFindAtCompany} />
                </motion.aside>
            )}
        </AnimatePresence>
    );
}

function Body({
    hit,
    accountId,
    selected,
    canSelect,
    onClose,
    onToggle,
    onAdd,
    onOpenContact,
    onFindAtCompany,
}: {
    hit: FinderHit;
    accountId: string;
    selected: boolean;
    canSelect: boolean;
    onClose: () => void;
    onToggle: () => void;
    onAdd: () => void;
    onOpenContact: (id: string) => void;
    onFindAtCompany?: () => void;
}) {
    const p = hit.person!;
    const ins = hit.insights;
    const degree = degreeLabel(p.network_distance);
    const inRole = tenure(ins?.position_started_on);
    const atCompany = tenure(ins?.company_started_on);
    const selectable = isSelectable(hit);
    const signals = signalsOf(hit);
    const contactId = contactOf(hit);
    const campaigns = campaignsOf(hit);
    const href = profileUrl("linkedin", p.profile_url);
    // A full profile read is a profile visit on the account, so it only loads when asked.
    const [wantProfile, setWantProfile] = useState(false);
    const profile = useLinkedInProfile(wantProfile ? p.public_id || p.provider_id || null : null, apiAccount(accountId));
    return (
        <>
            <div className="flex items-start gap-3 border-b border-slate-200 px-4 py-3.5">
                <Avatar src={p.picture_url} name={p.full_name || "LinkedIn member"} size={44} />
                <div className="grid min-w-0 flex-1 gap-0.5">
                    <p className="flex min-w-0 flex-wrap items-center gap-x-1.5 text-[14px] font-semibold text-slate-900">
                        <span className="truncate">{p.full_name || "LinkedIn member"}</span>
                        {degree && <span className="text-[12px] font-normal text-slate-500">· {degree}</span>}
                    </p>
                    {p.headline && <p className="line-clamp-3 text-[12px] text-slate-600">{p.headline}</p>}
                    <div className="mt-1 flex flex-wrap gap-1">
                        {p.premium && <Badge tone={TONE.amber}>Premium</Badge>}
                        {p.open_profile && <Badge tone={TONE.sky}>Open Profile</Badge>}
                        {contactId && <Badge tone={TONE.emerald}>Contact</Badge>}
                        <OutreachBadge hit={hit} />
                    </div>
                </div>
                <button type="button" onClick={onClose} aria-label="Close the preview" className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center">
                    <XIcon className="size-3.5" />
                </button>
            </div>

            <div className="flex flex-wrap gap-1.5 border-b border-slate-200 px-4 py-2.5">
                <button
                    type="button"
                    disabled={!canSelect || !selectable}
                    onClick={onToggle}
                    className={cn(
                        "h-7 px-2.5 rounded-md text-[12px] font-medium inline-flex items-center gap-1.5 disabled:opacity-50",
                        selected ? "bg-sky-600 text-white hover:bg-sky-700" : "border border-slate-200 bg-white text-slate-700 hover:bg-slate-50",
                    )}
                >
                    <CheckIcon className="size-3.5" /> {selected ? "Selected" : "Select"}
                </button>
                <button type="button" disabled={!canSelect || !selectable} onClick={onAdd} className="h-7 px-2.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5 disabled:opacity-50">
                    <UserPlusIcon className="size-3.5" /> Add to contacts
                </button>
                {href && (
                    <a href={href} target="_blank" rel="noreferrer noopener" className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5">
                        <ExternalLinkIcon className="size-3.5" /> LinkedIn
                    </a>
                )}
                {contactId && (
                    <button type="button" onClick={() => onOpenContact(contactId)} className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5">
                        <UserRoundIcon className="size-3.5" /> Open contact
                    </button>
                )}
            </div>

            <div className="min-h-0 flex-1 overflow-y-auto">
                <Section title="Profile">
                    <dl className="grid gap-1.5 text-[12.5px] text-slate-700">
                        {(ins?.current_title || ins?.current_company) && (
                            <Row icon={<BriefcaseIcon />}>
                                {[ins?.current_title, ins?.current_company].filter(Boolean).join(" at ")}
                                {inRole && <span className="text-slate-500"> · {inRole} in role</span>}
                                {atCompany && atCompany !== inRole && <span className="text-slate-500"> · {atCompany} at company</span>}
                            </Row>
                        )}
                        {p.location && <Row icon={<MapPinIcon />}>{p.location}</Row>}
                        {ins?.industry && <Row icon={<Building2Icon />}>{ins.industry}</Row>}
                    </dl>
                    {(signals.length > 0 || (ins?.shared_connections ?? 0) > 0) && (
                        <div className="mt-2.5 flex flex-wrap gap-1.5">
                            {signals.map((s) => (
                                <Spotlight key={s.key} icon={s.icon}>
                                    {s.label}
                                </Spotlight>
                            ))}
                            {(ins?.shared_connections ?? 0) > 0 && <Badge tone={TONE.slate}>{ins!.shared_connections} shared connections</Badge>}
                        </div>
                    )}
                    {!wantProfile ? (
                        <button type="button" onClick={() => setWantProfile(true)} className="mt-3 text-[11.5px] font-medium text-sky-700 hover:underline">
                            Load the full profile (uses one profile visit)
                        </button>
                    ) : profile.isPending ? (
                        <p className="mt-3 inline-flex items-center gap-1.5 text-[11.5px] text-slate-500">
                            <Loader2Icon className="size-3 animate-spin" /> Reading the profile…
                        </p>
                    ) : profile.isError ? (
                        <p className="mt-3 text-[11.5px] text-red-600">{socialErrorMessage(profile.error)}</p>
                    ) : profile.data ? (
                        <div className="mt-3 grid gap-2">
                            {profile.data.summary && <p className="whitespace-pre-line text-[12px] leading-relaxed text-slate-700">{profile.data.summary}</p>}
                            {!!profile.data.experience?.length && (
                                <ul className="grid gap-1">
                                    {profile.data.experience.slice(0, 6).map((x, i) => (
                                        <li key={i} className="text-[12px] text-slate-700">
                                            <span className="font-medium">{x.title}</span>
                                            {x.company && <span> at {x.company}</span>}
                                            {x.started_on && (
                                                <span className="text-slate-400">
                                                    {" "}
                                                    · {x.started_on.slice(0, 7)} to {x.ended_on ? x.ended_on.slice(0, 7) : "now"}
                                                </span>
                                            )}
                                        </li>
                                    ))}
                                </ul>
                            )}
                        </div>
                    ) : null}
                </Section>

                <Section title="Reach">
                    {hit.reach ? (
                        <div className="grid gap-2 text-[12px] text-slate-700">
                            <ReachMeter reach={hit.reach} showLabel />
                            {hit.reach.reasons.length > 0 && <ReachReasons reach={hit.reach} />}
                        </div>
                    ) : (
                        <p className="text-[12px] text-slate-500">No reach signals for this person.</p>
                    )}
                </Section>

                <Section title="In Warmbly">
                    <div className="grid gap-1.5 text-[12px] text-slate-700">
                        <p>{contactId ? "Already a contact in this workspace." : "Not a contact yet."}</p>
                        {campaigns.length > 0 && <p>In {campaigns.map((c) => c.name).join(", ")}.</p>}
                        {ins?.current_company && (
                            <p className="text-slate-500">
                                {ins.current_company}: {coverageLine(companyContacts(hit)).toLowerCase()}.
                            </p>
                        )}
                        {onFindAtCompany && (
                            <button type="button" onClick={onFindAtCompany} className="justify-self-start mt-1 h-7 px-2.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5">
                                <SearchIcon className="size-3.5" /> Find people at {ins?.current_company}
                            </button>
                        )}
                    </div>
                </Section>
            </div>
        </>
    );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
    return (
        <section className="border-b border-slate-100 px-4 py-3.5">
            <h3 className="mb-2 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{title}</h3>
            {children}
        </section>
    );
}

function Row({ icon, children }: { icon: ReactNode; children: ReactNode }) {
    return (
        <div className="flex items-start gap-2">
            <span className="mt-0.5 text-slate-400 [&>svg]:size-3.5">{icon}</span>
            <dd className="min-w-0">{children}</dd>
        </div>
    );
}
