import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast/headless";
import {
    BuildingIcon,
    CheckIcon,
    ClockIcon,
    CopyIcon,
    ExternalLinkIcon,
    FileTextIcon,
    GlobeIcon,
    InfoIcon,
    Loader2Icon,
    LockIcon,
    MailIcon,
    MapIcon,
    MapPinIcon,
    PhoneIcon,
    SparklesIcon,
    StarIcon,
    TagIcon,
    UserPlusIcon,
    UsersIcon,
    XIcon,
} from "lucide-react";
import { Link } from "@tanstack/react-router";
import ResourceViewers from "@/components/app/presence/ResourceViewers";
import ScrollStrip from "@/components/ui/scroll-strip";
import { usePresenceResource } from "@/hooks/PresenceProvider";
import type { AppError } from "@/lib/api/client/normalizeError";
import { useEnrichLeads, useLead, useUpdateLead } from "@/lib/api/hooks/app/leads/useLeads";
import type { Lead, LeadEmail, LeadFinderConfig, LeadStatus } from "@/lib/api/models/app/leads/Lead";
import buildError from "@/lib/helper/buildError";
import timeAgo from "@/lib/helper/timeAgo";
import { cn } from "@/lib/utils";
import PeopleTab from "./PeopleTab";
import { ScoreBar } from "./badges";
import { WebSearchOption, type WebSearchChoice } from "./LeadSelectionBar";
import { unreadDetail, unreadReason } from "./unread";
import { STATUS_LABEL, emailVerdict } from "./verdict";

type Tab = "overview" | "people" | "website" | "notes";

const READER_LABEL: Record<string, string> = {
    jina: "Jina Reader",
    crawl4ai: "Crawl4AI",
    ddgs: "DDGS",
    custom: "a reader service",
};

const SOCIAL_LABEL: Record<string, string> = {
    facebook: "Facebook",
    instagram: "Instagram",
    linkedin: "LinkedIn",
    twitter: "X",
    tiktok: "TikTok",
    youtube: "YouTube",
    pinterest: "Pinterest",
    yelp: "Yelp",
};

export default function LeadDrawer({
    id,
    config,
    webSearch,
    onClose,
    onAddToContacts,
}: {
    id: string | null;
    config: LeadFinderConfig;
    // The web search choice runs started here follow; unset when no search is available.
    webSearch?: WebSearchChoice;
    onClose: () => void;
    onAddToContacts: (id: string) => void;
}) {
    const lead = useLead(id);
    const [tab, setTab] = useState<Tab>("overview");
    usePresenceResource(id ? `lead:${id}` : null, "viewing");

    useEffect(() => {
        if (!id) return;
        const key = (e: KeyboardEvent) => {
            if (e.key !== "Escape" || document.querySelector("[data-floating]") || document.querySelector('[role="dialog"],[role="alertdialog"]')) return;
            onClose();
        };
        window.addEventListener("keydown", key);
        return () => window.removeEventListener("keydown", key);
    }, [id, onClose]);

    return (
        <AnimatePresence>
            {id && (
                <motion.aside
                    key="drawer"
                    initial={{ x: 24, opacity: 0 }}
                    animate={{ x: 0, opacity: 1 }}
                    exit={{ x: 24, opacity: 0 }}
                    transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                    className="absolute inset-y-0 right-0 z-20 flex w-full sm:w-[400px] flex-col border-l border-slate-200 bg-white shadow-[-16px_0_40px_-24px_rgba(15,23,42,0.35)]"
                    aria-label="Lead details"
                >
                    {!lead.data ? (
                        <div className="flex flex-1 items-center justify-center">
                            {lead.isError ? (
                                <p className="text-[12px] text-slate-500">This lead is no longer here.</p>
                            ) : (
                                <Loader2Icon className="size-5 animate-spin text-slate-300" />
                            )}
                        </div>
                    ) : (
                        <Body lead={lead.data} config={config} webSearch={webSearch} tab={tab} onTab={setTab} onClose={onClose} onAddToContacts={onAddToContacts} />
                    )}
                </motion.aside>
            )}
        </AnimatePresence>
    );
}

function Body({
    lead,
    config,
    webSearch,
    tab,
    onTab,
    onClose,
    onAddToContacts,
}: {
    lead: Lead;
    config: LeadFinderConfig;
    webSearch?: WebSearchChoice;
    tab: Tab;
    onTab: (t: Tab) => void;
    onClose: () => void;
    onAddToContacts: (id: string) => void;
}) {
    const update = useUpdateLead();
    const enrich = useEnrichLeads();
    const provider = config.providers.find((p) => p.id === lead.provider)?.name ?? lead.provider;
    const reachable = !!lead.email || (lead.people ?? []).some((p) => p.email && p.email_status !== "suppressed" && p.email_status !== "invalid");
    const setStatus = (status: LeadStatus) =>
        update.mutate({ id: lead.id, patch: { status } }, { onError: (e) => toast.error(buildError(e as unknown as AppError)) });

    const findEmails = async () => {
        try {
            await enrich.mutateAsync({ lead_ids: [lead.id], verify: true, people: true, web_search: webSearch?.on ?? true });
            toast.success(lead.website ? `Reading ${lead.domain || "the website"} for people and contact details` : `Looking for ${lead.name}'s website`);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    return (
        <>
            <header className="shrink-0 px-4 pt-3 pb-2.5 border-b border-slate-200">
                <div className="flex items-start gap-2">
                    <div className="mt-0.5 size-8 shrink-0 rounded-md bg-sky-50 text-sky-700 flex items-center justify-center">
                        <BuildingIcon className="size-4" />
                    </div>
                    <div className="min-w-0 flex-1">
                        <h2 className="truncate text-[14px] font-semibold text-slate-900">{lead.name}</h2>
                        <p className="truncate text-[11.5px] text-slate-500">{[lead.category, lead.city].filter(Boolean).join(" · ") || provider}</p>
                    </div>
                    <ResourceViewers resource={`lead:${lead.id}`} />
                    <button
                        type="button"
                        aria-label="Close"
                        onClick={onClose}
                        className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center"
                    >
                        <XIcon className="size-3.5" />
                    </button>
                </div>
                <div className="mt-2.5 flex items-center gap-1.5">
                    <div className="inline-flex rounded-md border border-slate-200 p-0.5">
                        {(["new", "saved", "contacted", "dismissed"] as LeadStatus[]).map((s) => (
                            <button
                                key={s}
                                type="button"
                                onClick={() => setStatus(s)}
                                className={cn(
                                    "h-6 px-2 rounded text-[11.5px] transition-colors",
                                    lead.status === s ? "bg-sky-50 text-sky-700 font-medium" : "text-slate-500 hover:text-slate-900",
                                )}
                            >
                                {STATUS_LABEL[s]}
                            </button>
                        ))}
                    </div>
                    <div className="ml-auto w-[72px]">
                        <ScoreBar score={lead.score} />
                    </div>
                </div>
                <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
                    <button
                        type="button"
                        onClick={findEmails}
                        disabled={enrich.isPending || lead.enrichment_status === "queued"}
                        className="h-7 px-2.5 rounded-md bg-sky-600 text-white text-[12px] font-medium inline-flex items-center gap-1.5 hover:bg-sky-700 disabled:opacity-60"
                    >
                        {lead.enrichment_status === "queued" ? (
                            <span className="campaign-grid text-white" aria-hidden />
                        ) : enrich.isPending ? (
                            <Loader2Icon className="size-3.5 animate-spin" />
                        ) : (
                            <SparklesIcon className="size-3.5" />
                        )}
                        {lead.enrichment_status === "queued" ? (lead.website ? "Reading website…" : "Looking for a website…") : lead.enriched_at ? "Read again" : "Find people and emails"}
                    </button>
                    {webSearch && <WebSearchOption choice={webSearch} side="bottom" />}
                    {lead.contact_id ? (
                        <Link
                            to="/app/contacts"
                            className="h-7 px-2.5 rounded-md border border-emerald-200 bg-emerald-50 text-emerald-700 text-[12px] font-medium inline-flex items-center gap-1.5"
                        >
                            <CheckIcon className="size-3.5" />
                            In contacts
                        </Link>
                    ) : (
                        <button
                            type="button"
                            onClick={() => onAddToContacts(lead.id)}
                            disabled={!reachable}
                            title={reachable ? undefined : "Find an email first"}
                            className="h-7 px-2.5 rounded-md border border-slate-200 bg-white text-slate-700 text-[12px] font-medium inline-flex items-center gap-1.5 hover:border-slate-300 hover:text-slate-900 disabled:opacity-50"
                        >
                            <UserPlusIcon className="size-3.5" />
                            Add to contacts
                        </button>
                    )}
                    {lead.website && (
                        <a href={lead.website} target="_blank" rel="noopener noreferrer nofollow" className="h-7 px-2 rounded-md border border-slate-200 text-slate-600 hover:text-slate-900 inline-flex items-center gap-1 text-[12px]">
                            <GlobeIcon className="size-3.5" />
                            Site
                        </a>
                    )}
                    {lead.maps_url && (
                        <a href={lead.maps_url} target="_blank" rel="noopener noreferrer nofollow" className="h-7 px-2 rounded-md border border-slate-200 text-slate-600 hover:text-slate-900 inline-flex items-center gap-1 text-[12px]">
                            <MapIcon className="size-3.5" />
                            Map
                        </a>
                    )}
                </div>
            </header>
            <ScrollStrip activeKey={tab} className="shrink-0 border-b border-slate-200" innerClassName="px-3 flex items-center gap-1">
                {(
                    [
                        ["overview", "Overview", InfoIcon],
                        ["people", "People", UsersIcon],
                        ["website", "Website", GlobeIcon],
                        ["notes", "Notes", FileTextIcon],
                    ] as const
                ).map(([key, label, Icon]) => (
                    <button
                        key={key}
                        type="button"
                        data-active={tab === key || undefined}
                        onClick={() => onTab(key)}
                        className={cn("relative h-10 px-2.5 inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap text-[12.5px]", tab === key ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-900")}
                    >
                        <Icon className="size-3.5" />
                        {label}
                        {key === "people" && lead.people_count > 0 && (
                            <span className="rounded-full bg-slate-100 px-1.5 text-[10.5px] font-medium tabular-nums text-slate-600">{lead.people_count}</span>
                        )}
                        {tab === key && <span className="absolute inset-x-2 -bottom-px h-0.5 rounded-full bg-sky-600" />}
                    </button>
                ))}
            </ScrollStrip>
            <div className="flex-1 min-h-0 overflow-y-auto px-4 py-3">
                {tab === "overview" && <Overview lead={lead} provider={provider} onPeople={() => onTab("people")} />}
                {tab === "people" && <PeopleTab lead={lead} onFindPeople={findEmails} finding={enrich.isPending || lead.enrichment_status === "queued"} />}
                {tab === "website" && <Website lead={lead} config={config} />}
                {tab === "notes" && <Notes key={lead.id} lead={lead} />}
            </div>
        </>
    );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
    return (
        <section className="mb-4">
            <h3 className="mb-1.5 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">{title}</h3>
            {children}
        </section>
    );
}

function copy(v: string) {
    navigator.clipboard?.writeText(v).then(
        () => toast.success("Copied"),
        () => toast.error("Could not copy"),
    );
}

function Overview({ lead, provider, onPeople }: { lead: Lead; provider: string; onPeople: () => void }) {
    const socials = Object.entries(lead.socials ?? {});
    const deciders = (lead.people ?? []).filter((p) => p.seniority !== "staff").slice(0, 3);
    return (
        <>
            {deciders.length > 0 && (
                <Section title="Decision makers">
                    <button type="button" onClick={onPeople} className="w-full rounded-md border border-slate-200 px-2.5 py-2 text-left hover:border-slate-300">
                        {deciders.map((p) => (
                            <div key={p.id} className="flex items-center gap-2 py-0.5 text-[12px]">
                                <span className="truncate font-medium text-slate-800">{p.name}</span>
                                {p.title && <span className="truncate text-slate-500">{p.title}</span>}
                                {p.email && p.email_status !== "suppressed" && <MailIcon className="ml-auto size-3 shrink-0 text-emerald-600" aria-label="Has an address" />}
                            </div>
                        ))}
                        <span className="mt-1 block text-[11px] text-sky-700">
                            {lead.people_count > deciders.length ? `See all ${lead.people_count} people` : "See people"}
                        </span>
                    </button>
                </Section>
            )}
            <Section title="Emails">
                {lead.emails.length === 0 ? (
                    <p className="text-[12px] text-slate-500">
                        {lead.enrichment_status === "no_website"
                            ? "The listing has no website, and none could be confirmed from the business's name."
                            : lead.enrichment_status === "done"
                              ? "The website lists no email address."
                              : lead.enrichment_status === "failed"
                                ? `No info available. ${unreadDetail(lead.site)}`
                                : "Not looked for yet. Find people and emails reads the business's own site."}
                    </p>
                ) : (
                    <ul className="space-y-1">
                        {lead.emails.map((e) => {
                            const v = emailVerdict(e);
                            return (
                                <li key={e.email} className="group flex items-center gap-2 rounded-md border border-slate-200 px-2 py-1.5">
                                    <MailIcon className="size-3.5 shrink-0 text-slate-400" />
                                    <div className="min-w-0 flex-1">
                                        <div className="flex items-center gap-1.5">
                                            <span className={cn("truncate text-[12.5px]", e.email === lead.email ? "font-medium text-slate-900" : "text-slate-700")}>{e.email}</span>
                                            {e.email === lead.email && <span className="rounded bg-sky-50 px-1 text-[10px] text-sky-700">Primary</span>}
                                        </div>
                                        <div className="flex items-center gap-1.5 text-[10.5px] text-slate-400">
                                            <span className={cn("inline-flex items-center gap-0.5", v.tone)}>
                                                {v.icon}
                                                {v.label}
                                            </span>
                                            <span>·</span>
                                            <EmailSource email={e} provider={provider} />
                                            {e.domain_match && <span>· On their domain</span>}
                                        </div>
                                    </div>
                                    <button
                                        type="button"
                                        aria-label={`Copy ${e.email}`}
                                        onClick={() => copy(e.email)}
                                        className="size-6 rounded text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100"
                                    >
                                        <CopyIcon className="size-3" />
                                    </button>
                                </li>
                            );
                        })}
                    </ul>
                )}
            </Section>
            <Section title="Details">
                <dl className="space-y-1.5 text-[12px]">
                    {lead.phone && (
                        <Fact icon={<PhoneIcon className="size-3.5" />}>
                            <a href={`tel:${lead.phone}`} className="text-slate-800 hover:text-sky-700">
                                {lead.phone}
                            </a>
                        </Fact>
                    )}
                    {lead.website && (
                        <Fact icon={<GlobeIcon className="size-3.5" />}>
                            <a href={lead.website} target="_blank" rel="noopener noreferrer nofollow" className="inline-flex items-center gap-1 text-slate-800 hover:text-sky-700">
                                {lead.domain || lead.website}
                                <ExternalLinkIcon className="size-3" />
                            </a>
                            {lead.site.found_website && <span className="ml-1.5 text-[11px] text-slate-400">Found from the name, confirmed on the site</span>}
                        </Fact>
                    )}
                    {lead.address && (
                        <Fact icon={<MapPinIcon className="size-3.5" />}>
                            <span className="text-slate-800">{lead.address}</span>
                        </Fact>
                    )}
                    {(lead.rating != null || lead.review_count != null) && (
                        <Fact icon={<StarIcon className="size-3.5" />}>
                            <span className="text-slate-800">
                                {lead.rating != null ? `${lead.rating.toFixed(1)} stars` : "No rating"}
                                {lead.review_count != null && <span className="text-slate-500"> from {lead.review_count.toLocaleString()} reviews</span>}
                                {lead.price_level != null && <span className="text-slate-500"> · {"$".repeat(lead.price_level)}</span>}
                            </span>
                        </Fact>
                    )}
                    {lead.business_status && lead.business_status !== "operational" && (
                        <Fact icon={<InfoIcon className="size-3.5" />}>
                            <span className="text-rose-700">{lead.business_status === "closed" ? "Permanently closed" : "Temporarily closed"}</span>
                        </Fact>
                    )}
                    {lead.chain && (
                        <Fact icon={<BuildingIcon className="size-3.5" />}>
                            <span className="text-slate-800">Part of {lead.chain}</span>
                        </Fact>
                    )}
                </dl>
                {lead.description && <p className="mt-2 text-[12px] leading-relaxed text-slate-600">{lead.description}</p>}
            </Section>
            {socials.length > 0 && (
                <Section title="Social profiles">
                    <div className="flex flex-wrap gap-1.5">
                        {socials.map(([kind, url]) => (
                            <a
                                key={kind}
                                href={url}
                                target="_blank"
                                rel="noopener noreferrer nofollow"
                                className="h-6 px-2 rounded-md border border-slate-200 text-[11.5px] text-slate-600 hover:text-slate-900 hover:border-slate-300 inline-flex items-center gap-1"
                            >
                                {SOCIAL_LABEL[kind] ?? kind}
                                <ExternalLinkIcon className="size-3" />
                            </a>
                        ))}
                    </div>
                </Section>
            )}
            {lead.hours.length > 0 && (
                <Section title="Hours">
                    <ul className="space-y-0.5 text-[12px] text-slate-700">
                        {lead.hours.map((h) => (
                            <li key={h} className="flex items-center gap-1.5">
                                <ClockIcon className="size-3 text-slate-400" />
                                {h}
                            </li>
                        ))}
                    </ul>
                </Section>
            )}
            {lead.categories.length > 0 && (
                <Section title="Categories">
                    <div className="flex flex-wrap gap-1">
                        {lead.categories.map((c) => (
                            <span key={c} className="rounded bg-slate-100 px-1.5 py-0.5 text-[11px] text-slate-600">
                                {c}
                            </span>
                        ))}
                    </div>
                </Section>
            )}
            <p className="text-[11px] text-slate-400">
                From {provider} · first found {timeAgo(lead.first_seen_at)}
            </p>
        </>
    );
}

function EmailSource({ email, provider }: { email: LeadEmail; provider: string }) {
    if (email.source === "website") return <span>From the website</span>;
    if (email.source !== "search") return <span>From {provider}</span>;
    if (!email.url) return <span>Web search</span>;
    return (
        <a href={email.url} target="_blank" rel="noopener noreferrer nofollow" title={email.url} className="inline-flex items-center gap-0.5 hover:text-sky-700">
            Web search
            <ExternalLinkIcon className="size-2.5" />
        </a>
    );
}

function Fact({ icon, children }: { icon: React.ReactNode; children: React.ReactNode }) {
    return (
        <div className="flex items-start gap-2">
            <span className="mt-0.5 text-slate-400">{icon}</span>
            <div className="min-w-0 break-words">{children}</div>
        </div>
    );
}

function Website({ lead, config }: { lead: Lead; config: LeadFinderConfig }) {
    const s = lead.site;
    if (!lead.website)
        return (
            <p className="text-[12px] text-slate-500">
                {lead.enrichment_status === "no_website"
                    ? "No website could be found for this business. That alone can be the reason to reach out."
                    : "The listing has no website. Find people and emails looks for one from the business's name first."}
            </p>
        );
    if (!s.checked_at) return <p className="text-[12px] text-slate-500">Not read yet. Find people and emails reads the site and reports what it runs on.</p>;
    if (!s.reachable)
        return (
            <div className="space-y-1.5 text-[12px]">
                <p className="font-medium text-slate-700">No info available</p>
                <p className="text-slate-500">{unreadDetail(s)}</p>
                {unreadReason(s) === "blocked" && !config.web_reader.available && (
                    <p className="text-[11.5px] text-slate-400">
                        Optional: a{" "}
                        <Link to="/app/integrations" className="underline underline-offset-2 hover:text-slate-600">
                            Jina AI key
                        </Link>{" "}
                        or a reader service your administrator sets up can read sites like this one.
                    </p>
                )}
            </div>
        );
    return (
        <>
            <Section title="Site">
                <dl className="space-y-1.5 text-[12px]">
                    <Fact icon={s.https ? <LockIcon className="size-3.5 text-emerald-600" /> : <LockIcon className="size-3.5 text-rose-500" />}>
                        <span className="text-slate-800">{s.https ? "Served over HTTPS" : "No HTTPS"}</span>
                    </Fact>
                    {s.platform && (
                        <Fact icon={<GlobeIcon className="size-3.5" />}>
                            <span className="text-slate-800">Built with {s.platform}</span>
                        </Fact>
                    )}
                    <Fact icon={<FileTextIcon className="size-3.5" />}>
                        <span className="text-slate-800">
                            {s.pages ?? 1} page{(s.pages ?? 1) === 1 ? "" : "s"} read {timeAgo(s.checked_at)}
                        </span>
                        {s.read_via && <p className="text-[11px] text-slate-400">Read through {READER_LABEL[s.read_via] ?? "a reader service"}</p>}
                    </Fact>
                </dl>
                {(s.title || s.description) && (
                    <div className="mt-2 rounded-md border border-slate-200 bg-slate-50/60 px-2.5 py-2">
                        {s.title && <p className="text-[12px] font-medium text-slate-800">{s.title}</p>}
                        {s.description && <p className="mt-0.5 text-[11.5px] leading-relaxed text-slate-600">{s.description}</p>}
                    </div>
                )}
            </Section>
            {s.signals && s.signals.length > 0 && (
                <Section title="Signals">
                    <div className="flex flex-wrap gap-1">
                        {s.signals.map((sig) => {
                            const gap = /^No |Not |Outdated/.test(sig);
                            return (
                                <span key={sig} className={cn("rounded px-1.5 py-0.5 text-[11px]", gap ? "bg-amber-50 text-amber-800" : "bg-emerald-50 text-emerald-800")}>
                                    {sig}
                                </span>
                            );
                        })}
                    </div>
                    <p className="mt-1.5 text-[11px] text-slate-400">Amber marks a gap you could offer to fix.</p>
                </Section>
            )}
        </>
    );
}

function Notes({ lead }: { lead: Lead }) {
    const update = useUpdateLead();
    const [notes, setNotes] = useState(lead.notes);
    // A teammate's save must not overwrite what is being typed here.
    const dirty = useRef(false);
    const [tag, setTag] = useState("");
    useEffect(() => {
        if (!dirty.current) setNotes(lead.notes);
    }, [lead.notes]);
    const save = (patch: Parameters<typeof update.mutate>[0]["patch"]) =>
        update.mutate({ id: lead.id, patch }, { onError: (e) => toast.error(buildError(e as unknown as AppError)) });
    const addTag = () => {
        const t = tag.trim();
        setTag("");
        if (!t || lead.tags.includes(t)) return;
        save({ tags: [...lead.tags, t] });
    };
    return (
        <>
            <Section title="Tags">
                <div className="flex flex-wrap items-center gap-1">
                    {lead.tags.map((t) => (
                        <span key={t} className="h-6 pl-2 pr-1 rounded-md bg-sky-50 text-sky-700 text-[11.5px] inline-flex items-center gap-1">
                            <TagIcon className="size-3" />
                            {t}
                            <button type="button" aria-label={`Remove ${t}`} onClick={() => save({ tags: lead.tags.filter((x) => x !== t) })} className="size-4 rounded hover:bg-sky-100 inline-flex items-center justify-center">
                                <XIcon className="size-3" />
                            </button>
                        </span>
                    ))}
                    <input
                        value={tag}
                        maxLength={40}
                        onChange={(e) => setTag(e.target.value)}
                        onKeyDown={(e) => {
                            if (e.key === "Enter" || e.key === ",") {
                                e.preventDefault();
                                addTag();
                            }
                        }}
                        onBlur={addTag}
                        placeholder="Add a tag…"
                        className="h-6 min-w-[100px] flex-1 rounded-md border border-transparent px-1.5 text-[12px] outline-none focus:border-slate-200"
                    />
                </div>
            </Section>
            <Section title="Notes">
                <textarea
                    value={notes}
                    maxLength={5000}
                    onChange={(e) => {
                        dirty.current = true;
                        setNotes(e.target.value);
                    }}
                    onBlur={() => {
                        dirty.current = false;
                        if (notes !== lead.notes) save({ notes });
                    }}
                    rows={8}
                    placeholder="What you know about them, who to ask for, when to follow up…"
                    className="w-full resize-y rounded-md border border-slate-200 px-2.5 py-2 text-[12.5px] text-slate-800 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                />
                <p className="mt-1 text-[11px] text-slate-400">Saved when you click away. Your team sees it too.</p>
            </Section>
        </>
    );
}
