import React from "react";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeftIcon, ExternalLinkIcon, Loader2Icon, Settings2Icon } from "lucide-react";
import { usePermission } from "@/hooks/usePermission";
import { usePresenceResource } from "@/hooks/PresenceProvider";
import { useCompanyEnrichConnection } from "@/lib/api/hooks/app/integrations/useCompanyEnrich";
import useIntegrationCatalog from "@/lib/api/hooks/app/integrations/useIntegrationCatalog";
import ProviderGlyph from "../../_components/ProviderGlyph";
import StatusPill from "../../_components/StatusPill";
import ConnectionDetail from "../../_components/ConnectionDetail";
import { secondaryBtn } from "../../salesforce/_components/shared";
import { errMsg } from "../../salesforce/_components/util";
import ResearchPanel from "../ResearchPanel";
import ContactEnrichment from "../ContactEnrichment";

export default function CompanyEnrichPage() {
    const { id } = useParams({ from: "/app/integrations/companyenrich/$id" });
    const detail = useCompanyEnrichConnection(id);
    const catalog = useIntegrationCatalog();
    const canUse = usePermission("USE_INTEGRATIONS");
    const canManageContacts = usePermission("MANAGE_CONTACTS");
    const canViewContacts = usePermission("VIEW_CONTACTS");
    const [manage, setManage] = React.useState(false);
    usePresenceResource(`integration_connection:${id}`, "viewing");
    const connection = detail.data?.connection;
    const ready = connection?.provider === "companyenrich" && ["connected", "degraded"].includes(connection.status);

    return <div className="min-h-full bg-white px-3 py-4 sm:px-5">
        <Link to="/app/integrations" className="mb-4 inline-flex items-center gap-1 text-[12px] text-slate-500 hover:text-slate-800"><ArrowLeftIcon className="size-3.5" />Integrations</Link>
        <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
            <div className="flex min-w-0 items-center gap-3"><ProviderGlyph provider="companyenrich" name="CompanyEnrich" /><div><div className="flex flex-wrap items-center gap-2"><h1 className="text-[18px] font-semibold text-slate-900">{connection?.label || "CompanyEnrich"}</h1>{connection && <StatusPill status={connection.status} />}</div><p className="mt-1 text-[12px] text-slate-500">Company and people research using your own account.</p></div></div>
            <div className="flex flex-wrap gap-2"><a href="https://app.companyenrich.com/" target="_blank" rel="noopener noreferrer" className={secondaryBtn}><ExternalLinkIcon className="size-3.5" />CompanyEnrich account</a>{connection && <button type="button" onClick={() => setManage(true)} className={secondaryBtn}><Settings2Icon className="size-3.5" />Connection settings</button>}</div>
        </div>
        {detail.isPending ? <div className="flex items-center gap-2 text-[12.5px] text-slate-500"><Loader2Icon className="size-4 animate-spin" />Loading connection…</div> : detail.isError ? <div role="alert" className="rounded-md border border-red-200 bg-red-50 p-4 text-[12.5px] text-red-700">{errMsg(detail.error, "This connection could not be loaded.")}<button type="button" onClick={() => void detail.refetch()} className={`${secondaryBtn} ml-3`}>Try again</button></div> : connection?.provider !== "companyenrich" ? <p role="alert" className="text-[12.5px] text-red-700">This is not a CompanyEnrich connection.</p> : <div className="max-w-5xl space-y-8">
            {(!ready || !canUse) && <p role="status" className="rounded-md border border-amber-200 bg-amber-50 p-3 text-[12.5px] text-amber-800">{!canUse ? "You need permission to use integrations before making requests." : "Disconnect this connection in settings, then connect CompanyEnrich again from Integrations with a current API key."}</p>}
            <ResearchPanel key={`research:${id}`} connectionId={id} enabled={ready && canUse} />
            {canManageContacts && canViewContacts ? <ContactEnrichment key={`contacts:${id}`} connectionId={id} enabled={ready && canUse} /> : <p className="text-[12px] text-slate-500">Enriching saved contacts also requires permission to view and manage contacts.</p>}
        </div>}
        {manage && connection && <ConnectionDetail connection={connection} entry={catalog.data?.catalog.find((entry) => entry.provider === "companyenrich")} onClose={() => { setManage(false); void detail.refetch(); }} />}
    </div>;
}
