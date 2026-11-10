// Terminal states, word-for-word with the static pages the portal service serves for the same conditions.
// They carry no brand of their own: they draw in whatever look the document already has (look.ts).

import type { ReactNode } from "react";

import { ClockIcon, QuestionIcon } from "./icons";

/** PlainState is a page that is only a message: an icon, a heading, a sentence and at most one action. */
export function PlainState({ icon, title, body, action }: { icon: ReactNode; title: string; body: string; action?: { label: string; run: () => void } }) {
    return (
        <main className="plain">
            <span className="plain-icon">{icon}</span>
            <h1>{title}</h1>
            <p>{body}</p>
            {action && (
                <button type="button" onClick={action.run}>
                    {action.label}
                </button>
            )}
        </main>
    );
}

export function NotFound() {
    return <PlainState icon={<QuestionIcon />} title="There is nothing at this address" body="Check the link you were sent." />;
}

// StalePage: the tab outlived its render token (12h); only a reload mints a new one.
export function StalePage() {
    return (
        <PlainState
            icon={<ClockIcon />}
            title="This page went stale"
            body="It has been open for a while. Refresh to continue."
            action={{ label: "Refresh", run: () => window.location.reload() }}
        />
    );
}
