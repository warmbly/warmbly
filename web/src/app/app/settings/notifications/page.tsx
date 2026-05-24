import { Section, SectionShell, ToggleRow } from "../_components/SectionShell";

const GROUPS: {
    title: string;
    description: string;
    items: { label: string; hint: string; defaultOn?: boolean }[];
}[] = [
    {
        title: "Inbound activity",
        description: "Get notified when something happens on a campaign you're running.",
        items: [
            { label: "Reply received", hint: "A recipient replied to a cold email." },
            { label: "Out-of-office detected", hint: "An auto-responder hit one of your sends." },
            { label: "Click on tracked link", hint: "Off by default to reduce noise on high-volume campaigns." },
        ],
    },
    {
        title: "Health",
        description: "Deliverability and infrastructure alerts. Recommend leaving these on.",
        items: [
            { label: "Bounce detected", hint: "A mailbox starts bouncing hard." },
            { label: "Spam complaint", hint: "Immediate alert on any complaint event.", defaultOn: true },
            { label: "Worker downtime", hint: "One of your sender workers stops responding.", defaultOn: true },
        ],
    },
    {
        title: "Reports",
        description: "Scheduled rollups so you don't have to pull them yourself.",
        items: [
            { label: "Weekly digest", hint: "Monday summary of last week's volume and replies.", defaultOn: true },
            { label: "Monthly billing summary", hint: "Sent on the 1st of every month." },
        ],
    },
    {
        title: "Channels",
        description: "Where to send notifications. Email is always on for security alerts.",
        items: [
            { label: "Email", hint: "Sent to your account email.", defaultOn: true },
            { label: "In-app", hint: "Bell icon in the dashboard chrome.", defaultOn: true },
            { label: "Slack", hint: "Coming soon — connect via the Integrations tab." },
        ],
    },
];

export default function NotificationsSettingsPage() {
    return (
        <SectionShell
            title="Notifications"
            description="Email + in-app alerts. Defaults reflect the recommendation."
        >
            {GROUPS.map((g) => (
                <Section key={g.title} eyebrow={g.title} description={g.description}>
                    {g.items.map((it) => (
                        <ToggleRow
                            key={it.label}
                            label={it.label}
                            description={it.hint}
                            defaultOn={it.defaultOn}
                        />
                    ))}
                </Section>
            ))}
        </SectionShell>
    );
}
