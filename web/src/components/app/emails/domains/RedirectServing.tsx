// Where a root redirect is served from, whether visitors get it, and how to fix it (or hand it to Warmbly Cloud) when they do not.
import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertTriangleIcon,
    CheckCircle2Icon,
    CircleDashedIcon,
    CloudIcon,
    ExternalLinkIcon,
    Loader2Icon,
    RefreshCwIcon,
    ServerIcon,
    ZapIcon,
} from "lucide-react";
import type { DomainRedirect, RedirectServer } from "@/lib/api/models/app/emails/SendingDomain";
import timeAgo from "@/lib/helper/timeAgo";
import { cn } from "@/lib/utils";
import { CopyButton, DnsRecordsTable } from "./parts";
import { PROXY_NAMES, redirectBlocked, shortUrl } from "./rules";
import type { CloudServing } from "./cloudServing";

/* ── Served from ─────────────────────── */

export function ServedByPicker({
    value,
    current,
    onChange,
    onConnect,
    cloud,
}: {
    value: RedirectServer;
    /** Where it is served today, so an existing Cloud redirect stays selectable at the limit. */
    current?: RedirectServer;
    onChange: (v: RedirectServer) => void;
    /** Opens the link to Warmbly Cloud in place. */
    onConnect: () => void;
    cloud: CloudServing;
}) {
    // A redirect already on Cloud stays selectable at the limit, never once the link or the offer is gone.
    const cloudOpen = cloud.canServe || (current === "cloud" && cloud.connected && !!cloud.offer?.available);
    let cloudBody: React.ReactNode;
    if (!cloud.connected) {
        cloudBody = (
            <>
                Link this instance to a free Warmbly Cloud workspace and it serves the redirect, with nothing to set up here.
                <span className="block mt-1.5">
                    <button
                        type="button"
                        onClick={(e) => {
                            e.stopPropagation();
                            onConnect();
                        }}
                        className="h-6 px-2 rounded-md border border-sky-200 bg-white text-[11.5px] font-medium text-sky-700 hover:bg-sky-50 inline-flex items-center gap-1 transition-colors"
                    >
                        <CloudIcon className="w-3 h-3" />
                        Connect Warmbly Cloud
                    </button>
                </span>
            </>
        );
    } else if (!cloud.offer?.available) {
        cloudBody = "Warmbly Cloud does not serve redirects for this instance right now.";
    } else if (!cloudOpen && cloud.offer.used >= cloud.offer.limit) {
        cloudBody = `Warmbly Cloud serves up to ${cloud.offer.limit} redirects for one instance, and this one has reached it.`;
    } else if (!cloudOpen) {
        cloudBody = "The Warmbly Cloud link is not tied to this workspace. Reconnect Warmbly Cloud from this workspace to serve redirects there.";
    } else {
        cloudBody = "Point the domain at Warmbly Cloud. It serves the redirect and its certificate, with nothing to set up on your server.";
    }
    return (
        <div className="space-y-1.5">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Served from</div>
            <div role="radiogroup" aria-label="Where the redirect is served from" className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                <ServerOption
                    active={value === "instance"}
                    onSelect={() => onChange("instance")}
                    icon={<ServerIcon className="w-3.5 h-3.5" />}
                    title="This server"
                    body="Point the domain at this instance. If a proxy sits in front of Warmbly, it has to send the domain on."
                />
                <ServerOption
                    active={value === "cloud"}
                    disabled={!cloudOpen}
                    onSelect={() => onChange("cloud")}
                    icon={<CloudIcon className="w-3.5 h-3.5" />}
                    title="Warmbly Cloud"
                    badge="No server setup"
                    body={cloudBody}
                />
            </div>
        </div>
    );
}

function ServerOption({
    active,
    disabled,
    onSelect,
    icon,
    title,
    badge,
    body,
}: {
    active: boolean;
    disabled?: boolean;
    onSelect: () => void;
    icon: React.ReactNode;
    title: string;
    badge?: string;
    body: React.ReactNode;
}) {
    return (
        <div
            role="radio"
            aria-checked={active}
            aria-disabled={disabled || undefined}
            tabIndex={disabled ? -1 : 0}
            onClick={() => !disabled && onSelect()}
            onKeyDown={(e) => {
                if (!disabled && (e.key === " " || e.key === "Enter")) {
                    e.preventDefault();
                    onSelect();
                }
            }}
            className={cn(
                "rounded-md border px-3 py-2.5 text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-100",
                active ? "border-sky-400 bg-sky-50/60 ring-1 ring-sky-100" : "border-slate-200",
                disabled ? "bg-slate-50/60 cursor-default" : "cursor-pointer hover:border-slate-300",
                active && "hover:border-sky-400",
            )}
        >
            <div className="flex items-center gap-1.5">
                <span className={cn("shrink-0", active ? "text-sky-700" : disabled ? "text-slate-400" : "text-slate-500")}>{icon}</span>
                <span className={cn("text-[12.5px] font-medium", disabled && !active ? "text-slate-500" : "text-slate-900")}>{title}</span>
                {badge && !disabled && (
                    <span className="ml-auto h-4 px-1.5 rounded bg-emerald-50 text-emerald-700 text-[10px] font-medium inline-flex items-center whitespace-nowrap">
                        {badge}
                    </span>
                )}
            </div>
            <p className="mt-1 text-[11.5px] text-slate-500 leading-relaxed">{body}</p>
        </div>
    );
}

/* ── Progress ─────────────────────── */

type StepState = "done" | "todo" | "blocked" | "waiting";

interface ProgressStep {
    key: string;
    state: StepState;
    title: string;
    body?: React.ReactNode;
}

function StepMark({ state, n }: { state: StepState; n: number }) {
    if (state === "done") return <CheckCircle2Icon className="w-4 h-4 text-emerald-600" />;
    if (state === "blocked") return <AlertTriangleIcon className="w-4 h-4 text-amber-600" />;
    if (state === "waiting") return <CircleDashedIcon className="w-4 h-4 text-slate-300" />;
    return (
        <span className="size-4 rounded-full ring-1 ring-inset ring-amber-300 bg-amber-50 text-amber-700 text-[10px] font-semibold inline-flex items-center justify-center">
            {n}
        </span>
    );
}

/** What stands between the redirect and a visitor, one step at a time, with the fix for the step that is missing. */
export function RedirectProgress({
    domain,
    redirect,
    fix,
    onCheck,
    checking,
}: {
    domain: string;
    redirect: DomainRedirect;
    /** Shown under the visitor step when DNS is right but visitors do not get the redirect. */
    fix: React.ReactNode;
    onCheck: () => void;
    checking: boolean;
}) {
    const reach = redirect.reach ?? null;
    const cloud = redirect.served_by === "cloud";
    const site = redirect.website?.record ? redirect.website : null;
    const siteOk = redirect.target_active !== false;
    const blocked = redirectBlocked(redirect);
    const live = redirect.verified && siteOk && !blocked;
    const checkedAt = redirect.last_checked_at ?? reach?.checked_at;
    const where = cloud ? "Warmbly Cloud" : "this server";

    const steps: ProgressStep[] = [];
    steps.push(
        redirect.verified
            ? { key: "dns", state: "done", title: `${domain} points at ${where}` }
            : cloud && !siteOk
              ? { key: "dns", state: "waiting", title: `${domain} points at ${where}`, body: "Warmbly Cloud checks the records once the website is confirmed." }
              : {
                    key: "dns",
                    state: "todo",
                    title: `Point ${domain} at ${where}`,
                    body: redirect.last_error || "Add the DNS records listed below at your DNS provider.",
                },
    );
    if (site) {
        steps.push(
            site.verified
                ? { key: "site", state: "done", title: `${site.domain} is confirmed as your website` }
                : {
                      key: "site",
                      state: "todo",
                      title: `Confirm ${site.domain} is your website`,
                      body: (
                          <div className="space-y-2">
                              <p>
                                  Redirects only send visitors to websites this workspace controls. Add this TXT record where{" "}
                                  <span className="font-medium text-slate-800">{site.domain}</span>&apos;s DNS is managed (not {domain}&apos;s), then
                                  press Check now. It does not change your website.
                              </p>
                              <DnsRecordsTable
                                  records={[{ key: "website", type: site.record!.type, name: site.record!.name, value: site.record!.value, ok: site.record!.ok }]}
                                  className="bg-white"
                              />
                          </div>
                      ),
                  },
        );
    }
    if (live) {
        steps.push({
            key: "visit",
            state: "done",
            title: "Visitors get the redirect",
            body:
                reach?.status === "unreachable" ? (
                    reach.hint === "no_listener" ? (
                        <>
                            This server could not open {domain} to double-check, which some networks do not allow.{" "}
                            <a
                                href={`http://${domain}`}
                                target="_blank"
                                rel="noreferrer"
                                className="text-sky-700 hover:text-sky-800 underline decoration-sky-300 hover:decoration-sky-600"
                            >
                                Open it
                            </a>{" "}
                            to confirm.
                        </>
                    ) : (
                        reach.detail
                    )
                ) : undefined,
        });
    } else if (blocked && siteOk) {
        steps.push({
            key: "visit",
            state: "blocked",
            title: "Visitors do not get the redirect yet",
            body: (
                <div className="space-y-2">
                    <p>{reach?.detail || `Opening ${domain} does not reach the redirect yet.`}</p>
                    {fix}
                </div>
            ),
        });
    } else {
        steps.push({ key: "visit", state: "waiting", title: "Visitors get the redirect", body: "Checked by opening the domain once the steps above are done." });
    }

    const left = steps.filter((st) => st.state === "todo").length;
    const headline = live
        ? "Live"
        : blocked && siteOk
          ? "DNS is in place, but visitors do not get the redirect"
          : left > 0
            ? `${left} ${left === 1 ? "step" : "steps"} left`
            : "Almost there";
    let n = 0;

    return (
        <div className={cn("rounded-md border", live ? "border-emerald-200 bg-emerald-50/40" : "border-slate-200 bg-white")}>
            <div className="px-3 pt-2.5 pb-2 flex items-start gap-2">
                <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5 flex-wrap">
                        <p className={cn("text-[12.5px] font-medium", live ? "text-emerald-900" : "text-slate-900")}>{headline}</p>
                        {cloud && (
                            <span className="h-4 px-1.5 rounded bg-white/70 ring-1 ring-inset ring-slate-200 text-slate-600 text-[10px] font-medium inline-flex items-center gap-1">
                                <CloudIcon className="w-2.5 h-2.5" />
                                Warmbly Cloud
                            </span>
                        )}
                    </div>
                    <p className="text-[11.5px] text-slate-600 break-words">
                        {domain}
                        {redirect.include_www ? ` and www.${domain}` : ""} <span className="text-slate-400">&rarr;</span>{" "}
                        <a
                            href={redirect.target_url}
                            target="_blank"
                            rel="noreferrer"
                            className="inline-flex items-center gap-0.5 text-slate-800 underline decoration-slate-300 hover:decoration-slate-600 break-all"
                        >
                            {shortUrl(redirect.target_url)}
                            <ExternalLinkIcon className="w-2.5 h-2.5 shrink-0" />
                        </a>
                    </p>
                </div>
                <button
                    type="button"
                    onClick={onCheck}
                    disabled={checking}
                    className="shrink-0 h-6 px-2 rounded-md border border-slate-200 bg-white text-[11.5px] text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                >
                    <RefreshCwIcon className={cn("w-3 h-3", checking && "animate-spin")} />
                    Check now
                </button>
            </div>
            <ol className="px-3 pb-2.5 space-y-2">
                {steps.map((st) => {
                    if (st.state === "todo") n++;
                    return (
                        <li key={st.key} className="flex items-start gap-2">
                            <span className="mt-px shrink-0">
                                <StepMark state={st.state} n={n} />
                            </span>
                            <div className="min-w-0 flex-1 text-[11.5px] leading-relaxed">
                                <p
                                    className={cn(
                                        "text-[12px]",
                                        st.state === "done" ? "text-slate-600" : st.state === "waiting" ? "text-slate-500" : "text-slate-900 font-medium",
                                    )}
                                >
                                    {st.title}
                                </p>
                                {st.body && <div className="text-slate-600 break-words">{st.body}</div>}
                            </div>
                        </li>
                    );
                })}
            </ol>
            {checkedAt && (
                <p className="px-3 pb-2 text-[11px] text-slate-400">
                    Last checked {timeAgo(checkedAt)}. Warmbly also checks by itself.
                </p>
            )}
        </div>
    );
}

/* ── Fix ─────────────────────── */

type ProxyKey = "traefik" | "coolify" | "dokploy" | "nginx" | "caddy";

const PROXIES: { key: ProxyKey; label: string }[] = [
    { key: "traefik", label: "Traefik" },
    { key: "coolify", label: "Coolify" },
    { key: "dokploy", label: "Dokploy" },
    { key: "nginx", label: "nginx" },
    { key: "caddy", label: "Caddy" },
];

interface Step {
    text: React.ReactNode;
    code?: string;
}

function proxySteps(key: ProxyKey, domain: string, host: string, www: boolean): Step[] {
    const names = [domain, ...(www ? [`www.${domain}`] : [])];
    const H = (h: string) => <span className="font-mono text-[11px] text-slate-800">{h}</span>;
    switch (key) {
        case "traefik":
            return [
                { text: <>Find the router rule that has {H(host)}, usually a label on the tracking container, and add the domain to it:</>, code: [host, ...names].map((n) => `Host(\`${n}\`)`).join(" || ") },
                { text: <>Apply it with <span className="font-mono text-[11px]">docker compose up -d</span>. The router&apos;s certificate resolver gets the certificates.</> },
            ];
        case "coolify":
            return [
                { text: <>Open your Warmbly resource and the service whose Domains field has {H(host)}. Add the domain to that field:</>, code: [host, ...names].map((n) => `https://${n}`).join(",") },
                { text: "Save and redeploy. Coolify gets the certificates." },
            ];
        case "dokploy":
            return [
                { text: "Open your Warmbly compose project and go to Domains." },
                ...names.map((n) => ({ text: <>Add a domain: host {H(n)}, service tracking, port 3000, HTTPS on with Let&apos;s Encrypt.</> })),
                { text: "Deploy." },
            ];
        case "nginx":
            return [
                { text: <>In the server block that proxies {H(host)}, add the domain to its names:</>, code: `server_name ${[host, ...names].join(" ")};` },
                { text: "Keep the original hostname on the way to Warmbly, then get a certificate and reload nginx:", code: `proxy_set_header Host $host;\ncertbot --nginx ${names.map((n) => `-d ${n}`).join(" ")}` },
            ];
        case "caddy":
            return [
                { text: <>Add the domain to the site block for {H(host)}:</>, code: `${[host, ...names].join(", ")} {` },
                { text: "Reload Caddy. It gets the certificates by itself." },
            ];
    }
}

function initialProxy(proxy?: string): ProxyKey {
    return proxy === "nginx" || proxy === "caddy" ? proxy : "traefik";
}

function CodeLine({ code }: { code: string }) {
    return (
        <div className="mt-1 rounded-md bg-slate-900 text-slate-100 flex items-start gap-2 pl-2.5 pr-1 py-1.5">
            <pre className="min-w-0 flex-1 font-mono text-[11px] leading-relaxed whitespace-pre-wrap break-all">{code}</pre>
            <span className="[&_button]:text-slate-400 [&_button:hover]:text-white [&_button:hover]:bg-white/10">
                <CopyButton value={code} label="the snippet" />
            </span>
        </div>
    );
}

/** What to change when DNS is right but visitors do not get the redirect, with Cloud as the way around it. */
export function ReachFix({
    domain,
    redirect,
    cloud,
    onServeFromCloud,
    onConnect,
    switching,
}: {
    domain: string;
    redirect: DomainRedirect;
    cloud: CloudServing;
    onServeFromCloud: () => void;
    /** Opens the link to Warmbly Cloud in place; serving from it follows. */
    onConnect: () => void;
    switching: boolean;
}) {
    const reach = redirect.reach;
    const [proxy, setProxy] = React.useState<ProxyKey>(() => initialProxy(reach?.proxy));
    if (!reach || !redirectBlocked(redirect)) return null;

    const host = redirect.serve_host || "your tracking host";
    const www = redirect.include_www;
    const named = reach.proxy ? PROXY_NAMES[reach.proxy] : "";
    let intro: React.ReactNode;
    let showProxies = true;
    if (redirect.served_by === "cloud") {
        intro = `Warmbly Cloud serves ${domain}, but the check did not reach it. Make sure the root records point only at the addresses below, and remove any other A or AAAA record for ${domain}.`;
        showProxies = false;
    } else {
        switch (reach.hint) {
            case "host_header":
                intro = "Your proxy sends the visit to Warmbly but replaces the hostname, so Warmbly cannot tell which domain was asked for. Pass the original Host header through. In nginx that is:";
                showProxies = false;
                break;
            case "wrong_target":
                intro = `Something in front of Warmbly already redirects ${domain} elsewhere: a proxy rule, or forwarding at your domain or DNS provider. Remove that redirect so the visit reaches Warmbly.`;
                showProxies = false;
                break;
            case "certificate":
                intro = `The redirect works over http, but https://${domain} has no valid certificate, so browsers warn before it. ${named ? `${named} gets` : "Your proxy gets"} one once the domain is in its config:`;
                break;
            case "no_listener":
                intro =
                    reach.status === "https_error"
                        ? `Nothing answers on port 443 for ${domain}. Open port 443 in your firewall, and make sure your proxy serves the domain:`
                        : `Nothing answers on port 80 for ${domain}. Open port 80 in your firewall; visitors who type the domain without https, and certificate issuance, both need it.`;
                showProxies = reach.status === "https_error";
                break;
            default:
                intro = named
                    ? `${named} on your server answered instead of Warmbly. It only serves the hostnames it was set up with, so add ${domain} next to ${host}, which already reaches Warmbly.`
                    : `Something in front of Warmbly answered instead, usually a proxy such as Traefik, nginx or Caddy, or a hosting panel. Add ${domain} wherever ${host} is set up, since that one already reaches Warmbly.`;
        }
    }
    const steps = proxySteps(proxy, domain, host, www);

    return (
        <div className="rounded-md border border-slate-200 bg-white">
            <div className="px-3 py-2.5 space-y-2 text-[11.5px] text-slate-600 leading-relaxed">
                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">How to fix it</div>
                <p>{intro}</p>
                {reach.hint === "host_header" && redirect.served_by !== "cloud" && <CodeLine code="proxy_set_header Host $host;" />}
                {showProxies && (
                    <>
                        <div role="tablist" aria-label="Your proxy" className="flex flex-wrap gap-1">
                            {PROXIES.map((p) => (
                                <button
                                    key={p.key}
                                    type="button"
                                    role="tab"
                                    aria-selected={proxy === p.key}
                                    onClick={() => setProxy(p.key)}
                                    className={cn(
                                        "h-6 px-2 rounded-md text-[11.5px] transition-colors",
                                        proxy === p.key ? "bg-sky-50 text-sky-700 font-medium ring-1 ring-inset ring-sky-200" : "text-slate-600 hover:bg-slate-100",
                                    )}
                                >
                                    {p.label}
                                </button>
                            ))}
                        </div>
                        {reach.proxy === "traefik" && (proxy === "traefik" || proxy === "coolify" || proxy === "dokploy") && (
                            <p className="text-[11px] text-slate-500">Coolify and Dokploy run Traefik underneath, so pick yours if you use one.</p>
                        )}
                        <AnimatePresence mode="wait" initial={false}>
                            <motion.ol
                                key={proxy}
                                initial={{ opacity: 0, y: 4 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, y: -4 }}
                                transition={{ duration: 0.14 }}
                                className="space-y-2 list-decimal pl-4 marker:text-slate-400"
                            >
                                {steps.map((s, i) => (
                                    <li key={i}>
                                        {s.text}
                                        {s.code && <CodeLine code={s.code} />}
                                    </li>
                                ))}
                            </motion.ol>
                        </AnimatePresence>
                    </>
                )}
                <p className="text-[11px] text-slate-500">After changing it, press Check now. Warmbly also checks again every 10 minutes.</p>
            </div>
            {redirect.served_by !== "cloud" && cloud.choosable && (cloud.canServe || !cloud.connected) && (
                <div className="border-t border-slate-100 px-3 py-2.5 flex items-start gap-2.5 bg-slate-50/60 rounded-b-md">
                    <CloudIcon className="w-3.5 h-3.5 text-sky-600 mt-0.5 shrink-0" />
                    <div className="min-w-0 flex-1 text-[11.5px] text-slate-600 leading-relaxed">
                        <p className="text-[12px] font-medium text-slate-900">Or skip the proxy work</p>
                        <p>
                            {cloud.canServe
                                ? "Warmbly Cloud can serve this redirect and its certificate. You replace the root records; nothing changes on your server."
                                : "Link this instance to a free Warmbly Cloud workspace and it can serve this redirect and its certificate, with nothing to change on your server. It takes about a minute."}
                        </p>
                        <div className="mt-2">
                            {cloud.canServe ? (
                                <button
                                    type="button"
                                    onClick={onServeFromCloud}
                                    disabled={switching}
                                    className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                                >
                                    {switching ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <ZapIcon className="w-3 h-3" />}
                                    Serve from Warmbly Cloud
                                </button>
                            ) : (
                                <button
                                    type="button"
                                    onClick={onConnect}
                                    className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                                >
                                    <CloudIcon className="w-3 h-3" />
                                    Connect Warmbly Cloud
                                </button>
                            )}
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}
