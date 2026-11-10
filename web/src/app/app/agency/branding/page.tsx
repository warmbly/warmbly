// Agency > Branding: the brand every client workspace shows until it sets its
// own, the look of the agency's portal and its sign-in page, the portal domain
// all their client pages live on, and embed code for the agency's website.

import React from "react";
import { AgencyPage } from "@/components/app/agency/AgencyPage";
import { Loader2Icon } from "lucide-react";
import { PageBody } from "@/components/layout/Page";
import { useCurrentAgency } from "@/lib/api/hooks/app/agency/useAgency";
import toast from "react-hot-toast/headless";
import {
    useAgencyBrand,
    useDeleteAgencyBrandImage,
    useSetAgencyPortalDomain,
    useSetAgencyPortalSignIn,
    useUpdateAgencyBrand,
    useUploadAgencyBrandImage,
    useVerifyAgencyPortalDomain,
} from "@/lib/api/hooks/app/brand";
import BrandEditor from "@/components/app/brand/BrandEditor";
import { useBrandAutosave } from "@/components/app/brand/useBrandAutosave";
import EmbedSnippets from "@/components/app/brand/EmbedSnippets";
import { CustomDomainPanel } from "@/components/app/forms/FormsDomainCard";
import { Section, SectionShell } from "@/app/app/settings/_components/SectionShell";
import SaveStatus from "@/app/app/settings/_components/SaveStatus";
import { OptionSelect } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import type { AppError } from "@/lib/api/client/normalizeError";
import type { AgencyBrandImage, PortalSignIn, PortalTheme } from "@/lib/api/models/app/brand/Brand";
import { applyBrandTheme } from "@/lib/portalTheme";
import type { BrandImageControl } from "@/components/app/brand/ImageField";
import buildError from "@/lib/helper/buildError";
import { APP_URL } from "@/lib/information";
import { currentPortal, mainHost } from "@/lib/portal";

/** The main dashboard's host, named in the sign-in choice. */
function mainAddress(): string {
    const portal = currentPortal();
    if (portal.portal) return mainHost(portal);
    try {
        return new URL(APP_URL).host || "the main address";
    } catch {
        return "the main address";
    }
}

function AgencyBrandingForCurrent() {
    const { agency } = useCurrentAgency();
    if (!agency) return null;
    // Keyed on the agency so switching agencies re-seeds the editor.
    return <AgencyBranding key={agency.id} agencyId={agency.id} agencyName={agency.name} />;
}

// One of the brand's uploaded images, with its own upload and delete routes.
function useBrandImage(agencyId: string, image: AgencyBrandImage, url: string | undefined): BrandImageControl {
    const upload = useUploadAgencyBrandImage(agencyId, image);
    const remove = useDeleteAgencyBrandImage(agencyId, image);
    return {
        url: url ?? "",
        upload: (file) => upload.mutateAsync(file),
        remove: () => remove.mutateAsync(undefined),
        busy: upload.isPending || remove.isPending,
    };
}

function AgencyBranding({ agencyId, agencyName }: { agencyId: string; agencyName: string }) {
    const view = useAgencyBrand(agencyId);
    const update = useUpdateAgencyBrand(agencyId);
    const setDomain = useSetAgencyPortalDomain(agencyId);
    const verifyDomain = useVerifyAgencyPortalDomain(agencyId);
    const setSignIn = useSetAgencyPortalSignIn(agencyId);
    const data = view.data;
    const logo = useBrandImage(agencyId, "logo", data?.brand.logo_url);
    const icon = useBrandImage(agencyId, "icon", data?.icon_url);
    const signInImage = useBrandImage(agencyId, "sign-in-image", data?.sign_in_image_url);
    const canWrite = !!data?.can_manage;
    // The theme travels in the same PUT as the other fields, so one save never undoes another.
    const { draft, setDraft, autosave, payload } = useBrandAutosave(data?.brand, (input) => update.mutateAsync(input), data?.theme);

    // On the agency's own portal domain, the dashboard takes a saved look at once, not on the next visit.
    const savedLook = data ? JSON.stringify([data.brand.accent_color, data.theme ?? {}]) : "";
    React.useEffect(() => {
        const portal = currentPortal();
        if (!savedLook || !portal.portal || portal.agency_id !== agencyId) return;
        const [accent_color, theme] = JSON.parse(savedLook) as [string, PortalTheme];
        applyBrandTheme({ accent_color, theme }, true);
    }, [savedLook, agencyId]);

    if (view.isLoading) {
        return (
            <div className="flex-1 flex items-center justify-center py-20">
                <Loader2Icon className="w-4 h-4 animate-spin text-slate-400" />
            </div>
        );
    }

    return (
        // The shell scrolls here, so the preview sticks to it, below the page's own bar.
        <PageBody className="overflow-visible [--brand-preview-top:4rem]">
            <SectionShell
                title="Branding"
                description="Every client workspace shows this brand on its connect links, shared reports, report emails and forms, unless it sets its own. Your portal domain carries it across the whole dashboard."
                actions={<SaveStatus status={autosave.status} onRetry={autosave.retry} />}
            >
                {!canWrite && (
                    <div className="px-4 md:px-8 pt-5">
                        <p className="rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-[12px] text-slate-600">
                            Only an agency owner or admin can change the branding.
                        </p>
                    </div>
                )}

                <BrandEditor
                    draft={draft}
                    setDraft={setDraft}
                    fallbackName={agencyName}
                    poweredBy={data?.powered_by}
                    entitled={!!data?.full_white_label}
                    canWrite={canWrite}
                    logo={logo}
                    // A backend from before the portal theme answers without one, and has no routes for these two.
                    icon={data?.theme ? icon : undefined}
                    signInImage={data?.theme ? signInImage : undefined}
                />

                <Section
                    eyebrow="Portal domain"
                    description="One subdomain for every client's connect links and reports, where clients can open their dashboard too. Forms stay on the forms host or a workspace's own forms domain."
                >
                    <CustomDomainPanel
                        label="Portal domain"
                        placeholder="portal.youragency.com"
                        links="client links"
                        target={{ name: "portal address", setting: "PORTAL_DOMAIN" }}
                        status={data?.portal_domain}
                        canWrite={canWrite}
                        guard={(fn) => fn()}
                        saving={setDomain.isPending}
                        verifying={verifyDomain.isPending}
                        onSave={async (d) => (await setDomain.mutateAsync(d)).portal_domain}
                        onVerify={async () => (await verifyDomain.mutateAsync(undefined)).portal_domain}
                    />
                </Section>

                <Section
                    eyebrow="Where clients sign in"
                    description="Either way, a session signed in for your portal reaches only your client workspaces. Passwords, two-factor and other account changes are made on the main address."
                >
                    {canWrite ? (
                        <OptionSelect<PortalSignIn>
                            aria-label="Where clients sign in"
                            cols={2}
                            value={data?.portal_sign_in ?? "portal"}
                            onChange={(v) => {
                                if (v === data?.portal_sign_in) return;
                                // The PUT carries the editor's fields so it never undoes an edit still saving.
                                setSignIn.mutate({ ...payload, portal_sign_in: v }, {
                                    onError: (err) => toast.error(buildError(err as unknown as AppError)),
                                });
                            }}
                            options={[
                                {
                                    value: "portal",
                                    label: "On your portal domain",
                                    hint: "The sign-in form is served from your domain, so it receives what clients type.",
                                },
                                {
                                    value: "warmbly",
                                    label: `On ${mainAddress()}`,
                                    hint: "Clients sign in on a page there that shows your brand, then return to your portal.",
                                },
                            ]}
                        />
                    ) : (
                        <p className="text-[12.5px] text-slate-600">
                            {data?.portal_sign_in === "warmbly" ? `Clients sign in on ${mainAddress()}.` : "Clients sign in on your portal domain."}
                        </p>
                    )}
                </Section>

                <Section
                    eyebrow="Embed on your website"
                    description="Put a client's connect page or report on your own site. It resizes to fit and keeps your brand."
                >
                    <EmbedSnippets baseUrl={data?.portal_base_url ?? ""} />
                </Section>
            </SectionShell>
        </PageBody>
    );
}

export default function AgencyBrandingPage() {
    return (
        <AgencyPage title="Branding">
            <AgencyBrandingForCurrent />
        </AgencyPage>
    );
}
