// Placeholder pages for nav entries we haven't wired backends for yet.
// They exist so the sidebar never 404s and so it's obvious to a dev
// which surface they should fill in next.

import { ComingSoon, PageHeader } from "@/components/layout/PageHeader";

interface StubProps {
    title: string;
    description: string;
    coming: string;
}

function Stub({ title, description, coming }: StubProps) {
    return (
        <div>
            <PageHeader title={title} description={description} />
            <ComingSoon label={coming} />
        </div>
    );
}

export function NotFoundPage() {
    return (
        <div>
            <PageHeader title="Page not found" description="That route isn't part of the admin app." />
            <a href="/" className="text-sm underline text-muted-foreground">
                Back to overview
            </a>
        </div>
    );
}
