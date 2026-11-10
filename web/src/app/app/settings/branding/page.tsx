// Settings > Branding — the workspace's own brand on every page its clients
// see, the domain those pages live on, and embed code for a website.

import { useConfirm } from "@/hooks/context/confirm";
import { useWriteGuard } from "@/hooks/usePermission";
import { useAppStore } from "@/stores";
import {
    useDeleteWorkspaceBrandLogo,
    useResetWorkspaceBrand,
    useUpdateWorkspaceBrand,
    useUploadWorkspaceBrandLogo,
    useWorkspaceBrand,
} from "@/lib/api/hooks/app/brand";
import BrandEditor from "@/components/app/brand/BrandEditor";
import { useBrandAutosave } from "@/components/app/brand/useBrandAutosave";
import EmbedSnippets from "@/components/app/brand/EmbedSnippets";
import FormsDomainCard from "@/components/app/forms/FormsDomainCard";
import { Section, SectionShell } from "../_components/SectionShell";
import SaveStatus from "../_components/SaveStatus";

export default function BrandingSettingsPage() {
    const orgID = useAppStore((s) => s.currentOrganization?.id);
    // Keyed on the workspace so a switch re-seeds the editor instead of saving one brand over another.
    return <BrandingSettings key={orgID ?? "none"} />;
}

function BrandingSettings() {
    const write = useWriteGuard("MANAGE_SETTINGS");
    const confirm = useConfirm();
    const orgName = useAppStore((s) => s.currentOrganization?.name) ?? "";
    const view = useWorkspaceBrand();
    const update = useUpdateWorkspaceBrand();
    const reset = useResetWorkspaceBrand();
    const upload = useUploadWorkspaceBrandLogo();
    const removeLogo = useDeleteWorkspaceBrandLogo();
    const data = view.data;
    const { draft, setDraft, autosave } = useBrandAutosave(data?.brand, (input) => update.mutateAsync(input));

    const ownSet = !!(data?.brand.display_name || data?.brand.logo_url || data?.brand.accent_color || data?.brand.website_url);
    const source = data?.effective.source;

    return (
        <SectionShell
            title="Branding"
            description="Put your brand on the pages clients see: connect links, shared reports, report emails and forms."
            actions={
                <>
                    <SaveStatus status={autosave.status} onRetry={autosave.retry} />
                    {ownSet && data?.inherited && (
                        <button
                            type="button"
                            disabled={!write.allowed || reset.isPending}
                            onClick={() =>
                                confirm.show("Remove this workspace's brand and use the agency's brand again?", async () => {
                                    await reset.mutateAsync(undefined);
                                })
                            }
                            className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-600 hover:bg-slate-50 disabled:opacity-50"
                        >
                            Use agency brand
                        </button>
                    )}
                </>
            }
        >
            {source === "agency" && !ownSet && (
                <div className="px-4 md:px-8 pt-5">
                    <p className="rounded-md border border-sky-200 bg-sky-50 px-3 py-2 text-[12px] text-sky-900">
                        This workspace shows its agency's brand. Set anything below to give it its own, which replaces the
                        agency's brand as a whole.
                    </p>
                </div>
            )}

            <BrandEditor
                draft={draft}
                setDraft={setDraft}
                inherited={data?.inherited}
                inheritedTheme={source === "agency" ? data?.effective.brand.theme : undefined}
                fallbackName={orgName}
                poweredBy={data?.powered_by}
                entitled={!!data?.effective.full_white_label}
                canWrite={write.allowed}
                logo={{
                    url: data?.brand.logo_url ?? "",
                    upload: (file) => upload.mutateAsync(file),
                    remove: () => removeLogo.mutateAsync(undefined),
                    busy: upload.isPending || removeLogo.isPending,
                }}
            />

            <Section
                eyebrow="Domain"
                description={
                    data?.portal_source === "agency"
                        ? `Connect links and reports are served on your agency's domain, ${data.portal_host}. A domain set here serves this workspace's forms.`
                        : "Serve this workspace's forms on your own subdomain. Connect links and reports are served on the portal address."
                }
            >
                <FormsDomainCard />
            </Section>

            <Section
                eyebrow="Embed on your website"
                description="Put a connect page or a report on your own site. It resizes to fit and keeps your brand."
            >
                <EmbedSnippets baseUrl={data?.portal_base_url ?? ""} />
            </Section>
        </SectionShell>
    );
}
