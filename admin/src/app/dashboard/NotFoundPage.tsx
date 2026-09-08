// Catch-all for paths that are not part of the admin app.

import { Link } from "react-router-dom";
import { PageHeader } from "@/components/layout/PageHeader";

export default function NotFoundPage() {
    return (
        <div>
            <PageHeader
                title="Page not found"
                description="That route is not part of the admin app. It may have moved; the sidebar and the search palette list every page."
            />
            <Link to="/" className="text-sm text-muted-foreground underline underline-offset-4 hover:text-foreground">
                Back to overview
            </Link>
        </div>
    );
}
