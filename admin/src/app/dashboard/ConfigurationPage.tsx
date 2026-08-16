// Configuration: the resolved environment of the running backend, read only.
// Nothing here can be written from the API, so the page's whole job is to
// answer "is my variable actually being picked up, and does it need a
// restart to change?". Sensitive keys show a fingerprint, never a value.

import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { ExternalLink, Search, Settings2 } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { ErrorState } from "@/components/ErrorState";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { docsUrl } from "@/lib/docs";
import { useQuery } from "@tanstack/react-query";
import {
    getInstanceConfig,
    type ConfigSource,
    type InstanceConfigEntry,
    type RuntimeChangeable,
} from "@/lib/api/client/admin/instance";

const GROUP_LABELS: Record<string, string> = {
    deployment: "Deployment",
    addresses: "Addresses",
    database: "Database",
    cache: "Cache",
    mail: "Platform mail",
    auth: "Authentication",
    encryption: "Encryption",
    storage: "Storage",
    eventbus: "Event bus",
    workers: "Workers",
    tracking: "Tracking",
    captcha: "Captcha",
    observability: "Observability",
};

const GROUP_ORDER = Object.keys(GROUP_LABELS);

const SOURCE_STYLES: Record<ConfigSource, { label: string; className: string; title: string }> = {
    env: {
        label: "env",
        className: "border-emerald-300 bg-emerald-50 text-emerald-700",
        title: "Read from this process's environment.",
    },
    default: {
        label: "default",
        className: "border-zinc-300 bg-zinc-50 text-zinc-600",
        title: "No environment variable set, so the built-in default applies.",
    },
    derived: {
        label: "derived",
        className: "border-sky-300 bg-sky-50 text-sky-700",
        title: "Computed from other values rather than set directly.",
    },
    unset: {
        label: "unset",
        className: "border-amber-300 bg-amber-50 text-amber-700",
        title: "Not set and there is no default: the feature it controls is off.",
    },
};

const RESTART_STYLES: Record<RuntimeChangeable, { label: string; className: string }> = {
    "boot-only": {
        label: "Restart to change",
        className: "border-zinc-300 bg-zinc-50 text-zinc-600",
    },
    "per-request": {
        label: "Takes effect immediately",
        className: "border-emerald-300 bg-emerald-50 text-emerald-700",
    },
};

// "eventbus" -> "Eventbus". Keeps a group the frontend has not learned yet
// rendering instead of disappearing.
function groupLabel(group: string): string {
    if (GROUP_LABELS[group]) return GROUP_LABELS[group];
    const spaced = group.replace(/[-_]+/g, " ").trim();
    if (!spaced) return "Other";
    return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

function groupRank(group: string): number {
    const i = GROUP_ORDER.indexOf(group);
    return i === -1 ? GROUP_ORDER.length : i;
}

function matches(entry: InstanceConfigEntry, needle: string): boolean {
    if (!needle) return true;
    const q = needle.toLowerCase();
    if (entry.key.toLowerCase().includes(q)) return true;
    if (groupLabel(entry.group).toLowerCase().includes(q)) return true;
    if (entry.effect.toLowerCase().includes(q)) return true;
    // A sensitive entry never carries its value, so there is nothing to match.
    if (!entry.sensitive && entry.value.toLowerCase().includes(q)) return true;
    return false;
}

export default function ConfigurationPage() {
    const [search, setSearch] = useState("");

    const configQ = useQuery({
        queryKey: ["admin", "instance", "config"],
        queryFn: getInstanceConfig,
        retry: false,
    });

    const entries = useMemo(() => configQ.data?.entries ?? [], [configQ.data]);
    const filtered = useMemo(
        () => entries.filter((e) => matches(e, search.trim())),
        [entries, search],
    );

    const groups = useMemo(() => {
        const byGroup = new Map<string, InstanceConfigEntry[]>();
        for (const entry of filtered) {
            const list = byGroup.get(entry.group);
            if (list) list.push(entry);
            else byGroup.set(entry.group, [entry]);
        }
        return [...byGroup.entries()].sort(
            (a, b) => groupRank(a[0]) - groupRank(b[0]) || a[0].localeCompare(b[0]),
        );
    }, [filtered]);

    return (
        <div>
            <PageHeader
                title="Configuration"
                description="Every value on this page comes from the environment of the running backend. Change it where you set your environment, then restart."
            >
                <Button size="sm" variant="outline" asChild>
                    <Link to="/configuration/settings">
                        <Settings2 className="size-4" />
                        Instance settings
                    </Link>
                </Button>
            </PageHeader>

            <div className="mb-4 flex flex-wrap items-center gap-3">
                <div className="relative w-full max-w-sm">
                    <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                    <Input
                        value={search}
                        onChange={(e) => setSearch(e.target.value)}
                        placeholder="Search variables, groups or effects"
                        className="h-8 pl-8 text-[12.5px]"
                    />
                </div>
                {entries.length > 0 && (
                    <span className="text-xs text-muted-foreground tabular-nums">
                        {filtered.length} of {entries.length} variables
                    </span>
                )}
            </div>

            {configQ.isLoading && (
                <div className="space-y-3">
                    <Skeleton className="h-40 w-full" />
                    <Skeleton className="h-40 w-full" />
                </div>
            )}

            {configQ.isError && (
                <ErrorState
                    error={configQ.error}
                    title="Could not read this instance's configuration"
                    onRetry={() => configQ.refetch()}
                />
            )}

            {configQ.data && entries.length === 0 && (
                <div className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">
                    The backend returned no configuration entries.
                </div>
            )}

            {configQ.data && entries.length > 0 && filtered.length === 0 && (
                <div className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">
                    No variable matches "{search.trim()}".
                </div>
            )}

            <div className="space-y-4">
                {groups.map(([group, groupEntries]) => (
                    <section
                        key={group}
                        className="overflow-hidden rounded-lg border border-border bg-white"
                    >
                        <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-2.5">
                            <div className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                                {groupLabel(group)}
                            </div>
                            <div className="text-[11px] text-muted-foreground tabular-nums">
                                {groupEntries.length}
                            </div>
                        </div>
                        <div className="divide-y divide-border">
                            {groupEntries.map((entry) => (
                                <ConfigRow key={entry.key} entry={entry} />
                            ))}
                        </div>
                    </section>
                ))}
            </div>
        </div>
    );
}

function ConfigRow({ entry }: { entry: InstanceConfigEntry }) {
    const source = SOURCE_STYLES[entry.source] ?? SOURCE_STYLES.default;
    const restart = RESTART_STYLES[entry.runtime_changeable] ?? RESTART_STYLES["boot-only"];

    return (
        // Anchored on the variable name so a check can deep-link to its row.
        <div id={entry.key} className="scroll-mt-20 px-4 py-3">
            <div className="flex flex-wrap items-center gap-2">
                <code className="text-[12.5px] font-semibold text-foreground">{entry.key}</code>
                <Badge variant="outline" className={`text-[10px] ${source.className}`} title={source.title}>
                    {source.label}
                </Badge>
                <Badge variant="outline" className={`text-[10px] ${restart.className}`}>
                    {restart.label}
                </Badge>
            </div>

            <div className="mt-1.5">
                {entry.sensitive ? (
                    <SensitiveValue entry={entry} />
                ) : (
                    <PlainValue entry={entry} />
                )}
            </div>

            {entry.effect && (
                <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground">
                    {entry.effect}
                </p>
            )}

            {entry.docs && (
                <a
                    href={docsUrl(entry.docs)}
                    target="_blank"
                    rel="noreferrer"
                    className="mt-1.5 inline-flex items-center gap-1 text-xs font-medium text-[var(--admin-accent-strong)] hover:underline"
                >
                    Documentation
                    <ExternalLink className="size-3" />
                </a>
            )}
        </div>
    );
}

// Gated on the resolved value, not on entry.set: a default or derived value
// resolves without any environment variable being present.
function PlainValue({ entry }: { entry: InstanceConfigEntry }) {
    if (entry.value === "") {
        return <span className="text-xs text-muted-foreground">Not set</span>;
    }
    return (
        <code className="block break-all rounded bg-muted px-2 py-1 font-mono text-xs text-foreground">
            {entry.value}
        </code>
    );
}

// A sensitive value is never sent. The fingerprint is there so two services
// can be compared (same AUTH_SECRET?) without disclosing either.
function SensitiveValue({ entry }: { entry: InstanceConfigEntry }) {
    // A fingerprint is only minted for a non-empty resolved value, so it is the
    // reliable "has a value" signal; source covers a backend that omits it.
    const resolved = entry.fingerprint !== "" || entry.source !== "unset";
    if (!resolved) {
        return <span className="text-xs text-muted-foreground">Not set</span>;
    }
    return (
        <span className="inline-flex flex-wrap items-center gap-2">
            <span className="inline-flex items-center gap-1.5 rounded bg-muted px-2 py-1 font-mono text-xs text-muted-foreground">
                Set, value hidden
            </span>
            {entry.fingerprint && (
                <span
                    className="font-mono text-[11px] text-muted-foreground"
                    title="First 4 hex characters of the SHA-256 of the value. Two services holding the same secret show the same fingerprint."
                >
                    fingerprint {entry.fingerprint}
                </span>
            )}
        </span>
    );
}
