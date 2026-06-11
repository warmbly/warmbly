import React from "react";
import { useNavigate } from "react-router-dom";
import { RowLink, Section, SectionShell } from "../_components/SectionShell";
import PasskeyManager from "./PasskeyManager";
import SessionManager from "./SessionManager";
import TwoFactorManager from "./TwoFactorManager";
import ChangePasswordDialog from "./ChangePasswordDialog";

export default function SecuritySettingsPage() {
    const [pwOpen, setPwOpen] = React.useState(false);
    const navigate = useNavigate();
    return (
        <SectionShell title="Security" description="Sign-in protection for your account.">
            <PasskeyManager />

            <TwoFactorManager />

            <Section eyebrow="Password" description="The credential you sign in with.">
                <RowLink
                    title="Change password"
                    description="Use 12+ characters with mixed case and a number."
                    cta="Change"
                    onClick={() => setPwOpen(true)}
                />
            </Section>

            <SessionManager />

            <Section eyebrow="Alerts" description="How we let you know about account activity.">
                <RowLink
                    title="Sign-in alerts"
                    description="Get notified when your account is accessed from a new device. Turn email delivery on under Notifications."
                    cta="Configure"
                    onClick={() => navigate("/app/settings/notifications")}
                />
            </Section>

            <Section
                eyebrow="Authorized apps"
                description="Third-party apps connected to your account."
            >
                <p className="text-[12px] text-slate-500 leading-relaxed">
                    No apps connected yet. OAuth-connected services show up here when you grant
                    them access.
                </p>
            </Section>

            <Section
                eyebrow="Email security"
                description="Sender authentication for the mailboxes you connect."
            >
                <RowLink
                    title="DKIM, SPF, DMARC"
                    description="Authenticate sending domains for deliverability."
                    cta="Open mailboxes"
                    onClick={() => (window.location.href = "/app/emails")}
                />
                <RowLink
                    title="API keys"
                    description="Programmatic access tokens. Revoke from the API Keys tab."
                    cta="Manage"
                    onClick={() => (window.location.href = "/app/api-keys")}
                />
            </Section>
            <ChangePasswordDialog open={pwOpen} onClose={() => setPwOpen(false)} />
        </SectionShell>
    );
}
