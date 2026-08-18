// Settings → Data. Move this workspace to or from another Warmbly instance.
//
// Backed by:
//   GET    /organization/current/transfer/groups
//   POST   /organization/current/export
//   GET    /organization/current/export
//   GET    /organization/current/export/:id/download
//   DELETE /organization/current/export/:id
//   POST   /organization/current/import/preflight
//   POST   /organization/current/import
//   GET    /organization/current/import
//
// Owner-only, matching the danger zone: an export with credentials is the most
// sensitive file this product produces, and an import rewrites the workspace.

import React from "react";
import toast from "react-hot-toast";
import { DownloadIcon, UploadIcon } from "lucide-react";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import { useOrgExports, useOrgImports, useOrgTransferGroups } from "@/lib/api/hooks/app/orgtransfer/useOrgTransfer";
import { Section, SectionShell } from "../_components/SectionShell";
import ExportPanel from "./ExportPanel";
import ImportPanel from "./ImportPanel";
import TransferHistory from "./TransferHistory";

export default function DataSettingsPage() {
    const access = useFeatureAccess();
    const groups = useOrgTransferGroups();
    const exports = useOrgExports();
    const imports = useOrgImports();

    if (!access.isOwner) {
        return (
            <SectionShell
                title="Data"
                description="Move this workspace to or from another Warmbly instance."
            >
                <Section eyebrow="Restricted">
                    <p className="text-[12.5px] text-slate-500 leading-relaxed">
                        Only the workspace owner can export or import workspace data. An
                        export contains every contact, message and mailbox credential in
                        the workspace, so it is held to the same bar as deleting it.
                    </p>
                </Section>
            </SectionShell>
        );
    }

    return (
        <SectionShell
            title="Data"
            description="Move this workspace to or from another Warmbly instance."
        >
            <Section
                eyebrow="Export"
                description="Write the whole workspace to a single archive file you can import on another instance."
                actions={
                    <span className="inline-flex items-center gap-1.5 text-[11px] text-slate-400">
                        <DownloadIcon className="w-3 h-3" />
                        {groups.data ? `Kept for ${groups.data.retention_days} days` : ""}
                    </span>
                }
            >
                <ExportPanel
                    groups={groups.data?.groups ?? []}
                    minPassphrase={groups.data?.min_passphrase ?? 12}
                    loading={groups.isLoading}
                    onStarted={() => toast.success("Export started. It keeps running if you leave this page.")}
                />
            </Section>

            <Section
                eyebrow="Archives"
                description="Exports from this workspace. Each one is a full copy, so they expire."
            >
                <TransferHistory
                    exports={exports.data ?? []}
                    imports={imports.data ?? []}
                    loading={exports.isLoading || imports.isLoading}
                />
            </Section>

            <Section
                eyebrow="Import"
                description="Apply an archive exported from another instance to this workspace."
                actions={
                    <span className="inline-flex items-center gap-1.5 text-[11px] text-slate-400">
                        <UploadIcon className="w-3 h-3" />
                        Nothing is written until you confirm
                    </span>
                }
            >
                <ImportPanel
                    groups={groups.data?.groups ?? []}
                    onStarted={() => toast.success("Import started. It keeps running if you leave this page.")}
                />
            </Section>
        </SectionShell>
    );
}
