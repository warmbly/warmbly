// Domain choices an import applies as each mailbox connects: a tracking host on
// the domain (link.<domain> unless one already works) and a redirect of its root
// to the company website. One base value covers every domain; the dropdown under
// it changes any domain on its own.
import React from "react";
import { SparklesIcon } from "lucide-react";
import { TextInput } from "@/components/ui/field";
import { CheckSquare } from "@/components/ui/check-square";
import { Chip, SuggestionChip } from "@/components/app/emails/domains/parts";
import { PerDomainList, type PerDomainRow } from "@/components/app/emails/domains/PerDomainList";
import { commonWebsite, redirectTargetProblem, trackingHostProblem } from "@/components/app/emails/domains/rules";
import { useSendingDomains } from "@/lib/api/hooks/app/emails/useSendingDomains";
import { vendorLabel } from "@/lib/api/models/app/emails/MailboxSources";
import { plural } from "./importFields";
import { SectionLabel } from "./parts";
import {
    DEFAULT_TRACKING_LABEL,
    effectivePick,
    labelProblem,
    needsRecord,
    offersChoices,
    vendorCoverage,
    vendorForwards,
    vendorWritesCname,
    type DomainInfo,
    type DomainPicks,
} from "./domainChoiceRules";

function CheckRow({
    checked,
    onToggle,
    label,
    children,
}: {
    checked: boolean;
    onToggle: () => void;
    label: string;
    children?: React.ReactNode;
}) {
    return (
        <div className="flex items-center gap-2 min-w-0 min-h-7">
            <button
                type="button"
                role="checkbox"
                aria-checked={checked}
                onClick={onToggle}
                className="inline-flex items-center gap-2 shrink-0 text-[11.5px] text-slate-700 hover:text-slate-900 rounded outline-none focus-visible:ring-2 focus-visible:ring-sky-100"
            >
                <CheckSquare checked={checked} />
                {label}
            </button>
            {children}
        </div>
    );
}

/** The choice for every domain at once, with how much of it the vendor does. */
export function AllDomainsChoice({
    infos,
    picks,
    setPicks,
}: {
    infos: DomainInfo[];
    picks: DomainPicks;
    setPicks: React.Dispatch<React.SetStateAction<DomainPicks>>;
}) {
    const sending = useSendingDomains();
    const website = React.useMemo(() => commonWebsite(sending.data?.data ?? []), [sending.data]);
    const usable = infos.filter(offersChoices);
    const withTracking = usable.filter((i) => i.tracking);
    const cov = vendorCoverage(usable);
    const vendor = vendorLabel(cov.vendor);
    const track = picks.all.track ?? true;
    const label = picks.all.label ?? DEFAULT_TRACKING_LABEL;
    const redirect = picks.all.redirect ?? false;
    const url = picks.all.url ?? "";
    const lp = track ? labelProblem(label) : null;
    const urlProblem = redirect && url.trim() ? redirectTargetProblem(url, "") : null;
    const n = usable.length;
    const many = n > 1;
    const newHosts = withTracking.filter((i) => i.tracking?.status === "suggested").length;
    const example = usable[0]?.domain ?? "yourdomain.com";
    const exampleHost = many ? `${label || DEFAULT_TRACKING_LABEL}.${example}` : effectivePick(usable[0], picks).host;

    // Base values fill every domain nobody changed; a changed domain keeps its own until Reset.
    const setTrack = (on: boolean) => setPicks((p) => ({ ...p, all: { ...p.all, track: on } }));
    const setLabel = (v: string) => setPicks((p) => ({ ...p, all: { ...p.all, label: v.trim().toLowerCase() } }));
    const setRedirect = (on: boolean) =>
        setPicks((p) => ({ ...p, all: { ...p.all, redirect: on, url: on && !p.all.url ? website : p.all.url } }));
    const setUrl = (v: string) => setPicks((p) => ({ ...p, all: { ...p.all, url: v } }));

    if (n === 0) return null;
    return (
        <div className="rounded-lg border border-sky-200 bg-sky-50/40 p-3 space-y-3">
            <div className="flex items-center gap-1.5">
                <SparklesIcon className="w-3 h-3 text-sky-600" />
                <span className="text-[12px] font-medium text-slate-900">{many ? `For all ${n.toLocaleString()} domains` : "Recommended for deliverability"}</span>
            </div>

            {withTracking.length > 0 && (
                <div className="space-y-1">
                    <CheckRow checked={track} onToggle={() => setTrack(!track)} label="Own tracking domain">
                        {track && newHosts > 0 && (
                            <span className="inline-flex items-center min-w-0 rounded-md border border-slate-200 bg-white focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100">
                                <input
                                    value={label}
                                    onChange={(e) => setLabel(e.target.value)}
                                    aria-label="Tracking subdomain"
                                    aria-invalid={!!lp}
                                    spellCheck={false}
                                    className="h-7 w-16 pl-2 bg-transparent text-[12px] font-mono text-slate-900 outline-none"
                                />
                                <span className="pr-2 text-[12px] font-mono text-slate-400 truncate">.{many ? "<domain>" : example}</span>
                            </span>
                        )}
                        <Chip tone="sky">Recommended</Chip>
                    </CheckRow>
                    <p className="pl-[22px] text-[11px] text-slate-500 leading-relaxed">
                        Links and the open pixel use {exampleHost} instead of a host shared with other senders, so every link
                        in a message points at the company that sent it.
                        {track && newHosts > 0 && (
                            cov.cname > 0 ? (
                                <span className="text-slate-600">
                                    {" "}
                                    {vendor} adds the DNS record for {cov.cname === n ? (many ? "every one" : "it") : `${cov.cname} of ${n}`}.
                                    {cov.cname < n && " The rest wait on Sending domains for a CNAME you add."}
                                </span>
                            ) : (
                                <span className="text-slate-600">
                                    {" "}
                                    {many ? "Each needs" : "It needs"} a CNAME you add after the import; links switch over once it resolves.
                                </span>
                            )
                        )}
                    </p>
                    {lp && <p className="pl-[22px] text-[11px] text-rose-700">{lp}</p>}
                </div>
            )}

            <div className="space-y-1">
                <CheckRow checked={redirect} onToggle={() => setRedirect(!redirect)} label={many ? "Redirect every root to" : "Redirect root to"}>
                    {redirect && (
                        <TextInput
                            value={url}
                            onChange={setUrl}
                            placeholder={website || "https://yourcompany.com"}
                            invalid={!!urlProblem}
                            title={urlProblem ?? undefined}
                            className="flex-1 min-w-0"
                        />
                    )}
                </CheckRow>
                <p className="pl-[22px] text-[11px] text-slate-500 leading-relaxed">
                    Someone who types {many ? "one of these domains" : example} into a browser lands on your website instead of an empty page, so it reads as
                    part of a real business.
                    {redirect &&
                        (cov.forward > 0 ? (
                            <span className="text-slate-600">
                                {" "}
                                {vendor} forwards {cov.forward === n ? (many ? "all of them" : "it") : `${cov.forward} of ${n}`} with no DNS to add.
                            </span>
                        ) : (
                            <span className="text-slate-600"> The DNS records to add wait on Sending domains after the import.</span>
                        ))}
                </p>
                {urlProblem && <p className="pl-[22px] text-[11px] text-rose-700">{urlProblem}</p>}
            </div>
        </div>
    );
}

/** The shared choice with each domain's own values in a dropdown under it. */
export function DomainChoicesPanel({
    infos,
    picks,
    setPicks,
}: {
    infos: DomainInfo[];
    picks: DomainPicks;
    setPicks: React.Dispatch<React.SetStateAction<DomainPicks>>;
}) {
    const usable = infos.filter(offersChoices);
    if (usable.length === 0) return null;
    const rows: PerDomainRow[] = usable.map((info) => {
        const own = picks.each[info.domain];
        const e = effectivePick(info, picks);
        const vendor = vendorLabel(info.vendor_domain?.vendor);
        const t = info.tracking;
        const trackChip = !t ? null : !needsRecord(info, e.host) ? (
            <SuggestionChip status={t.status} />
        ) : vendorWritesCname(info.vendor_domain) ? (
            <Chip tone="sky" title={`${vendor} writes the CNAME through its API during the import.`}>
                {vendor} adds DNS
            </Chip>
        ) : (
            <Chip tone="slate" title={t.cname_target ? `Add a CNAME for ${e.host} pointing at ${t.cname_target} after the import.` : undefined}>
                Needs DNS
            </Chip>
        );
        const redirectChip =
            info.redirect?.verified && info.redirect.target_url === e.url.trim() ? (
                <Chip tone="emerald">Verified</Chip>
            ) : vendorForwards(info.vendor_domain) ? (
                <Chip tone="sky" title={`${vendor} forwards the domain itself, so there is no DNS to add.`}>
                    Via {vendor}
                </Chip>
            ) : null;
        return {
            domain: info.domain,
            canTrack: !!t || !!info.trackingLoading,
            loading: info.trackingLoading,
            track: e.track,
            host: e.host,
            redirect: e.redirect,
            url: e.url,
            urlPlaceholder: picks.all.url ?? "",
            custom: !!own && Object.keys(own).length > 0,
            trackChip,
            redirectChip,
            hostProblem: own?.host !== undefined ? trackingHostProblem(e.host, info.domain) : null,
            urlProblem: e.redirect ? redirectTargetProblem(e.url, info.domain) : null,
        };
    });
    return (
        <div className="space-y-2">
            <AllDomainsChoice infos={infos} picks={picks} setPicks={setPicks} />
            {usable.length > 1 && (
                <PerDomainList
                    rows={rows}
                    onChange={(d, patch) => setPicks((prev) => ({ ...prev, each: { ...prev.each, [d]: { ...prev.each[d], ...patch } } }))}
                    onReset={(d) =>
                        setPicks((prev) => {
                            const each = { ...prev.each };
                            delete each[d];
                            return { ...prev, each };
                        })
                    }
                />
            )}
        </div>
    );
}

/** The picked mailboxes' domains with their choices, for the vendor and grant wizards. */
export function DomainChoicesSection({
    infos,
    picks,
    setPicks,
    capped = 0,
}: {
    infos: DomainInfo[];
    picks: DomainPicks;
    setPicks: React.Dispatch<React.SetStateAction<DomainPicks>>;
    /** Domains past the ones shown, which get no choices here. */
    capped?: number;
}) {
    if (!infos.some(offersChoices)) return null;
    return (
        <div>
            <SectionLabel className="mb-1.5">Domains</SectionLabel>
            <DomainChoicesPanel infos={infos} picks={picks} setPicks={setPicks} />
            {capped > 0 && (
                <p className="mt-1 text-[11px] text-slate-500">
                    {plural(capped, "more domain is", "more domains are")} not listed. Set them up on the Sending domains page after the
                    import.
                </p>
            )}
        </div>
    );
}
