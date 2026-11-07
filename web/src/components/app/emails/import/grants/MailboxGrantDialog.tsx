// MailboxGrantDialog: the admin grant view opened from a mailbox connected
// through one, with that grant picked: its status, Check again, removal, and
// its directory for connecting more mailboxes.
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { GRANT_PROVIDER_LABELS, type GrantProvider } from "@/lib/api/models/app/emails/MailboxSources";
import ImportDialogShell from "../ImportDialogShell";
import GrantImportWizard from "./GrantImportWizard";

export default function MailboxGrantDialog({
    open,
    provider,
    grantId,
    onClose,
}: {
    open: boolean;
    provider: GrantProvider;
    grantId?: string;
    onClose: () => void;
}) {
    return (
        <ImportDialogShell
            open={open}
            title={`${GRANT_PROVIDER_LABELS[provider]} admin grant`}
            icon={<ProviderLogo id={provider} size="xs" framed={false} />}
            onClose={onClose}
            discardText="Discard what you picked and entered here?"
        >
            {({ onAllowance, setDirty }) => (
                <GrantImportWizard
                    key={grantId ?? provider}
                    provider={provider}
                    initialGrantId={grantId}
                    onDone={onClose}
                    onAllowance={onAllowance}
                    onDirtyChange={setDirty}
                />
            )}
        </ImportDialogShell>
    );
}
