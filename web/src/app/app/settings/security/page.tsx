import { comingSoon } from "@/lib/helper/comingSoon";
import { RowLink, Section, SectionShell } from "../_components/SectionShell";

export default function SecuritySettingsPage() {
    return (
        <SectionShell title="Security" description="Sign-in protection for your account.">
            <Section eyebrow="Authentication" description="How you prove it's you when signing in.">
                <RowLink
                    title="Two-factor authentication"
                    description="Add a one-time code to every sign-in."
                    cta="Enable 2FA"
                    statusLabel="Off"
                    statusTone="muted"
                    onClick={() => comingSoon("Two-factor authentication")}
                />
                <RowLink
                    title="Change password"
                    description="Use 12+ characters with mixed case and a number."
                    cta="Change"
                    onClick={() => comingSoon("Change password")}
                />
                <RowLink
                    title="Backup codes"
                    description="One-time codes you can use if you lose access to your 2FA device."
                    cta="Generate"
                    onClick={() => comingSoon("Backup codes")}
                />
            </Section>

            <Section eyebrow="Sessions" description="Active sign-ins and how they're alerted.">
                <RowLink
                    title="Active sessions"
                    description="Devices currently signed in to your account."
                    cta="View"
                    onClick={() => comingSoon("Active sessions")}
                />
                <RowLink
                    title="Sign out everywhere"
                    description="Revoke every session except this one."
                    cta="Sign out"
                    onClick={() => comingSoon("Sign out everywhere")}
                />
                <RowLink
                    title="Sign-in alerts"
                    description="Email when your account is accessed from a new device."
                    cta="Configure"
                    statusLabel="On"
                    statusTone="ok"
                    onClick={() => comingSoon("Sign-in alerts")}
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
        </SectionShell>
    );
}
