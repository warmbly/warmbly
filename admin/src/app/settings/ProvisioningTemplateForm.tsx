// Shared form for create/edit of a provisioning template.
//
// The same form is reused by:
//   - /admin/provisioning-templates (full template CRUD)
//   - /admin/workers (Provision new modal → "Custom" tab)
//
// The component is "controlled" - the parent owns the form state and
// passes it back in. That keeps the live-cost preview, the
// auto-provision toggle exclusivity, and the "save as template"
// affordance composable across both surfaces.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, Info, Plus, Trash2 } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { Skeleton } from "@/components/ui/skeleton";
import {
    listHetznerLocations,
    listHetznerServerTypes,
} from "@/lib/api/client/admin/cloud";
import {
    listProvisioningTemplates,
    listWorkerProfiles,
} from "@/lib/api/client/admin/provisioning";
import type {
    HetznerServerType,
    ProvisioningConfig,
    ProvisioningEgressKind,
    ProvisioningTemplate,
    ProvisioningWorkerTier,
} from "@/lib/api/models/admin";

export interface TemplateFormValue {
    name: string;
    description: string;
    config: ProvisioningConfig;
    /** When set, this template becomes the default auto-provision template for the tier. */
    auto_provision_tier: ProvisioningWorkerTier | "";
    is_draft: boolean;
}

interface Props {
    /** Existing template being edited; null when creating. */
    editing?: ProvisioningTemplate | null;
    /** Hides the name/description/draft block when used inside the provision modal. */
    mode?: "template" | "inline";
    value: TemplateFormValue;
    onChange: (v: TemplateFormValue) => void;
}

const DEFAULT_CONFIG: ProvisioningConfig = {
    provider: "hetzner",
    location: "fsn1",
    server_type: "cx22",
    server_count: 1,
    ipv4_per_server: 1,
    ipv6_per_server: 1,
    worker_tier: "shared_premium",
    egress_kind: "cold_smtp",
    image: "ubuntu-22.04",
    firewall: "warmbly-worker",
    labels: [],
};

export function makeEmptyFormValue(): TemplateFormValue {
    return {
        name: "",
        description: "",
        config: { ...DEFAULT_CONFIG, labels: [] },
        auto_provision_tier: "",
        is_draft: true,
    };
}

export function templateToFormValue(t: ProvisioningTemplate): TemplateFormValue {
    return {
        name: t.name,
        description: t.description ?? "",
        config: { ...DEFAULT_CONFIG, ...t.config, labels: t.config.labels ?? [] },
        auto_provision_tier: t.auto_provision_tier ?? "",
        is_draft: t.is_draft,
    };
}

const TIERS: { value: ProvisioningWorkerTier; label: string }[] = [
    { value: "shared_free", label: "Shared - free" },
    { value: "shared_premium", label: "Shared - premium" },
    { value: "dedicated", label: "Dedicated" },
];

const EGRESS: { value: ProvisioningEgressKind; label: string }[] = [
    { value: "cold_smtp", label: "Cold SMTP" },
    { value: "oauth_api", label: "OAuth API" },
    { value: "warmup_only", label: "Warmup only" },
];

export function ProvisioningTemplateForm({
    editing,
    mode = "template",
    value,
    onChange,
}: Props) {
    const [showAdvanced, setShowAdvanced] = useState(false);

    const locationsQ = useQuery({
        queryKey: ["admin", "hetzner-locations"],
        queryFn: listHetznerLocations,
        retry: false,
        staleTime: 5 * 60_000,
    });
    const serverTypesQ = useQuery({
        queryKey: ["admin", "hetzner-server-types"],
        queryFn: listHetznerServerTypes,
        retry: false,
        staleTime: 5 * 60_000,
    });
    const profilesQ = useQuery({
        queryKey: ["admin", "worker-profiles"],
        queryFn: listWorkerProfiles,
        retry: false,
        staleTime: 5 * 60_000,
    });
    const templatesQ = useQuery({
        queryKey: ["admin", "provisioning-templates"],
        queryFn: listProvisioningTemplates,
        retry: false,
    });

    const selectedServerType: HetznerServerType | undefined = useMemo(() => {
        return serverTypesQ.data?.find((s) => s.name === value.config.server_type);
    }, [serverTypesQ.data, value.config.server_type]);

    // Live cost preview. Server price * count, plus the cost of any IPs
    // beyond the one Hetzner includes free with each box.
    const cost = useMemo(() => {
        const sp = selectedServerType?.price_monthly_eur ?? 0;
        const ipPrice = selectedServerType?.price_ipv4_monthly_eur ?? 0.5;
        const extraIps =
            Math.max(0, value.config.ipv4_per_server - 1) * value.config.server_count;
        const serverTotal = sp * value.config.server_count;
        const ipTotal = ipPrice * extraIps;
        return {
            serverTotal,
            ipTotal,
            total: serverTotal + ipTotal,
            extraIps,
            ipPrice,
        };
    }, [selectedServerType, value.config]);

    const totalIpSlots = value.config.ipv4_per_server * value.config.server_count;

    // Auto-provision tier exclusivity: if another template already owns
    // the tier, disable the checkbox here for that tier (unless this is
    // the template doing it).
    const claimedTiers = useMemo(() => {
        const claimed = new Set<ProvisioningWorkerTier>();
        for (const t of templatesQ.data ?? []) {
            if (t.auto_provision_tier && t.id !== editing?.id) {
                claimed.add(t.auto_provision_tier);
            }
        }
        return claimed;
    }, [templatesQ.data, editing?.id]);

    const set = (next: Partial<TemplateFormValue>) =>
        onChange({ ...value, ...next });
    const setConfig = (next: Partial<ProvisioningConfig>) =>
        onChange({ ...value, config: { ...value.config, ...next } });

    return (
        <div className="space-y-6">
            {mode === "template" && (
                <Section title="Template metadata">
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                        <div className="space-y-1.5">
                            <Label htmlFor="t-name">Name</Label>
                            <Input
                                id="t-name"
                                placeholder="e.g. EU shared premium - small"
                                value={value.name}
                                onChange={(e) => set({ name: e.target.value })}
                            />
                        </div>
                        <div className="space-y-1.5">
                            <Label htmlFor="t-provider">Provider</Label>
                            <Select
                                value={value.config.provider}
                                onValueChange={(v) =>
                                    setConfig({ provider: v as "hetzner" })
                                }
                            >
                                <SelectTrigger id="t-provider">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="hetzner">Hetzner Cloud</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>
                    <div className="space-y-1.5">
                        <Label htmlFor="t-desc">Description (optional)</Label>
                        <Textarea
                            id="t-desc"
                            placeholder="What this template is meant for..."
                            value={value.description}
                            onChange={(e) => set({ description: e.target.value })}
                            rows={2}
                        />
                    </div>
                </Section>
            )}

            <Section title="Location & server">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                    <div className="space-y-1.5">
                        <Label htmlFor="t-loc">Location</Label>
                        {locationsQ.isLoading ? (
                            <Skeleton className="h-9 w-full" />
                        ) : (
                            <Select
                                value={value.config.location}
                                onValueChange={(v) => setConfig({ location: v })}
                            >
                                <SelectTrigger id="t-loc">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    {(locationsQ.data ?? FALLBACK_LOCATIONS).map((l) => (
                                        <SelectItem key={l.name} value={l.name}>
                                            <span className="font-mono text-xs mr-2">
                                                {l.name}
                                            </span>
                                            {l.description || `${l.city}, ${l.country}`}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        )}
                        {locationsQ.data && locationsQ.data.length === 0 && (
                            <p className="text-[11px] text-amber-700">
                                Backend endpoint not yet available - using built-in fallback list.
                            </p>
                        )}
                    </div>

                    <div className="space-y-1.5">
                        <Label htmlFor="t-stype">Server type</Label>
                        {serverTypesQ.isLoading ? (
                            <Skeleton className="h-9 w-full" />
                        ) : (
                            <Select
                                value={value.config.server_type}
                                onValueChange={(v) => setConfig({ server_type: v })}
                            >
                                <SelectTrigger id="t-stype">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    {(serverTypesQ.data ?? FALLBACK_SERVER_TYPES).map((s) => (
                                        <SelectItem key={s.name} value={s.name}>
                                            <span className="font-mono text-xs mr-2">
                                                {s.name}
                                            </span>
                                            {s.cores} CPU, {s.memory_gb} GB RAM,{" "}
                                            {s.disk_gb} GB - €{s.price_monthly_eur.toFixed(2)}/mo
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        )}
                        {selectedServerType && (
                            <p className="text-[11px] text-muted-foreground">
                                {selectedServerType.description ||
                                    `${selectedServerType.cores} cores, ${selectedServerType.memory_gb} GB RAM, ${selectedServerType.disk_gb} GB disk`}
                            </p>
                        )}
                        {serverTypesQ.data && serverTypesQ.data.length === 0 && (
                            <p className="text-[11px] text-amber-700">
                                Backend endpoint not yet available - using built-in fallback list.
                            </p>
                        )}
                    </div>
                </div>
            </Section>

            <Section title="Capacity">
                <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                    <NumberField
                        id="t-count"
                        label="Number of servers"
                        min={1}
                        value={value.config.server_count}
                        onChange={(n) => setConfig({ server_count: n })}
                    />
                    <NumberField
                        id="t-ipv4"
                        label="IPv4 per server"
                        min={1}
                        value={value.config.ipv4_per_server}
                        onChange={(n) => setConfig({ ipv4_per_server: n })}
                        hint={
                            value.config.ipv4_per_server === 1
                                ? "+1 IPv4 included free with the box"
                                : `${value.config.ipv4_per_server - 1} extra IPv4 per server`
                        }
                    />
                    <NumberField
                        id="t-ipv6"
                        label="IPv6 per server"
                        min={0}
                        value={value.config.ipv6_per_server}
                        onChange={(n) => setConfig({ ipv6_per_server: n })}
                        hint="IPv6 is free on Hetzner"
                    />
                </div>
            </Section>

            <Section title="Worker config">
                <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                    <div className="space-y-1.5">
                        <Label htmlFor="t-tier">Worker tier</Label>
                        <Select
                            value={value.config.worker_tier}
                            onValueChange={(v) =>
                                setConfig({ worker_tier: v as ProvisioningWorkerTier })
                            }
                        >
                            <SelectTrigger id="t-tier">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {TIERS.map((t) => (
                                    <SelectItem key={t.value} value={t.value}>
                                        {t.label}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    <div className="space-y-1.5">
                        <Label htmlFor="t-profile">Worker profile</Label>
                        <Select
                            value={value.config.worker_profile_id ?? "__none"}
                            onValueChange={(v) =>
                                setConfig({
                                    worker_profile_id: v === "__none" ? undefined : v,
                                })
                            }
                        >
                            <SelectTrigger id="t-profile">
                                <SelectValue placeholder="Default" />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="__none">Default</SelectItem>
                                {(profilesQ.data ?? []).map((p) => (
                                    <SelectItem key={p.id} value={p.id}>
                                        {p.name}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                        {profilesQ.data && profilesQ.data.length === 0 && (
                            <p className="text-[11px] text-amber-700">
                                /admin/worker-profiles not yet available.
                            </p>
                        )}
                    </div>

                    <div className="space-y-1.5">
                        <Label htmlFor="t-egress">Egress kind</Label>
                        <Select
                            value={value.config.egress_kind}
                            onValueChange={(v) =>
                                setConfig({ egress_kind: v as ProvisioningEgressKind })
                            }
                        >
                            <SelectTrigger id="t-egress">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {EGRESS.map((e) => (
                                    <SelectItem key={e.value} value={e.value}>
                                        {e.label}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>
                </div>
            </Section>

            <div>
                <button
                    type="button"
                    onClick={() => setShowAdvanced((s) => !s)}
                    className="flex items-center gap-1.5 text-sm font-medium text-foreground hover:text-[var(--admin-accent-strong)]"
                >
                    {showAdvanced ? (
                        <ChevronDown className="size-4" />
                    ) : (
                        <ChevronRight className="size-4" />
                    )}
                    Advanced
                </button>
                {showAdvanced && (
                    <div className="mt-3 space-y-3 border-l-2 border-[var(--admin-accent)] pl-4">
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                            <TextField
                                id="t-image"
                                label="Image"
                                value={value.config.image}
                                onChange={(s) => setConfig({ image: s })}
                                hint="OS image slug, e.g. ubuntu-22.04"
                            />
                            <TextField
                                id="t-dc"
                                label="Datacenter override"
                                value={value.config.datacenter ?? ""}
                                onChange={(s) =>
                                    setConfig({ datacenter: s ? s : undefined })
                                }
                                hint="Override the auto-picked datacenter, e.g. fsn1-dc14"
                            />
                            <TextField
                                id="t-pg"
                                label="Placement group"
                                value={value.config.placement_group ?? ""}
                                onChange={(s) =>
                                    setConfig({ placement_group: s ? s : undefined })
                                }
                                hint="Spread servers across hardware hosts"
                            />
                            <TextField
                                id="t-pn"
                                label="Private network"
                                value={value.config.private_network ?? ""}
                                onChange={(s) =>
                                    setConfig({ private_network: s ? s : undefined })
                                }
                                hint="Attach servers to this private network"
                            />
                            <TextField
                                id="t-fw"
                                label="Firewall"
                                value={value.config.firewall}
                                onChange={(s) => setConfig({ firewall: s })}
                            />
                        </div>

                        <LabelEditor
                            labels={value.config.labels}
                            onChange={(labels) => setConfig({ labels })}
                        />
                    </div>
                )}
            </div>

            {mode === "template" && (
                <Section title="Behaviour">
                    <div className="space-y-2 text-sm">
                        {TIERS.map((t) => {
                            const claimed = claimedTiers.has(t.value);
                            const checked = value.auto_provision_tier === t.value;
                            return (
                                <label
                                    key={t.value}
                                    className={`flex items-start gap-2 ${
                                        claimed && !checked ? "opacity-50" : ""
                                    }`}
                                >
                                    <Checkbox
                                        checked={checked}
                                        disabled={claimed && !checked}
                                        onCheckedChange={(c) =>
                                            set({
                                                auto_provision_tier: c
                                                    ? t.value
                                                    : value.auto_provision_tier === t.value
                                                      ? ""
                                                      : value.auto_provision_tier,
                                            })
                                        }
                                    />
                                    <span className="text-sm">
                                        Is the auto-provision template for{" "}
                                        <strong>{t.label}</strong>
                                        {claimed && !checked && (
                                            <span className="text-[11px] text-muted-foreground ml-1">
                                                (already claimed by another template)
                                            </span>
                                        )}
                                    </span>
                                </label>
                            );
                        })}
                    </div>
                </Section>
            )}

            <CostPreview
                serverTotal={cost.serverTotal}
                ipTotal={cost.ipTotal}
                total={cost.total}
                extraIps={cost.extraIps}
                totalIpSlots={totalIpSlots}
                serverCount={value.config.server_count}
                ipv4PerServer={value.config.ipv4_per_server}
            />
        </div>
    );
}

// --------------------------------------------------------------------
// Sub-components
// --------------------------------------------------------------------

function Section({
    title,
    children,
}: {
    title: string;
    children: React.ReactNode;
}) {
    return (
        <div className="space-y-3">
            <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                {title}
            </div>
            {children}
        </div>
    );
}

function NumberField({
    id,
    label,
    value,
    onChange,
    min = 0,
    hint,
}: {
    id: string;
    label: string;
    value: number;
    onChange: (n: number) => void;
    min?: number;
    hint?: string;
}) {
    return (
        <div className="space-y-1.5">
            <Label htmlFor={id}>{label}</Label>
            <Input
                id={id}
                type="number"
                min={min}
                value={value}
                onChange={(e) => {
                    const n = Number(e.target.value);
                    onChange(Number.isFinite(n) ? Math.max(min, Math.floor(n)) : min);
                }}
            />
            {hint && <p className="text-[11px] text-muted-foreground">{hint}</p>}
        </div>
    );
}

function TextField({
    id,
    label,
    value,
    onChange,
    hint,
}: {
    id: string;
    label: string;
    value: string;
    onChange: (s: string) => void;
    hint?: string;
}) {
    return (
        <div className="space-y-1.5">
            <Label htmlFor={id}>{label}</Label>
            <Input id={id} value={value} onChange={(e) => onChange(e.target.value)} />
            {hint && <p className="text-[11px] text-muted-foreground">{hint}</p>}
        </div>
    );
}

function LabelEditor({
    labels,
    onChange,
}: {
    labels: { key: string; value: string }[];
    onChange: (next: { key: string; value: string }[]) => void;
}) {
    return (
        <div className="space-y-1.5">
            <Label>Labels</Label>
            <div className="space-y-2">
                {labels.map((l, i) => (
                    <div key={i} className="flex items-center gap-2">
                        <Input
                            placeholder="key"
                            value={l.key}
                            className="font-mono text-xs"
                            onChange={(e) => {
                                const next = [...labels];
                                next[i] = { ...next[i], key: e.target.value };
                                onChange(next);
                            }}
                        />
                        <Input
                            placeholder="value"
                            value={l.value}
                            className="font-mono text-xs"
                            onChange={(e) => {
                                const next = [...labels];
                                next[i] = { ...next[i], value: e.target.value };
                                onChange(next);
                            }}
                        />
                        <Button
                            size="icon-sm"
                            variant="ghost"
                            onClick={() => onChange(labels.filter((_, j) => j !== i))}
                        >
                            <Trash2 className="size-4" />
                        </Button>
                    </div>
                ))}
                <Button
                    size="sm"
                    variant="outline"
                    onClick={() => onChange([...labels, { key: "", value: "" }])}
                >
                    <Plus className="size-4" />
                    Add label
                </Button>
            </div>
        </div>
    );
}

function CostPreview({
    serverTotal,
    ipTotal,
    total,
    extraIps,
    totalIpSlots,
    serverCount,
    ipv4PerServer,
}: {
    serverTotal: number;
    ipTotal: number;
    total: number;
    extraIps: number;
    totalIpSlots: number;
    serverCount: number;
    ipv4PerServer: number;
}) {
    return (
        <div className="rounded-lg border border-[var(--admin-accent)]/30 bg-[var(--admin-accent-soft)]/40 p-4 space-y-3">
            <div className="flex items-start gap-2">
                <Info className="size-4 text-[var(--admin-accent-strong)] mt-0.5" />
                <div className="text-sm font-medium text-[var(--admin-accent-strong)]">
                    Provisioning preview
                </div>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 text-sm">
                <div className="space-y-1">
                    <Row label="Servers" value={`${serverCount}x`} />
                    <Row label="Server subtotal" value={fmt(serverTotal)} />
                    <Row
                        label={`Extra IPv4 (${extraIps})`}
                        value={fmt(ipTotal)}
                    />
                    <div className="border-t border-[var(--admin-accent)]/30 pt-1 mt-1">
                        <Row label="Estimated monthly" value={fmt(total)} bold />
                    </div>
                </div>
                <div className="space-y-1 text-xs text-foreground/80">
                    <div>
                        Total IP slots: <strong>{totalIpSlots}</strong>{" "}
                        <span className="text-muted-foreground">
                            = {ipv4PerServer} worker egress{ipv4PerServer === 1 ? "" : "es"}{" "}
                            per box
                        </span>
                    </div>
                    <div className="text-muted-foreground">
                        Hetzner includes 1 IPv4 free with each box; only the extras above
                        cost money.
                    </div>
                    <div>
                        <Badge variant="outline" className="text-[10px]">
                            preview only
                        </Badge>
                        <span className="ml-2 text-muted-foreground">
                            Real billing depends on provider invoices.
                        </span>
                    </div>
                </div>
            </div>
        </div>
    );
}

function Row({
    label,
    value,
    bold,
}: {
    label: string;
    value: string;
    bold?: boolean;
}) {
    return (
        <div className="flex items-center justify-between">
            <span className="text-xs text-muted-foreground">{label}</span>
            <span
                className={
                    bold ? "tabular-nums font-semibold" : "tabular-nums text-sm"
                }
            >
                {value}
            </span>
        </div>
    );
}

function fmt(n: number): string {
    return n.toLocaleString(undefined, {
        style: "currency",
        currency: "EUR",
        maximumFractionDigits: 2,
    });
}

// --------------------------------------------------------------------
// Fallback catalog used while the backend endpoints aren't wired yet.
// Kept conservative - the operator can still produce a valid template
// against these defaults; the backend will reject anything stale at
// job-create time.
// --------------------------------------------------------------------

const FALLBACK_LOCATIONS = [
    { name: "fsn1", description: "Falkenstein (DE)", city: "Falkenstein", country: "DE" },
    { name: "nbg1", description: "Nuremberg (DE)", city: "Nuremberg", country: "DE" },
    { name: "hel1", description: "Helsinki (FI)", city: "Helsinki", country: "FI" },
    { name: "ash", description: "Ashburn (US Virginia)", city: "Ashburn", country: "US" },
    { name: "hil", description: "Hillsboro (US Oregon)", city: "Hillsboro", country: "US" },
    { name: "sin", description: "Singapore (SG)", city: "Singapore", country: "SG" },
];

const FALLBACK_SERVER_TYPES: HetznerServerType[] = [
    {
        name: "cx22",
        description: "2 vCPU shared, 4 GB RAM",
        cores: 2,
        memory_gb: 4,
        disk_gb: 40,
        cpu_type: "shared",
        price_monthly_eur: 4.59,
        price_ipv4_monthly_eur: 0.6,
    },
    {
        name: "cx32",
        description: "4 vCPU shared, 8 GB RAM",
        cores: 4,
        memory_gb: 8,
        disk_gb: 80,
        cpu_type: "shared",
        price_monthly_eur: 7.59,
        price_ipv4_monthly_eur: 0.6,
    },
    {
        name: "cpx21",
        description: "3 vCPU AMD, 4 GB RAM",
        cores: 3,
        memory_gb: 4,
        disk_gb: 80,
        cpu_type: "shared",
        price_monthly_eur: 8.46,
        price_ipv4_monthly_eur: 0.6,
    },
    {
        name: "ccx13",
        description: "2 vCPU dedicated, 8 GB RAM",
        cores: 2,
        memory_gb: 8,
        disk_gb: 80,
        cpu_type: "dedicated",
        price_monthly_eur: 14.86,
        price_ipv4_monthly_eur: 0.6,
    },
];
