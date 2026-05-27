import { BackendsPage } from "./BackendsPage";

export default function TransportsPage() {
    return (
        <BackendsPage
            kind="transport"
            title="Transports (SMTP / OAuth)"
            description="Outbound SMTP defaults, OAuth client registration for Gmail and Outlook, and any other mailbox-side transport credentials the workers need to send mail."
            notes="Per the agent notes, mailbox-level send budgets stay the source of truth; transport defaults set ceilings, not floors. Don't push a mailbox above its measured safe band by raising the transport default."
        />
    );
}
