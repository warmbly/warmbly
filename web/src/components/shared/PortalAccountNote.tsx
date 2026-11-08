// On an agency portal: account settings live on the main address, where a session is not held to one agency.

import { currentPortal, mainHost } from "@/lib/portal";

export default function PortalAccountNote({ what }: { what: string }) {
    const portal = currentPortal();
    if (!portal.portal) return null;
    return (
        <p className="rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-[12.5px] leading-relaxed text-slate-600">
            You signed in on {portal.brand.name}'s portal, which reaches only its client workspaces. {what} at{" "}
            {portal.main_url ? (
                <a href={portal.main_url} className="font-medium text-slate-700 underline">
                    {mainHost(portal)}
                </a>
            ) : (
                mainHost(portal)
            )}
            .
        </p>
    );
}
