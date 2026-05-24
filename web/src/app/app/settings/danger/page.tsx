// Danger zone — schedule a delayed delete or cancel one in progress.
//
// Backed by:
//   GET    /organization/current/danger-zone
//   POST   /organization/current/danger-zone/delete
//   DELETE /organization/current/danger-zone/delete
//   GET    /me/danger-zone
//   POST   /me/danger-zone/delete
//   DELETE /me/danger-zone/delete
//
// The "delete" actions don't actually delete anything immediately —
// they mark the resource pending deletion for the configured grace
// window (30 days). A background job hard-deletes once the window has
// elapsed, with cancellation possible at any time before then.

import React from "react";
import { TrashIcon } from "lucide-react";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useOrganizationDangerZone from "@/lib/api/hooks/app/dangerzone/useOrganizationDangerZone";
import useAccountDangerZone from "@/lib/api/hooks/app/dangerzone/useAccountDangerZone";
import useScheduleOrganizationDeletion from "@/lib/api/hooks/app/dangerzone/useScheduleOrganizationDeletion";
import useCancelOrganizationDeletion from "@/lib/api/hooks/app/dangerzone/useCancelOrganizationDeletion";
import useScheduleAccountDeletion from "@/lib/api/hooks/app/dangerzone/useScheduleAccountDeletion";
import useCancelAccountDeletion from "@/lib/api/hooks/app/dangerzone/useCancelAccountDeletion";
import { Section, SectionShell } from "../_components/SectionShell";
import ScheduleDeletionModal from "./ScheduleDeletionModal";
import PendingDeletionBanner from "./PendingDeletionBanner";

export default function DangerSettingsPage() {
    const access = useFeatureAccess();

    const orgDanger = useOrganizationDangerZone();
    const accountDanger = useAccountDangerZone();

    const scheduleOrg = useScheduleOrganizationDeletion();
    const cancelOrg = useCancelOrganizationDeletion();
    const scheduleAcc = useScheduleAccountDeletion();
    const cancelAcc = useCancelAccountDeletion();

    const [openOrg, setOpenOrg] = React.useState(false);
    const [openAcc, setOpenAcc] = React.useState(false);

    const orgPending = orgDanger.data?.pending_deletion;
    const accPending = accountDanger.data?.pending_deletion;

    return (
        <SectionShell
            title="Danger zone"
            description="Irreversible actions. Read carefully — each one has a recovery window."
        >
            <Section
                eyebrow="Account"
                description="Affects you across every workspace."
            >
                {accPending ? (
                    <PendingDeletionBanner
                        title="Your account is scheduled for deletion"
                        deletion={accPending}
                        onCancel={() => cancelAcc.mutateAsync(undefined)}
                    />
                ) : (
                    <DangerRow
                        title="Delete account"
                        body="Permanently delete your account and every workspace you own. You'll have 30 days to cancel before the data is gone."
                        cta="Delete account…"
                        onClick={() => setOpenAcc(true)}
                    />
                )}
            </Section>

            <Section
                eyebrow="Workspace"
                description="Affects only the workspace you're viewing."
            >
                {orgPending ? (
                    <PendingDeletionBanner
                        title={`"${orgDanger.data?.resource_name}" is scheduled for deletion`}
                        deletion={orgPending}
                        onCancel={() => cancelOrg.mutateAsync(undefined)}
                    />
                ) : (
                    <DangerRow
                        title="Delete workspace"
                        body="Permanently delete this workspace and every campaign, contact and mailbox it contains. You'll have 30 days to cancel."
                        cta="Delete workspace…"
                        onClick={() => setOpenOrg(true)}
                        disabled={!access.isOwner}
                        disabledReason="Only the workspace owner can schedule deletion."
                    />
                )}
            </Section>

            {accountDanger.data && (
                <ScheduleDeletionModal
                    open={openAcc}
                    onClose={() => setOpenAcc(false)}
                    title="Delete your account"
                    body={
                        <>
                            This will schedule your <strong>entire account</strong>{" "}
                            for permanent deletion, including any workspaces you own
                            and every campaign / contact / mailbox inside them.
                        </>
                    }
                    confirmationHint={accountDanger.data.confirmation_hint}
                    graceDays={accountDanger.data.grace_days}
                    submitLabel="Schedule account deletion"
                    onSubmit={(d) => scheduleAcc.mutateAsync(d)}
                />
            )}

            {orgDanger.data && (
                <ScheduleDeletionModal
                    open={openOrg}
                    onClose={() => setOpenOrg(false)}
                    title={`Delete ${orgDanger.data.resource_name}`}
                    body={
                        <>
                            This will schedule the workspace{" "}
                            <strong>"{orgDanger.data.resource_name}"</strong> for
                            permanent deletion. Campaigns keep running during the
                            grace period — members keep their access until the
                            deletion runs.
                        </>
                    }
                    confirmationHint={orgDanger.data.confirmation_hint}
                    graceDays={orgDanger.data.grace_days}
                    submitLabel="Schedule workspace deletion"
                    onSubmit={(d) => scheduleOrg.mutateAsync(d)}
                />
            )}
        </SectionShell>
    );
}

function DangerRow({
    title,
    body,
    cta,
    onClick,
    disabled,
    disabledReason,
}: {
    title: string;
    body: string;
    cta: string;
    onClick: () => void;
    disabled?: boolean;
    disabledReason?: string;
}) {
    return (
        <div className="flex flex-col sm:flex-row gap-2 sm:gap-4 sm:items-center border-l-2 border-red-200 pl-3">
            <div className="min-w-0 flex-1">
                <div className="text-[12.5px] font-medium text-red-700 leading-tight flex items-center gap-1.5">
                    <TrashIcon className="w-3 h-3" />
                    {title}
                </div>
                <div className="text-[11.5px] text-red-700/70 leading-tight mt-0.5">
                    {body}
                </div>
                {disabled && disabledReason && (
                    <div className="text-[11px] text-slate-500 italic mt-1">
                        {disabledReason}
                    </div>
                )}
            </div>
            <button
                type="button"
                onClick={onClick}
                disabled={disabled}
                className="self-start sm:ml-auto h-7 px-2.5 rounded-md border border-red-300 hover:border-red-400 text-red-700 hover:text-red-800 hover:bg-red-100/60 text-[12px] font-medium transition-colors shrink-0 disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:border-red-300 disabled:hover:text-red-700 disabled:hover:bg-transparent"
            >
                {cta}
            </button>
        </div>
    );
}
