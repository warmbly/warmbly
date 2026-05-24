import React from "react";
import { useAppStore } from "@/stores";
import { TextInput } from "@/components/ui/field";
import { TopbarAction } from "@/components/layout/Page";
import { comingSoon } from "@/lib/helper/comingSoon";
import { AvatarUploader } from "@/components/app/avatar/AvatarUploader";
import {
    useDeleteOrgAvatar,
    useUploadOrgAvatar,
} from "@/lib/api/hooks/app/avatar/useOrgAvatar";
import { Row, Section, SectionShell, ToggleRow } from "../_components/SectionShell";

export default function WorkspaceSettingsPage() {
    const currentOrg = useAppStore((s) => s.currentOrganization);
    const [name, setName] = React.useState(currentOrg?.name ?? "");
    const [domain, setDomain] = React.useState("");

    const uploadOrgAvatar = useUploadOrgAvatar();
    const removeOrgAvatar = useDeleteOrgAvatar();

    // Avatar changes are committed immediately by the uploader, so
    // they don't count toward the dirty flag — only fields that need
    // a "Save workspace" action do.
    const dirty = name !== (currentOrg?.name ?? "") || domain !== "";

    function discard() {
        setName(currentOrg?.name ?? "");
        setDomain("");
    }

    return (
        <SectionShell
            title="Workspace"
            description="Org-wide settings. Visible only to the owner."
            actions={
                dirty ? (
                    <>
                        <TopbarAction variant="ghost" onClick={discard}>
                            Discard
                        </TopbarAction>
                        <TopbarAction onClick={() => comingSoon("Workspace settings")}>
                            Save workspace
                        </TopbarAction>
                    </>
                ) : null
            }
        >
            <Section
                eyebrow="Identity"
                description="How this workspace is named and addressed."
            >
                <Row
                    label="Workspace avatar"
                    description="Square logo or initials. Shown in the org switcher and on shared report URLs."
                    align="start"
                >
                    <AvatarUploader
                        current={currentOrg?.avatar_url ?? currentOrg?.avatar}
                        fallbackInitials={(currentOrg?.name ?? "WS").slice(0, 2).toUpperCase()}
                        shape="square"
                        onUpload={async (blob) => {
                            await uploadOrgAvatar.mutateAsync(blob);
                        }}
                        onRemove={async () => {
                            await removeOrgAvatar.mutateAsync();
                        }}
                    />
                </Row>
                <Row label="Workspace name" description="Shown in the sidebar and invitation emails.">
                    <TextInput value={name} onChange={setName} className="w-[280px]" />
                </Row>
                <Row
                    label="Workspace ID"
                    description="Stable identifier. Used in API calls and support tickets."
                    align="start"
                >
                    <input
                        type="text"
                        value={currentOrg?.id ?? ""}
                        disabled
                        className="w-[300px] h-7 px-2.5 rounded-md border border-slate-200 bg-slate-50 text-[12px] text-slate-500 font-mono"
                    />
                </Row>
            </Section>

            <Section
                eyebrow="Sending defaults"
                description="Used by new campaigns unless overridden."
            >
                <Row label="Default sender domain" description="Outgoing mail uses this domain by default.">
                    <TextInput
                        value={domain}
                        onChange={setDomain}
                        placeholder="company.com"
                        className="w-[280px]"
                    />
                </Row>
                <Row
                    label="Default daily cap"
                    description="Built-in safety: 50/day per cold mailbox. Raise per-campaign if needed."
                >
                    <TextInput
                        value="50"
                        onChange={() => undefined}
                        type="number"
                        className="w-[120px]"
                    />
                </Row>
            </Section>

            <Section
                eyebrow="Privacy & compliance"
                description="Headers and identifiers attached to every send."
            >
                <ToggleRow
                    label="Include List-Unsubscribe header"
                    description="Required by Gmail/Outlook for bulk senders. Strongly recommended."
                    defaultOn
                />
                <ToggleRow
                    label="Append unsubscribe link"
                    description="Plain-text footer link respecting the suppression list."
                    defaultOn
                />
                <ToggleRow
                    label="Track opens by default"
                    description="Inserts a 1×1 pixel. Disable for highest deliverability."
                />
            </Section>

            <Section
                eyebrow="Workspace stats"
                description="Snapshot of how this workspace is being used."
            >
                <div className="grid grid-cols-3 gap-6">
                    <Stat label="Members" value={1} />
                    <Stat label="Mailboxes" value={0} />
                    <Stat label="Campaigns" value={0} />
                </div>
            </Section>
        </SectionShell>
    );
}

function Stat({ label, value }: { label: string; value: string | number }) {
    return (
        <div>
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                {label}
            </div>
            <div className="text-[18px] font-semibold text-slate-900 tabular-nums leading-tight mt-0.5">
                {value}
            </div>
        </div>
    );
}
