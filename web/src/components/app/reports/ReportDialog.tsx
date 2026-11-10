// Create or edit a client report: its name, what it covers, which sections it shows and its default range.

import React from "react";
import { createPortal } from "react-dom";
import { motion } from "framer-motion";
import { FolderIcon, GlobeIcon, InboxIcon, Loader2Icon, LockIcon, MegaphoneIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { FieldError, Label, TextInput } from "@/components/ui/field";
import { Segmented, Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { ResourcePicker } from "@/app/app/settings/members/MemberAccessDialog";
import { useConfirm } from "@/hooks/context/confirm";
import { useUserProfile } from "@/hooks/context/user";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import useEmails from "@/lib/api/hooks/app/emails/useEmails";
import { useCreateReport, useUpdateReport } from "@/lib/api/hooks/app/reports";
import type ClientReport from "@/lib/api/models/app/reports/ClientReport";
import type { ClientReportInput, ClientReportRange, ClientReportSections } from "@/lib/api/models/app/reports/ClientReport";
import type MemberAccess from "@/lib/api/models/app/organizations/MemberAccess";
import { WORKSPACE_ACCESS } from "@/lib/api/models/app/organizations/MemberAccess";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { nameError, normalizeName, WORKSPACE_NAME_MAX } from "@/lib/displayName";
import { cn } from "@/lib/utils";
import { DEFAULT_SECTIONS, RANGE_OPTIONS, SECTION_OPTIONS } from "./reportText";

interface Draft {
    name: string;
    access: MemberAccess;
    sections: ClientReportSections;
    show_reply_snippets: boolean;
    date_range: ClientReportRange;
}

function draftOf(report?: ClientReport | null): Draft {
    const a = report?.access;
    return {
        name: report?.name ?? "",
        access:
            a && a.scope === "restricted"
                ? {
                      scope: "restricted",
                      campaign_folder_ids: a.campaign_folder_ids ?? [],
                      campaign_ids: a.campaign_ids ?? [],
                      email_account_ids: a.email_account_ids ?? [],
                  }
                : { ...WORKSPACE_ACCESS },
        sections: { ...DEFAULT_SECTIONS, ...(report?.sections ?? {}) },
        show_reply_snippets: report?.show_reply_snippets ?? false,
        date_range: report?.date_range ?? "last_30_days",
    };
}

function sameIds(a: string[], b: string[]) {
    if (a.length !== b.length) return false;
    const s = new Set(a);
    return b.every((x) => s.has(x));
}

function sameAccess(a: MemberAccess, b: MemberAccess) {
    if (a.scope !== b.scope) return false;
    if (a.scope !== "restricted") return true;
    return (
        sameIds(a.campaign_folder_ids, b.campaign_folder_ids) &&
        sameIds(a.campaign_ids, b.campaign_ids) &&
        sameIds(a.email_account_ids, b.email_account_ids)
    );
}

function sameSections(a: ClientReportSections, b: ClientReportSections) {
    return SECTION_OPTIONS.every((o) => a[o.key] === b[o.key]);
}

/** The fields that changed, so a PATCH leaves the rest alone. */
function diff(start: Draft, d: Draft): ClientReportInput {
    const out: ClientReportInput = {};
    if (normalizeName(d.name) !== normalizeName(start.name)) out.name = normalizeName(d.name);
    if (!sameAccess(start.access, d.access)) out.access = d.access.scope === "restricted" ? d.access : WORKSPACE_ACCESS;
    if (!sameSections(start.sections, d.sections)) out.sections = d.sections;
    if (start.show_reply_snippets !== d.show_reply_snippets) out.show_reply_snippets = d.show_reply_snippets;
    if (start.date_range !== d.date_range) out.date_range = d.date_range;
    return out;
}

export default function ReportDialog({
    report,
    onClose,
    onCreated,
}: {
    /** The report to edit; absent creates a new one. */
    report?: ClientReport | null;
    onClose: () => void;
    onCreated?: (report: ClientReport) => void;
}) {
    const confirm = useConfirm();
    const create = useCreateReport();
    const update = useUpdateReport();
    const start = React.useMemo(() => draftOf(report), [report]);
    const [draft, setDraft] = React.useState<Draft>(start);
    const [saving, setSaving] = React.useState(false);
    const [serverError, setServerError] = React.useState<string | null>(null);
    const [showErrors, setShowErrors] = React.useState(false);

    const changes = diff(start, draft);
    const dirty = Object.keys(changes).length > 0;
    const set = (patch: Partial<Draft>) => {
        setServerError(null);
        setDraft((d) => ({ ...d, ...patch }));
    };

    const nameProblem = nameError("Name", draft.name, "workspace");
    const restricted = draft.access.scope === "restricted";
    const nothingInScope =
        restricted &&
        draft.access.campaign_folder_ids.length + draft.access.campaign_ids.length + draft.access.email_account_ids.length === 0;
    const noSections = !SECTION_OPTIONS.some((o) => draft.sections[o.key]);
    const blocker = nameProblem ?? (nothingInScope ? "Choose at least one campaign, folder or mailbox, or cover the whole workspace." : null) ?? (noSections ? "Turn on at least one section." : null);

    const requestClose = React.useCallback(() => {
        if (dirty && !saving) {
            confirm.show(report ? "Discard your changes to this report?" : "Discard this new report?", async () => onClose());
            return;
        }
        onClose();
    }, [dirty, saving, confirm, onClose, report]);

    React.useEffect(() => {
        function onKey(e: KeyboardEvent) {
            if (e.key !== "Escape") return;
            if (document.querySelector('[data-floating], [role="alertdialog"]')) return;
            requestClose();
        }
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [requestClose]);

    async function save() {
        if (blocker) {
            setShowErrors(true);
            return;
        }
        setSaving(true);
        try {
            if (report) {
                if (Object.keys(changes).length > 0) await update.mutateAsync({ id: report.id, input: changes });
                toast.success("Report saved");
                onClose();
            } else {
                const created = await create.mutateAsync({
                    name: normalizeName(draft.name),
                    access: restricted ? draft.access : WORKSPACE_ACCESS,
                    sections: draft.sections,
                    show_reply_snippets: draft.show_reply_snippets,
                    date_range: draft.date_range,
                });
                toast.success("Report created");
                onClose();
                onCreated?.(created);
            }
        } catch (err) {
            const e = err as AppError;
            if (e.code && e.message) setServerError(e.message);
            else toast.error(buildError(e));
        } finally {
            setSaving(false);
        }
    }

    const title = report ? "Edit report" : "New report";

    return createPortal(
        <div className="fixed inset-0 z-[60] flex items-start justify-center p-4 pt-[6vh]">
            <motion.div
                className="absolute inset-0 bg-slate-900/40"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ duration: 0.12 }}
                onMouseDown={requestClose}
            />
            <motion.div
                role="dialog"
                aria-modal="true"
                aria-label={title}
                onMouseDown={(e) => e.stopPropagation()}
                initial={{ opacity: 0, y: 8 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.16, ease: "easeOut" }}
                className="relative w-full max-w-[580px] max-h-[88vh] flex flex-col overflow-hidden rounded-lg border border-slate-200 bg-white shadow-[0_24px_60px_-12px_rgba(15,23,42,0.25)]"
            >
                <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-3 shrink-0">
                    <div className="min-w-0">
                        <div className="text-[13px] font-semibold text-slate-900 leading-tight">{title}</div>
                        <div className="text-[11px] text-slate-500 truncate leading-tight">
                            What a client sees on the shared page and in scheduled emails
                        </div>
                    </div>
                    <button
                        type="button"
                        onClick={requestClose}
                        aria-label="Close"
                        className="ml-auto h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-slate-100"
                    >
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>

                <div className="flex-1 overflow-y-auto p-4 flex flex-col gap-5">
                    <div>
                        <Label>Name</Label>
                        <TextInput
                            value={draft.name}
                            onChange={(v) => set({ name: v })}
                            placeholder="Acme outreach, monthly"
                            autoFocus={!report}
                            maxLength={WORKSPACE_NAME_MAX}
                            invalid={showErrors && !!nameProblem}
                            className="w-full"
                            onKeyDown={(e) => {
                                if (e.key === "Enter") void save();
                            }}
                        />
                        <FieldError message={showErrors ? nameProblem : null} />
                        <p className="mt-1 text-[11px] text-slate-400">Shown as the title of the report page and emails.</p>
                    </div>

                    <ScopeEditor value={draft.access} onChange={(access) => set({ access })} />
                    {showErrors && nothingInScope && (
                        <FieldError className="-mt-3" message="Choose at least one campaign, folder or mailbox, or cover the whole workspace." />
                    )}

                    <div>
                        <Label>Default date range</Label>
                        <Segmented value={draft.date_range} onChange={(date_range) => set({ date_range })} options={RANGE_OPTIONS} className="flex-wrap" />
                        <p className="mt-1 text-[11px] text-slate-400">What the shared page and each scheduled email cover.</p>
                    </div>

                    <div>
                        <Label>Sections</Label>
                        <div className="rounded-md border border-slate-200 divide-y divide-slate-100">
                            {SECTION_OPTIONS.map((o) => (
                                <div key={o.key} className="px-3 py-2 flex items-center gap-3">
                                    <div className="min-w-0 flex-1">
                                        <div className="text-[12.5px] text-slate-900">{o.label}</div>
                                        <div className="text-[11px] text-slate-500">{o.hint}</div>
                                    </div>
                                    <Toggle
                                        value={draft.sections[o.key]}
                                        onChange={(v) => set({ sections: { ...draft.sections, [o.key]: v } })}
                                        ariaLabel={o.label}
                                    />
                                </div>
                            ))}
                        </div>
                        <FieldError message={showErrors && noSections ? "Turn on at least one section." : null} />
                    </div>

                    <div className="rounded-md border border-slate-200 px-3 py-2 flex items-start gap-3">
                        <div className="min-w-0 flex-1">
                            <div className="text-[12.5px] text-slate-900">Show reply snippets</div>
                            <div className="text-[11px] text-slate-500 leading-relaxed">
                                Snippets show part of what leads wrote back, so anyone with the link reads it. Off by default.
                            </div>
                        </div>
                        <Toggle
                            value={draft.show_reply_snippets}
                            onChange={(v) => set({ show_reply_snippets: v })}
                            ariaLabel="Show reply snippets"
                        />
                    </div>
                </div>

                <div className="min-h-12 px-4 py-2 border-t border-slate-200 flex items-center gap-2 shrink-0">
                    {serverError && <span className="text-[11.5px] text-red-600 min-w-0">{serverError}</span>}
                    <button
                        type="button"
                        onClick={requestClose}
                        className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:bg-slate-100 transition-colors"
                    >
                        Cancel
                    </button>
                    <button
                        type="button"
                        onClick={() => void save()}
                        disabled={saving || (!!report && !dirty)}
                        className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {saving && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        {report ? "Save report" : "Create report"}
                    </button>
                </div>
            </motion.div>
        </div>,
        document.body,
    );
}

/** Whole workspace, or the campaign folders, campaigns and mailboxes picked here. */
function ScopeEditor({ value, onChange }: { value: MemberAccess; onChange: (next: MemberAccess) => void }) {
    const restricted = value.scope === "restricted";
    const { user } = useUserProfile();
    const campaigns = useCampaigns({ query: "", folder: "", enabled: restricted });
    const emails = useEmails({ query: "", tag: "", enabled: restricted });

    const folderItems = React.useMemo(
        () =>
            [...(user.folders ?? [])]
                .sort((a, b) => a.position - b.position)
                .map((f) => ({ id: f.id, label: f.title, color: f.color })),
        [user.folders],
    );
    const campaignItems = React.useMemo(
        () => campaigns.campaigns.map((c) => ({ id: c.id, label: c.name || "Untitled campaign" })),
        [campaigns.campaigns],
    );
    const mailboxItems = React.useMemo(() => emails.emails.map((e) => ({ id: e.id, label: e.email, sub: e.name })), [emails.emails]);

    const set = (patch: Partial<MemberAccess>) => onChange({ ...value, ...patch });

    return (
        <div className="flex flex-col gap-3">
            <div>
                <Label>Covers</Label>
                <div className="grid grid-cols-2 gap-1.5">
                    {(
                        [
                            ["workspace", "Whole workspace", "Every campaign and mailbox", GlobeIcon],
                            ["restricted", "Selected", "Only the campaigns and mailboxes you pick", LockIcon],
                        ] as const
                    ).map(([scope, label, sub, Icon]) => (
                        <button
                            key={scope}
                            type="button"
                            onClick={() => set({ scope })}
                            className={cn(
                                "rounded-md border px-2.5 py-2 text-left transition-colors",
                                value.scope === scope ? "border-sky-400 bg-sky-50" : "border-slate-200 hover:border-slate-300",
                            )}
                        >
                            <span className="flex items-center gap-1.5 text-[12.5px] font-medium text-slate-900">
                                <Icon className="w-3 h-3 text-slate-500" />
                                {label}
                            </span>
                            <span className="block text-[11px] text-slate-500">{sub}</span>
                        </button>
                    ))}
                </div>
            </div>

            {restricted && (
                <motion.div
                    initial={{ opacity: 0, height: 0 }}
                    animate={{ opacity: 1, height: "auto" }}
                    transition={{ duration: 0.16, ease: "easeOut" }}
                    className="flex flex-col gap-3"
                >
                    <div>
                        <Label>Campaign folders</Label>
                        <ResourcePicker
                            icon={FolderIcon}
                            items={folderItems}
                            value={value.campaign_folder_ids}
                            onChange={(ids) => set({ campaign_folder_ids: ids })}
                            placeholder="Choose folders…"
                            empty="This workspace has no campaign folders."
                        />
                        <p className="mt-1 text-[11px] text-slate-400">A folder covers whatever campaigns it holds, including ones added later.</p>
                    </div>
                    <div>
                        <Label>Campaigns</Label>
                        <ResourcePicker
                            icon={MegaphoneIcon}
                            items={campaignItems}
                            loading={campaigns.isPending}
                            value={value.campaign_ids}
                            onChange={(ids) => set({ campaign_ids: ids })}
                            placeholder="Choose campaigns…"
                            empty="This workspace has no campaigns."
                        />
                    </div>
                    <div>
                        <Label>Mailboxes</Label>
                        <ResourcePicker
                            icon={InboxIcon}
                            items={mailboxItems}
                            loading={emails.isPending}
                            value={value.email_account_ids}
                            onChange={(ids) => set({ email_account_ids: ids })}
                            placeholder="Choose mailboxes…"
                            empty="This workspace has no mailboxes."
                        />
                        <p className="mt-1 text-[11px] text-slate-400">Mailboxes feed the mailbox health and warmup section. Campaign numbers come from the campaigns above.</p>
                    </div>
                </motion.div>
            )}
        </div>
    );
}
