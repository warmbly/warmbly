// "Copy to workspace": send a campaign or templates to the caller's other
// workspaces (an agency's clients, usually) as drafts. Two steps: pick the
// workspaces, then a summary with a way into each copy.

import React from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "@tanstack/react-router";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertTriangleIcon,
    ArrowRightIcon,
    CheckIcon,
    CopyIcon,
    Loader2Icon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast/headless";
import { Checkbox } from "@/components/ui/checkbox";
import { SearchInput } from "@/components/ui/field";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { useAppStore } from "@/stores";
import type { Organization } from "@/stores/slices/organizationSlice";
import { isAccessRestricted, orgHasPermission } from "@/hooks/usePermission";
import useSwitchOrganization from "@/lib/api/hooks/app/organizations/useSwitchOrganization";
import useTemplates from "@/lib/api/hooks/app/templates/useTemplates";
import {
    useAgencyGroups,
    useCopyCampaignToWorkspaces,
    useCopyTemplatesToWorkspaces,
} from "@/lib/api/hooks/app/workspaceCopy/useCopyToWorkspaces";
import {
    COPY_DESTINATIONS_MAX,
    COPY_TEMPLATES_MAX,
    type WorkspaceCopyResult,
} from "@/lib/api/models/app/workspaceCopy/WorkspaceCopy";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

export type CopySubject =
    | { kind: "campaign"; campaign: { id: string; name: string } }
    | { kind: "templates"; templates: { id: string; name: string }[] };

interface Props {
    open: boolean;
    onClose: () => void;
    subject: CopySubject | null;
}

// What each cleared step setting is called on screen.
const FIELD_LABELS: Record<string, string> = {
    category_id: "tag",
    segment_id: "segment",
    label_ids: "inbox labels",
    task_assigned_to: "task assignee",
    task_assigned_team_id: "task team",
    deal_pipeline_id: "deal pipeline",
    deal_stage_id: "deal stage",
    automation_id: "automation",
    ai_add_tags: "AI tag choices",
    ai_remove_tags: "AI tag choices",
    ai_labels: "AI label choices",
    ai_allowed_actions: "AI actions",
    form_link: "form link",
};

// randomUUID only exists in secure contexts; a plain-http self-host still needs a key.
function newKey(): string {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") return crypto.randomUUID();
    return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`;
}

function canCreateIn(org: Organization): boolean {
    return !isAccessRestricted(org) && orgHasPermission(org, "MANAGE_CAMPAIGNS");
}

// Portalled, because a list row is a link: the dialog must not sit inside it.
export default function CopyToWorkspaceDialog({ open, onClose, subject }: Props) {
    if (typeof document === "undefined") return null;
    return createPortal(
        <AnimatePresence>
            {open && subject && <DialogBody key="copy-to-workspace" subject={subject} onClose={onClose} />}
        </AnimatePresence>,
        document.body,
    );
}

function DialogBody({ subject, onClose }: { subject: CopySubject; onClose: () => void }) {
    const navigate = useNavigate();
    const organizations = useAppStore((s) => s.organizations);
    const current = useAppStore((s) => s.currentOrganization);
    const setCurrentOrganization = useAppStore((s) => s.setCurrentOrganization);
    const switchOrg = useSwitchOrganization();
    const copyCampaign = useCopyCampaignToWorkspaces();
    const copyTemplates = useCopyTemplatesToWorkspaces();

    const [step, setStep] = React.useState<"pick" | "done">("pick");
    const [search, setSearch] = React.useState("");
    const [selected, setSelected] = React.useState<string[]>([]);
    const [withTemplates, setWithTemplates] = React.useState(false);
    const [templateIds, setTemplateIds] = React.useState<string[]>([]);
    const [results, setResults] = React.useState<WorkspaceCopyResult[]>([]);
    // One key per submission: a retry of the same click replays instead of copying twice.
    const [submissionKey, setSubmissionKey] = React.useState(() => newKey());

    const others = React.useMemo(
        () => organizations.filter((o) => o.id !== current?.id),
        [organizations, current?.id],
    );
    const agencyGroups = useAgencyGroups(others.length > 0);
    const restrictedHere = isAccessRestricted(current);
    const templatesQuery = useTemplates();
    const allTemplates = templatesQuery.data ?? [];
    const pending = copyCampaign.isPending || copyTemplates.isPending;

    // Escape closes the innermost layer only.
    React.useEffect(() => {
        function onKey(e: KeyboardEvent) {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            if (!pending) onClose();
        }
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [onClose, pending]);

    const groups = React.useMemo(() => {
        const q = search.trim().toLowerCase();
        const visible = others.filter((o) => !q || o.name.toLowerCase().includes(q));
        const taken = new Set<string>();
        const out: { label: string | null; orgs: Organization[] }[] = [];
        for (const g of agencyGroups.data ?? []) {
            const orgs = visible.filter((o) => g.organizationIds.includes(o.id) && !taken.has(o.id));
            orgs.forEach((o) => taken.add(o.id));
            if (orgs.length > 0) out.push({ label: g.name, orgs });
        }
        const rest = visible.filter((o) => !taken.has(o.id));
        if (rest.length > 0) out.push({ label: out.length > 0 ? "Other workspaces" : null, orgs: rest });
        return out;
    }, [others, search, agencyGroups.data]);

    function toggleOrg(id: string) {
        setSelected((prev) => {
            if (prev.includes(id)) return prev.filter((x) => x !== id);
            if (prev.length >= COPY_DESTINATIONS_MAX) {
                toast.error(`Pick up to ${COPY_DESTINATIONS_MAX} workspaces at a time`);
                return prev;
            }
            return [...prev, id];
        });
        setSubmissionKey(newKey());
    }

    function setIncludeTemplates(on: boolean) {
        setWithTemplates(on);
        setTemplateIds(on ? allTemplates.slice(0, COPY_TEMPLATES_MAX).map((t) => t.id) : []);
        setSubmissionKey(newKey());
    }

    function toggleTemplate(id: string) {
        setTemplateIds((prev) => {
            if (prev.includes(id)) return prev.filter((x) => x !== id);
            if (prev.length >= COPY_TEMPLATES_MAX) return prev;
            return [...prev, id];
        });
        setSubmissionKey(newKey());
    }

    const blocked =
        selected.length === 0
            ? "Pick at least one workspace"
            : subject.kind === "campaign" && withTemplates && templateIds.length === 0
              ? "Pick the templates to include, or turn templates off"
              : null;

    async function submit() {
        if (blocked || pending) return;
        try {
            const resp =
                subject.kind === "campaign"
                    ? await copyCampaign.mutateAsync({
                          campaignId: subject.campaign.id,
                          organizationIds: selected,
                          templateIds: withTemplates ? templateIds : [],
                          key: submissionKey,
                      })
                    : await copyTemplates.mutateAsync({
                          templateIds: subject.templates.map((t) => t.id),
                          organizationIds: selected,
                          key: submissionKey,
                      });
            setResults(resp.data);
            setStep("done");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    async function openCopy(r: WorkspaceCopyResult) {
        const org = organizations.find((o) => o.id === r.organization_id);
        try {
            if (r.organization_id !== current?.id) {
                await switchOrg.mutateAsync(r.organization_id);
                if (org) setCurrentOrganization(org);
            }
            onClose();
            if (r.campaign) {
                navigate({ to: "/app/campaigns/$id/preferences", params: { id: r.campaign.id } });
            } else {
                navigate({ to: "/app/templates" });
            }
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    const title = subject.kind === "campaign" ? subject.campaign.name : templatesTitle(subject.templates);
    const copiedCount = results.filter((r) => r.status !== "failed").length;

    return (
        <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            onMouseDown={(e) => {
                if (e.target === e.currentTarget && !pending) onClose();
            }}
            // React bubbles portalled events to the row that opened this; keep them here.
            onClick={(e) => e.stopPropagation()}
            className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
        >
            <motion.div
                initial={{ y: 8, opacity: 0 }}
                animate={{ y: 0, opacity: 1 }}
                exit={{ y: 8, opacity: 0 }}
                transition={{ duration: 0.16 }}
                role="dialog"
                aria-modal="true"
                aria-label={`Copy ${title} to another workspace`}
                className="w-full max-w-[480px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[calc(100dvh-2rem)]"
            >
                <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                    <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                        <CopyIcon className="w-3 h-3" />
                    </div>
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                        Copy to workspace
                    </span>
                    <div className="h-4 w-px bg-slate-200" />
                    <span className="text-[12.5px] text-slate-900 font-medium truncate">{title}</span>
                    <button
                        type="button"
                        onClick={onClose}
                        disabled={pending}
                        aria-label="Close"
                        className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-50"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </div>

                <div className="relative flex-1 min-h-0 overflow-hidden flex flex-col">
                    <AnimatePresence mode="wait" initial={false}>
                        {step === "pick" ? (
                            <motion.div
                                key="pick"
                                initial={{ x: -24, opacity: 0 }}
                                animate={{ x: 0, opacity: 1 }}
                                exit={{ x: -24, opacity: 0 }}
                                transition={{ duration: 0.16 }}
                                className="flex-1 min-h-0 overflow-y-auto px-4 py-4 space-y-3"
                            >
                                <p className="text-[11.5px] text-slate-500 leading-relaxed">
                                    {subject.kind === "campaign"
                                        ? "Each workspace gets a draft with the same steps, branches, settings and schedule. Leads, results, mailboxes and tracking domains stay here."
                                        : "Each workspace gets its own copy of the templates, images included."}
                                </p>

                                {others.length === 0 ? (
                                    <div className="rounded-md border border-dashed border-slate-300 bg-slate-50/40 px-3 py-5 text-center text-[12px] text-slate-500">
                                        You belong to no other workspace yet.
                                    </div>
                                ) : (
                                    <>
                                        <SearchInput value={search} onChange={setSearch} placeholder="Search workspaces…" autoFocus />
                                        <div className="rounded-md border border-slate-200 divide-y divide-slate-100 max-h-[260px] overflow-y-auto">
                                            {groups.length === 0 && (
                                                <div className="px-3 py-3 text-[12px] text-slate-400">No workspace matches.</div>
                                            )}
                                            {groups.map((g) => (
                                                <div key={g.label ?? "all"}>
                                                    {g.label && (
                                                        <div className="px-3 pt-2 pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                                            {g.label}
                                                        </div>
                                                    )}
                                                    {g.orgs.map((o) => (
                                                        <WorkspaceRow
                                                            key={o.id}
                                                            org={o}
                                                            checked={selected.includes(o.id)}
                                                            onToggle={() => toggleOrg(o.id)}
                                                        />
                                                    ))}
                                                </div>
                                            ))}
                                        </div>
                                        <div className="text-[11px] text-slate-400 font-mono tabular-nums">
                                            {selected.length} of {COPY_DESTINATIONS_MAX} selected
                                        </div>
                                    </>
                                )}

                                {subject.kind === "campaign" && !restrictedHere && allTemplates.length > 0 && (
                                    <div className="rounded-md border border-slate-200">
                                        <div
                                            className="px-3 h-10 flex items-center gap-2 cursor-pointer"
                                            onClick={() => setIncludeTemplates(!withTemplates)}
                                        >
                                            <span className="text-[12.5px] text-slate-900 flex-1">Also copy templates</span>
                                            <span onClick={(e) => e.stopPropagation()}>
                                                <Toggle value={withTemplates} onChange={setIncludeTemplates} ariaLabel="Also copy templates" />
                                            </span>
                                        </div>
                                        {withTemplates && (
                                            <div className="border-t border-slate-100 max-h-[160px] overflow-y-auto py-1">
                                                {allTemplates.map((t) => (
                                                    <label key={t.id} className="px-3 h-8 flex items-center gap-2 cursor-pointer hover:bg-slate-50">
                                                        <Checkbox
                                                            tone="slate"
                                                            checked={templateIds.includes(t.id)}
                                                            onChange={() => toggleTemplate(t.id)}
                                                        />
                                                        <span className="text-[12px] text-slate-700 truncate">{t.name}</span>
                                                    </label>
                                                ))}
                                            </div>
                                        )}
                                    </div>
                                )}
                            </motion.div>
                        ) : (
                            <motion.div
                                key="done"
                                initial={{ x: 24, opacity: 0 }}
                                animate={{ x: 0, opacity: 1 }}
                                exit={{ x: 24, opacity: 0 }}
                                transition={{ duration: 0.16 }}
                                className="flex-1 min-h-0 overflow-y-auto px-4 py-4 space-y-2"
                            >
                                <p className="text-[11.5px] text-slate-500 leading-relaxed">
                                    {copiedCount === results.length
                                        ? `Copied to ${copiedCount} ${copiedCount === 1 ? "workspace" : "workspaces"}.`
                                        : `Copied to ${copiedCount} of ${results.length} workspaces.`}
                                    {subject.kind === "campaign" && copiedCount > 0 && " Each copy is a draft: add leads and mailboxes there before starting it."}
                                </p>
                                {results.map((r) => (
                                    <ResultRow
                                        key={r.organization_id}
                                        result={r}
                                        name={r.organization_name || organizations.find((o) => o.id === r.organization_id)?.name || "Workspace"}
                                        opening={switchOrg.isPending}
                                        onOpen={() => openCopy(r)}
                                    />
                                ))}
                            </motion.div>
                        )}
                    </AnimatePresence>
                </div>

                <div className="px-3 h-12 border-t border-slate-200 flex items-center gap-1.5 shrink-0">
                    {step === "pick" ? (
                        <>
                            {blocked && others.length > 0 && (
                                <span className="text-[11px] text-slate-400 truncate pl-1">{blocked}</span>
                            )}
                            <button
                                type="button"
                                onClick={onClose}
                                disabled={pending}
                                className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={submit}
                                disabled={!!blocked || pending}
                                title={blocked ?? undefined}
                                className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {pending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                {selected.length > 1 ? `Copy to ${selected.length} workspaces` : "Copy"}
                            </button>
                        </>
                    ) : (
                        <button
                            type="button"
                            onClick={onClose}
                            className="ml-auto h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium transition-colors"
                        >
                            Done
                        </button>
                    )}
                </div>
            </motion.div>
        </motion.div>
    );
}

function WorkspaceRow({ org, checked, onToggle }: { org: Organization; checked: boolean; onToggle: () => void }) {
    const allowed = canCreateIn(org);
    return (
        <label
            className={cn(
                "px-3 h-9 flex items-center gap-2.5",
                allowed ? "cursor-pointer hover:bg-slate-50" : "cursor-not-allowed opacity-60",
            )}
            title={allowed ? undefined : "You need to manage campaigns there, with access to the whole workspace"}
        >
            <Checkbox checked={checked} disabled={!allowed} onChange={onToggle} />
            <span className="text-[12.5px] text-slate-900 truncate flex-1">{org.name}</span>
            {!allowed && <span className="text-[11px] text-slate-400 shrink-0">No create access</span>}
        </label>
    );
}

function ResultRow({
    result,
    name,
    opening,
    onOpen,
}: {
    result: WorkspaceCopyResult;
    name: string;
    opening: boolean;
    onOpen: () => void;
}) {
    const failed = result.status === "failed";
    const fields = [...new Set(result.review.flatMap((r) => r.fields.map((f) => FIELD_LABELS[f] ?? f)))];
    return (
        <div className="rounded-md border border-slate-200 px-3 py-2.5">
            <div className="flex items-center gap-2">
                <span
                    className={cn(
                        "size-4 rounded-full flex items-center justify-center shrink-0",
                        failed ? "bg-rose-50 text-rose-600" : result.status === "partial" ? "bg-amber-50 text-amber-600" : "bg-emerald-50 text-emerald-600",
                    )}
                >
                    {failed ? <XIcon className="w-2.5 h-2.5" /> : result.status === "partial" ? <AlertTriangleIcon className="w-2.5 h-2.5" /> : <CheckIcon className="w-2.5 h-2.5" />}
                </span>
                <span className="text-[12.5px] font-medium text-slate-900 truncate flex-1">{name}</span>
                {!failed && (
                    <button
                        type="button"
                        onClick={onOpen}
                        disabled={opening}
                        className="h-6 px-2 rounded text-[11.5px] text-sky-700 hover:bg-sky-50 inline-flex items-center gap-1 shrink-0 disabled:opacity-50"
                    >
                        {result.campaign ? "Open draft" : "Open templates"}
                        <ArrowRightIcon className="w-3 h-3" />
                    </button>
                )}
            </div>
            <div className="mt-1 pl-6 space-y-0.5 text-[11.5px] leading-relaxed">
                {failed && result.error && <div className="text-rose-600">{result.error.message}</div>}
                {!failed && (
                    <div className="text-slate-500">
                        {[
                            result.campaign ? `Campaign "${result.campaign.name}"` : null,
                            result.templates.length > 0
                                ? `${result.templates.length} ${result.templates.length === 1 ? "template" : "templates"}`
                                : null,
                        ]
                            .filter(Boolean)
                            .join(", ")}
                    </div>
                )}
                {fields.length > 0 && (
                    <div className="text-amber-700">
                        Pick again in the copy: {fields.join(", ")}. They pointed at things only the original workspace has.
                    </div>
                )}
                {result.warnings.map((w) => (
                    <div key={w} className="text-amber-700">{w}</div>
                ))}
            </div>
        </div>
    );
}

function templatesTitle(templates: { name: string }[]): string {
    if (templates.length === 1) return templates[0].name;
    return `${templates.length} templates`;
}
