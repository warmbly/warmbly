// "Provision new" modal. Tabs:
//   - From template: dropdown + cost preview + Provision now
//   - Custom: the same form as the template editor; can save as template
// After submission, switches into the live progress panel polling
// /admin/provisioning-jobs/:id.

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
    CheckCircle2,
    Loader2,
    Rocket,
    Send,
    XCircle,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

import {
    createProvisioningJob,
    getProvisioningJob,
    listProvisioningTemplates,
} from "@/lib/api/client/admin/provisioning";
import {
    listHetznerServerTypes,
} from "@/lib/api/client/admin/cloud";
import type {
    ProvisioningJob,
    ProvisioningJobState,
    ProvisioningTemplate,
} from "@/lib/api/models/admin";

import {
    ProvisioningTemplateForm,
    makeEmptyFormValue,
    type TemplateFormValue,
} from "@/app/settings/ProvisioningTemplateForm";

interface Props {
    open: boolean;
    onOpenChange: (next: boolean) => void;
    /** Called once a job is created so the parent can invalidate lists. */
    onJobCreated?: (job: ProvisioningJob) => void;
}

type Stage =
    | { kind: "form" }
    | { kind: "running"; jobId: string };

export function ProvisionModal({ open, onOpenChange, onJobCreated }: Props) {
    const [tab, setTab] = useState<"template" | "custom">("template");
    const [stage, setStage] = useState<Stage>({ kind: "form" });
    const [selectedTplId, setSelectedTplId] = useState<string>("");
    const [customForm, setCustomForm] =
        useState<TemplateFormValue>(makeEmptyFormValue);
    const [saveAsTemplate, setSaveAsTemplate] = useState(false);
    const [saveTemplateName, setSaveTemplateName] = useState("");

    // Reset on close to avoid stale state next open.
    useEffect(() => {
        if (!open) {
            setStage({ kind: "form" });
            setTab("template");
            setCustomForm(makeEmptyFormValue());
            setSaveAsTemplate(false);
            setSaveTemplateName("");
            setSelectedTplId("");
        }
    }, [open]);

    const templatesQ = useQuery({
        queryKey: ["admin", "provisioning-templates"],
        queryFn: listProvisioningTemplates,
        enabled: open,
        retry: false,
    });

    const selectedTpl: ProvisioningTemplate | undefined = useMemo(
        () => templatesQ.data?.find((t) => t.id === selectedTplId),
        [templatesQ.data, selectedTplId],
    );

    const submitMut = useMutation({
        mutationFn: () => {
            if (tab === "template") {
                if (!selectedTplId) {
                    throw new Error("Pick a template first");
                }
                return createProvisioningJob({ template_id: selectedTplId });
            }
            return createProvisioningJob({
                config: customForm.config,
                save_as_template:
                    saveAsTemplate && saveTemplateName.trim()
                        ? {
                            name: saveTemplateName.trim(),
                            description: customForm.description || undefined,
                        }
                        : undefined,
            });
        },
        onSuccess: (job) => {
            toast.success("Provisioning job created");
            setStage({ kind: "running", jobId: job.id });
            onJobCreated?.(job);
        },
        onError: (e: Error) => toast.error(e.message),
    });

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent
                className="sm:max-w-3xl max-h-[90vh] overflow-y-auto"
                showCloseButton={stage.kind === "form"}
            >
                {stage.kind === "form" ? (
                    <>
                        <DialogHeader>
                            <DialogTitle className="flex items-center gap-2">
                                <Rocket className="size-5 text-[var(--admin-accent)]" />
                                Provision new worker box
                            </DialogTitle>
                            <DialogDescription>
                                Spin up new Hetzner machines, install the worker binary, and
                                register them with the control plane. Start from a saved
                                template or hand-roll a one-off.
                            </DialogDescription>
                        </DialogHeader>

                        <Tabs
                            value={tab}
                            onValueChange={(v) => setTab(v as "template" | "custom")}
                        >
                            <TabsList className="w-full">
                                <TabsTrigger value="template">From template</TabsTrigger>
                                <TabsTrigger value="custom">Custom</TabsTrigger>
                            </TabsList>

                            <TabsContent value="template" className="pt-3 space-y-3">
                                {templatesQ.isLoading && <Skeleton className="h-9 w-full" />}
                                {!templatesQ.isLoading &&
                                    (templatesQ.data ?? []).length === 0 && (
                                        <div className="rounded-md border border-dashed border-border bg-muted/30 p-4 text-sm">
                                            <div className="font-medium">No templates yet</div>
                                            <p className="text-xs text-muted-foreground mt-1">
                                                Create one under Settings → Provisioning Templates,
                                                or switch to the Custom tab to launch a one-off.
                                            </p>
                                        </div>
                                    )}
                                {(templatesQ.data ?? []).length > 0 && (
                                    <div className="space-y-1.5">
                                        <Label htmlFor="tpl-pick">Template</Label>
                                        <Select
                                            value={selectedTplId}
                                            onValueChange={setSelectedTplId}
                                        >
                                            <SelectTrigger id="tpl-pick">
                                                <SelectValue placeholder="Pick a template..." />
                                            </SelectTrigger>
                                            <SelectContent>
                                                {(templatesQ.data ?? []).map((t) => (
                                                    <SelectItem key={t.id} value={t.id}>
                                                        {t.name}
                                                        {t.is_draft && " (draft)"}
                                                    </SelectItem>
                                                ))}
                                            </SelectContent>
                                        </Select>
                                    </div>
                                )}

                                {selectedTpl && (
                                    <TemplateSummary tpl={selectedTpl} />
                                )}
                            </TabsContent>

                            <TabsContent value="custom" className="pt-3">
                                <ProvisioningTemplateForm
                                    mode="inline"
                                    value={customForm}
                                    onChange={setCustomForm}
                                />

                                <div className="mt-4 border-t border-border pt-4 space-y-2">
                                    <label className="flex items-start gap-2">
                                        <Checkbox
                                            checked={saveAsTemplate}
                                            onCheckedChange={(c) => setSaveAsTemplate(!!c)}
                                        />
                                        <div className="text-sm">
                                            <div className="font-medium">Save as template</div>
                                            <div className="text-[11px] text-muted-foreground">
                                                Reuse this config later without re-entering every field.
                                            </div>
                                        </div>
                                    </label>
                                    {saveAsTemplate && (
                                        <Input
                                            placeholder="Template name"
                                            value={saveTemplateName}
                                            onChange={(e) =>
                                                setSaveTemplateName(e.target.value)
                                            }
                                            className="ml-6"
                                        />
                                    )}
                                </div>
                            </TabsContent>
                        </Tabs>

                        <DialogFooter>
                            <Button variant="outline" onClick={() => onOpenChange(false)}>
                                Cancel
                            </Button>
                            <Button
                                onClick={() => submitMut.mutate()}
                                disabled={
                                    submitMut.isPending ||
                                    (tab === "template" && !selectedTplId)
                                }
                                className="bg-[var(--admin-accent)] hover:bg-[var(--admin-accent-strong)] text-white"
                            >
                                <Send className="size-4" />
                                {submitMut.isPending ? "Submitting..." : "Provision now"}
                            </Button>
                        </DialogFooter>
                    </>
                ) : (
                    <ProvisionProgressPanel
                        jobId={stage.jobId}
                        onClose={() => onOpenChange(false)}
                    />
                )}
            </DialogContent>
        </Dialog>
    );
}

// --------------------------------------------------------------------
// Template summary card (selected template preview)
// --------------------------------------------------------------------

function TemplateSummary({ tpl }: { tpl: ProvisioningTemplate }) {
    const serverTypesQ = useQuery({
        queryKey: ["admin", "hetzner-server-types"],
        queryFn: listHetznerServerTypes,
        retry: false,
        staleTime: 5 * 60_000,
    });

    const st = serverTypesQ.data?.find(
        (s) => s.name === tpl.config.server_type,
    );
    const sp = st?.price_monthly_eur ?? 0;
    const ipPrice = st?.price_ipv4_monthly_eur ?? 0.5;
    const extraIps =
        Math.max(0, tpl.config.ipv4_per_server - 1) * tpl.config.server_count;
    const cost = sp * tpl.config.server_count + ipPrice * extraIps;

    return (
        <div className="rounded-lg border border-[var(--admin-accent)]/30 bg-[var(--admin-accent-soft)]/40 p-4 space-y-3">
            <div className="text-xs uppercase tracking-wider text-[var(--admin-accent-strong)] font-semibold">
                Preview
            </div>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3 text-xs">
                <KVMini label="Provider" value={tpl.config.provider} />
                <KVMini label="Location" value={tpl.config.location} />
                <KVMini label="Server type" value={tpl.config.server_type} />
                <KVMini label="Servers" value={`${tpl.config.server_count}x`} />
                <KVMini label="IPv4 / server" value={String(tpl.config.ipv4_per_server)} />
                <KVMini label="Tier" value={tpl.config.worker_tier} />
                <KVMini label="Egress" value={tpl.config.egress_kind} />
                <KVMini label="Image" value={tpl.config.image} />
            </div>
            <div className="flex items-center justify-between border-t border-[var(--admin-accent)]/30 pt-2">
                <span className="text-xs text-muted-foreground">
                    Estimated monthly
                </span>
                <span className="text-sm font-semibold tabular-nums">
                    {cost.toLocaleString(undefined, {
                        style: "currency",
                        currency: "EUR",
                        maximumFractionDigits: 2,
                    })}
                </span>
            </div>
        </div>
    );
}

function KVMini({ label, value }: { label: string; value: string }) {
    return (
        <div>
            <div className="uppercase tracking-wide text-[9px] text-muted-foreground">
                {label}
            </div>
            <div className="font-mono text-[11px]">{value}</div>
        </div>
    );
}

// --------------------------------------------------------------------
// Live progress
// --------------------------------------------------------------------

const STATE_LABEL: Record<ProvisioningJobState, string> = {
    pending: "Pending",
    creating_server: "Creating server",
    creating_ips: "Creating IPs",
    assigning_ips: "Assigning IPs",
    setting_rdns: "Setting rDNS",
    installing: "Installing",
    verifying: "Verifying (heartbeats)",
    completed: "Completed",
    failed: "Failed",
};

const STATE_ORDER: ProvisioningJobState[] = [
    "pending",
    "creating_server",
    "creating_ips",
    "assigning_ips",
    "setting_rdns",
    "installing",
    "verifying",
    "completed",
];

export function ProvisionProgressPanel({
    jobId,
    onClose,
}: {
    jobId: string;
    onClose: () => void;
}) {
    const jobQ = useQuery({
        queryKey: ["admin", "provisioning-job", jobId],
        queryFn: () => getProvisioningJob(jobId),
        // Stop polling once the job is terminal.
        refetchInterval: (q) => {
            const j = q.state.data;
            if (j && (j.state === "completed" || j.state === "failed")) return false;
            return 2000;
        },
        retry: false,
    });

    const job = jobQ.data;

    return (
        <>
            <DialogHeader>
                <DialogTitle className="flex items-center gap-2">
                    <Rocket className="size-5 text-[var(--admin-accent)]" />
                    Provisioning job
                </DialogTitle>
                <DialogDescription>
                    Job ID: <code className="font-mono text-[11px]">{jobId}</code>
                </DialogDescription>
            </DialogHeader>

            <div className="space-y-3">
                {jobQ.isLoading && <Skeleton className="h-32 w-full" />}
                {!jobQ.isLoading && !job && (
                    <div className="rounded-md border border-dashed border-border bg-muted/30 p-4 text-sm">
                        <div className="font-medium">Backend endpoint not yet available</div>
                        <p className="text-xs text-muted-foreground mt-1">
                            The job was created, but the live status endpoint
                            (<code>/admin/provisioning-jobs/:id</code>) hasn't returned yet.
                        </p>
                    </div>
                )}
                {job && <JobProgressTimeline job={job} />}
            </div>

            <DialogFooter>
                <Button variant="outline" onClick={onClose}>
                    Close
                </Button>
            </DialogFooter>
        </>
    );
}

export function JobProgressTimeline({ job }: { job: ProvisioningJob }) {
    const reachedIdx = STATE_ORDER.indexOf(job.state);
    const isFailed = job.state === "failed";

    return (
        <div className="space-y-3">
            <div className="space-y-1.5">
                {STATE_ORDER.map((s, i) => {
                    const passed = i < reachedIdx || job.state === "completed";
                    const current = i === reachedIdx && !isFailed;
                    const pending = i > reachedIdx;
                    const progress = job.progress.find(
                        (p) =>
                            (s === "creating_ips" && p.key === "ips_created") ||
                            (s === "setting_rdns" && p.key === "rdns_set") ||
                            (s === "verifying" && p.key === "heartbeats_received"),
                    );

                    return (
                        <div
                            key={s}
                            className="flex items-start gap-2.5 text-sm"
                        >
                            <div className="mt-0.5 shrink-0">
                                {passed && (
                                    <CheckCircle2 className="size-4 text-emerald-600" />
                                )}
                                {current && (
                                    <Loader2 className="size-4 text-[var(--admin-accent)] animate-spin" />
                                )}
                                {pending && (
                                    <div className="size-4 rounded-full border-2 border-border" />
                                )}
                            </div>
                            <div className="flex-1 min-w-0">
                                <div
                                    className={
                                        passed
                                            ? "text-foreground"
                                            : current
                                              ? "text-foreground font-medium"
                                              : "text-muted-foreground"
                                    }
                                >
                                    {STATE_LABEL[s]}
                                    {progress && current && (
                                        <span className="text-muted-foreground ml-1">
                                            ({progress.done}/{progress.total})...
                                        </span>
                                    )}
                                </div>
                            </div>
                        </div>
                    );
                })}

                {isFailed && (
                    <div className="flex items-start gap-2.5 text-sm pt-1">
                        <XCircle className="size-4 text-red-600 mt-0.5" />
                        <div className="flex-1">
                            <div className="text-red-700 font-medium">Job failed</div>
                            {job.last_error && (
                                <pre className="mt-1 text-[11px] bg-red-50 border border-red-200 rounded p-2 overflow-auto font-mono whitespace-pre-wrap">
                                    {job.last_error}
                                </pre>
                            )}
                        </div>
                    </div>
                )}
            </div>

            {job.state === "completed" && (
                <div className="rounded-md border border-emerald-200 bg-emerald-50/60 p-3 text-sm">
                    <div className="flex items-center gap-2 font-medium text-emerald-700">
                        <CheckCircle2 className="size-4" />
                        Provisioned successfully
                    </div>
                    {job.created_worker_ids && job.created_worker_ids.length > 0 && (
                        <div className="mt-1 text-[11px] text-emerald-800">
                            Created {job.created_worker_ids.length} worker
                            {job.created_worker_ids.length === 1 ? "" : "s"}.{" "}
                            <Badge variant="outline" className="text-[10px]">
                                visible in Workers
                            </Badge>
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}
