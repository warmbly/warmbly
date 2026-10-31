import { useState } from "react";
import toast from "react-hot-toast/headless";
import { RowLink, Section } from "../_components/SectionShell";
import { useUserProfile } from "@/hooks/context/user";
import useReauthMethods from "@/lib/api/hooks/auth/useReauthMethods";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import sendPasswordLink from "@/lib/api/client/auth/sendPasswordLink";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import ChangePasswordDialog from "./ChangePasswordDialog";

// An account created through Google, Apple or SSO has no password; it sets one
// through an emailed link instead of the change dialog.
export default function PasswordSection() {
    const { user } = useUserProfile();
    const { config } = useAuthConfig();
    const { data: methods, isLoading } = useReauthMethods();
    const [dialogOpen, setDialogOpen] = useState(false);
    const [sending, setSending] = useState(false);
    const [sentTo, setSentTo] = useState<string | null>(null);

    if (!config.password_login) return null;

    const sendLink = async () => {
        if (sending) return;
        setSending(true);
        try {
            await sendPasswordLink();
            setSentTo(user.email);
            toast.success(`Link sent to ${user.email}`);
        } catch (err) {
            toast.error(buildError(err as AppError));
        } finally {
            setSending(false);
        }
    };

    return (
        <Section eyebrow="Password" description="The credential you sign in with.">
            {isLoading ? null : methods?.password !== false ? (
                <RowLink
                    title="Change password"
                    description="Use 12+ characters with mixed case and a number."
                    cta="Change"
                    onClick={() => setDialogOpen(true)}
                />
            ) : (
                <RowLink
                    title="Set a password"
                    statusLabel="Not set"
                    description={
                        sentTo
                            ? `Open the link we sent to ${sentTo} within an hour. Setting the password signs out every device, then you sign in with your email and the new password.`
                            : `You sign in through a provider. We email a link to ${user.email} so you can also sign in with your email and a password. Your account, workspaces and plan stay the same.`
                    }
                    cta={sending ? "Sending…" : sentTo ? "Send again" : "Email me a link"}
                    onClick={sendLink}
                />
            )}
            <ChangePasswordDialog open={dialogOpen} onClose={() => setDialogOpen(false)} />
        </Section>
    );
}
