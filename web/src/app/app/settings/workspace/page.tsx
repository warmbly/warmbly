import React from "react";
import { useAppStore } from "@/stores";
import { TextInput } from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";
import useUpdateOrganization from "@/lib/api/hooks/app/organizations/useUpdateOrganization";
import { AvatarUploader } from "@/components/app/avatar/AvatarUploader";
import {
    useDeleteOrgAvatar,
    useUploadOrgAvatar,
} from "@/lib/api/hooks/app/avatar/useOrgAvatar";
import { Row, Section, SectionShell, ToggleRow } from "../_components/SectionShell";
import SaveStatus from "../_components/SaveStatus";
import { useAutosave } from "@/hooks/useAutosave";
import { useRegisterUnsaved } from "@/hooks/context/unsaved";
import useCurrentOrganization from "@/lib/api/hooks/app/organizations/useCurrentOrganization";
import { usePermission } from "@/hooks/usePermission";

export default function WorkspaceSettingsPage() {
    const currentOrg = useAppStore((s) => s.currentOrganization);
    const [name, setName] = React.useState(currentOrg?.name ?? "");

    const uploadOrgAvatar = useUploadOrgAvatar();
    const removeOrgAvatar = useDeleteOrgAvatar();
    const updateOrg = useUpdateOrganization();

    // Team presence privacy. The full org (with the flags) comes from
    // /organization/current; toggling saves immediately and the realtime
    // service re-gates everyone live. Only admins with Manage settings can edit.
    const orgQuery = useCurrentOrganization();
    const canManageSettings = usePermission("MANAGE_SETTINGS");
    const [showOnline, setShowOnline] = React.useState(true);
    const [showActivity, setShowActivity] = React.useState(true);
    React.useEffect(() => {
        if (!orgQuery.data) return;
        setShowOnline(orgQuery.data.presence_show_online ?? true);
        setShowActivity(orgQuery.data.presence_show_activity ?? true);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [orgQuery.data?.presence_show_online, orgQuery.data?.presence_show_activity]);

    const onToggleOnline = (next: boolean) => {
        setShowOnline(next);
        updateOrg.mutate({ presence_show_online: next });
    };
    const onToggleActivity = (next: boolean) => {
        setShowActivity(next);
        updateOrg.mutate({ presence_show_activity: next });
    };

    // AI voice profile. Grounds every AI writing surface. Saved on blur when
    // changed. Manage settings only.
    const [productDesc, setProductDesc] = React.useState("");
    const [icpNotes, setIcpNotes] = React.useState("");
    const [voiceProfile, setVoiceProfile] = React.useState("");
    React.useEffect(() => {
        if (!orgQuery.data) return;
        setProductDesc(orgQuery.data.product_description ?? "");
        setIcpNotes(orgQuery.data.icp_notes ?? "");
        setVoiceProfile(orgQuery.data.voice_profile ?? "");
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [
        orgQuery.data?.product_description,
        orgQuery.data?.icp_notes,
        orgQuery.data?.voice_profile,
    ]);
    const saveVoiceField = (key: "product_description" | "icp_notes" | "voice_profile", value: string, saved: string) => {
        if (value !== saved) updateOrg.mutate({ [key]: value });
    };

    // Inbox agent opt-in (paid). When on, an inbound human reply gets an
    // AI-drafted suggested reply awaiting review in the unibox.
    const [inboxAgent, setInboxAgent] = React.useState(false);
    React.useEffect(() => {
        if (orgQuery.data) setInboxAgent(orgQuery.data.inbox_agent_enabled ?? false);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [orgQuery.data?.inbox_agent_enabled]);
    const onToggleInboxAgent = (next: boolean) => {
        setInboxAgent(next);
        updateOrg.mutate({ inbox_agent_enabled: next });
    };

    // Auto-save the workspace name ~700ms after typing stops. An empty name is
    // never persisted; the field just stays unsaved until it's valid again.
    const autosave = useAutosave({
        value: name.trim(),
        debounceMs: 700,
        save: async (v) => {
            if (!v) throw new Error("name required");
            await updateOrg.mutateAsync({ name: v });
        },
    });
    useRegisterUnsaved(autosave, () => setName(autosave.savedValue));

    React.useEffect(() => {
        autosave.markSaved(currentOrg?.name ?? "");
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [currentOrg?.name]);

    return (
        <SectionShell
            title="Workspace"
            description="Org-wide settings. Visible only to the owner."
            actions={<SaveStatus status={autosave.status} onRetry={autosave.retry} />}
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
                    <TextInput value={name} onChange={setName} className="w-full max-w-[280px]" />
                </Row>
                <Row
                    label="Workspace ID"
                    description="Stable identifier. Used in API calls and support tickets."
                    align="start"
                >
                    <input
                        type="text"
                        value={currentOrg?.id ?? ""}
                        readOnly
                        className="w-full max-w-[300px] h-7 px-2.5 rounded-md border border-slate-200 bg-slate-50 text-[12px] text-slate-500 font-mono"
                    />
                </Row>
            </Section>

            <Section
                eyebrow="Sending defaults"
                description="Used by new campaigns unless overridden."
            >
                <Row
                    label="Default daily cap"
                    description="Built-in safety: 50/day per cold mailbox. Raise per-campaign if needed."
                >
                    <input
                        type="text"
                        value="50 / day"
                        disabled
                        className="w-full max-w-[120px] h-7 px-2.5 rounded-md border border-slate-200 bg-slate-50 text-[12px] text-slate-500"
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
                eyebrow="Team presence"
                description="What members can see about each other in real time. Applies to everyone in the workspace."
            >
                <ToggleRow
                    label="Show who's online"
                    description="Display the live avatar stack of members currently in the dashboard. Off hides all online presence from teammates."
                    checked={showOnline}
                    onChange={onToggleOnline}
                    disabled={!canManageSettings}
                />
                <ToggleRow
                    label="Show activity"
                    description="Let teammates see what someone is viewing, editing, or replying to. Off keeps online status but hides the detail."
                    checked={showActivity && showOnline}
                    onChange={onToggleActivity}
                    disabled={!canManageSettings || !showOnline}
                />
            </Section>

            <Section
                eyebrow="AI voice profile"
                description="Grounds every AI writing surface (assistant, reply drafts, research openers) so drafts sound like you and know what you sell. All optional."
            >
                <Row
                    label="What you sell"
                    description="One or two sentences on your product and the outcome it delivers."
                    align="start"
                >
                    <Textarea
                        value={productDesc}
                        onChange={(e) => setProductDesc(e.target.value)}
                        onBlur={() => saveVoiceField("product_description", productDesc, orgQuery.data?.product_description ?? "")}
                        disabled={!canManageSettings}
                        rows={3}
                        maxLength={2000}
                        placeholder="We help RevOps teams keep their CRM clean by..."
                        className="w-full max-w-[420px] text-[12.5px]"
                    />
                </Row>
                <Row
                    label="Who you sell to"
                    description="Your ideal customer: role, company type, the pain they feel."
                    align="start"
                >
                    <Textarea
                        value={icpNotes}
                        onChange={(e) => setIcpNotes(e.target.value)}
                        onBlur={() => saveVoiceField("icp_notes", icpNotes, orgQuery.data?.icp_notes ?? "")}
                        disabled={!canManageSettings}
                        rows={3}
                        maxLength={2000}
                        placeholder="Heads of RevOps at 50-500 person B2B SaaS companies who..."
                        className="w-full max-w-[420px] text-[12.5px]"
                    />
                </Row>
                <Row
                    label="House voice"
                    description="How you want to sound. Casual or formal, phrases to use or avoid."
                    align="start"
                >
                    <Textarea
                        value={voiceProfile}
                        onChange={(e) => setVoiceProfile(e.target.value)}
                        onBlur={() => saveVoiceField("voice_profile", voiceProfile, orgQuery.data?.voice_profile ?? "")}
                        disabled={!canManageSettings}
                        rows={3}
                        maxLength={2000}
                        placeholder="Direct and warm, lowercase openers are fine, never salesy."
                        className="w-full max-w-[420px] text-[12.5px]"
                    />
                </Row>
            </Section>

            <Section
                eyebrow="Inbox agent"
                description="On an inbound human reply, draft a suggested reply in your voice and hold it in the unibox for review. It never sends on its own. Paid feature; each handled reply costs 5 AI credits."
            >
                <ToggleRow
                    label="Draft replies for me"
                    description="When someone replies, the agent writes a suggested reply and attaches it to the thread under Agent drafts. You approve-and-send, edit, or discard it."
                    checked={inboxAgent}
                    onChange={onToggleInboxAgent}
                    disabled={!canManageSettings}
                />
            </Section>

            <Section
                eyebrow="Workspace stats"
                description="Snapshot of how this workspace is being used."
            >
                <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-6">
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
