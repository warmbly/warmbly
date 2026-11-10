// BrandEditor: the fields of one brand (a workspace's or an agency's), autosaved, beside a live
// preview of the pages that carry it. An agency's brand also has a theme: the look of its portal,
// its sign-in page and its support and legal links. The "Powered by" switch is for the plans and
// instances that can turn it off.

import React from "react";
import { LockIcon } from "lucide-react";

import { FieldError, TextInput } from "@/components/ui/field";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { Row, Section } from "@/app/app/settings/_components/SectionShell";
import { useUpgradeDialog } from "@/hooks/context/upgrade";
import { WORKSPACE_NAME_MAX, nameError } from "@/lib/displayName";
import type { BrandSettings, PortalTheme, PublicBrand } from "@/lib/api/models/app/brand/Brand";
import AccentField from "./AccentField";
import BrandPreview, { type PreviewTab } from "./BrandPreview";
import ImageField, { type BrandImageControl } from "./ImageField";
import PortalThemeEditor from "./PortalThemeEditor";
import { websiteError, type BrandDraft, type ThemeDraft } from "./useBrandAutosave";

const LOGO_MAX_BYTES = 2 << 20;
const ICON_MAX_BYTES = 1 << 20;

const CLIENT_PAGE_TABS: PreviewTab[] = ["connect", "report"];

export default function BrandEditor({
    draft,
    setDraft,
    inherited,
    inheritedTheme,
    fallbackName,
    poweredBy,
    entitled,
    canWrite,
    logo,
    icon,
    signInImage,
}: {
    draft: BrandDraft;
    setDraft: React.Dispatch<React.SetStateAction<BrandDraft>>;
    /** The agency brand a workspace shows while it sets none of its own, and that brand's theme. */
    inherited?: BrandSettings;
    inheritedTheme?: PortalTheme;
    /** Shown in the preview when no name is set. */
    fallbackName: string;
    /** The deployment's attribution, absent when it configured none. */
    poweredBy?: PublicBrand["powered_by"];
    entitled: boolean;
    canWrite: boolean;
    logo: BrandImageControl;
    /** An agency's tab icon and sign-in image; a workspace's brand has neither. */
    icon?: BrandImageControl;
    signInImage?: BrandImageControl;
}) {
    const upgrade = useUpgradeDialog();
    const nameInvalid = nameError("Brand name", draft.display_name, "workspace", true);
    const websiteInvalid = websiteError(draft.website_url);
    const set = (patch: Partial<BrandDraft>) => setDraft((d) => ({ ...d, ...patch }));
    // Only an agency's brand has a theme, and with it the portal's own pages to preview.
    const themed = draft.theme && signInImage ? { theme: draft.theme, signInImage } : null;
    const [previewTab, setPreviewTab] = React.useState<PreviewTab | undefined>(undefined);

    // Inheritance is all or nothing: the preview shows the agency's brand until this one names anything.
    const ownSet = !!(draft.display_name.trim() || logo.url || draft.accent_color || draft.website_url.trim());
    const shown: PublicBrand =
        !ownSet && inherited
            ? {
                  name: inherited.display_name,
                  logo_url: inherited.logo_url,
                  accent_color: inherited.accent_color,
                  website_url: inherited.website_url,
                  theme: inheritedTheme,
              }
            : {
                  name: draft.display_name.trim(),
                  logo_url: logo.url,
                  icon_url: icon?.url,
                  sign_in_image_url: signInImage?.url,
                  accent_color: draft.accent_color,
                  website_url: draft.website_url.trim(),
                  theme: draft.theme,
              };
    const hideOn = (ownSet || !inherited ? draft.hide_powered_by : inherited.hide_powered_by) && entitled;
    shown.powered_by = hideOn ? undefined : poweredBy;

    return (
        <div className="@container/brand">
            <div className="@4xl/brand:grid @4xl/brand:grid-cols-[minmax(0,1fr)_minmax(0,46%)]">
                {/* Above the fields where there is no room beside them; beside them, it stays in view while they scroll. */}
                <aside className="min-w-0 px-4 pt-5 md:px-8 @4xl/brand:order-2 @4xl/brand:py-6 @4xl/brand:pl-2">
                    <div className="@4xl/brand:sticky @4xl/brand:top-[var(--brand-preview-top,1rem)]">
                        <BrandPreview
                            brand={shown}
                            fallbackName={fallbackName}
                            tabs={themed ? undefined : CLIENT_PAGE_TABS}
                            tab={previewTab}
                            onTabChange={setPreviewTab}
                        />
                    </div>
                </aside>

                <div className="min-w-0 divide-y divide-slate-200/70 @4xl/brand:order-1">
                    <Section
                        eyebrow="Identity"
                        description={
                            themed
                                ? "Who clients see on your portal, connect pages, shared reports and forms."
                                : "What clients see in the header of their connect page, shared reports and forms."
                        }
                    >
                        <div className="space-y-4">
                            <Row label="Name" description="Shown beside the logo, and on its own when there is no logo." align="start">
                                <div className="w-full sm:w-[280px]">
                                    <TextInput
                                        value={draft.display_name}
                                        onChange={(v) => set({ display_name: v })}
                                        placeholder={inherited?.display_name || fallbackName}
                                        invalid={!!nameInvalid}
                                        maxLength={WORKSPACE_NAME_MAX}
                                        disabled={!canWrite}
                                        className="w-full"
                                    />
                                    <FieldError message={nameInvalid} />
                                </div>
                            </Row>
                            <Row label="Logo" description="PNG or JPG, up to 2 MB and 1024px on each side. A wide logo reads best." align="start">
                                <ImageField image={logo} name="logo" accept={["image/png", "image/jpeg"]} maxBytes={LOGO_MAX_BYTES} canWrite={canWrite} />
                            </Row>
                            {icon && (
                                <Row
                                    label="Tab icon"
                                    description="The small square in the browser tab on your portal. PNG, up to 1 MB and 512px on each side. Without one, the logo is used."
                                    align="start"
                                >
                                    <ImageField image={icon} name="icon" accept={["image/png"]} maxBytes={ICON_MAX_BYTES} canWrite={canWrite} shape="square" />
                                </Row>
                            )}
                            {!themed && (
                                <Row label="Accent colour" description="Buttons and highlights on client pages." align="start">
                                    <AccentField value={draft.accent_color} onChange={(hex) => set({ accent_color: hex })} disabled={!canWrite} />
                                </Row>
                            )}
                            <Row label="Website" description="The name and logo link here. https:// only." align="start">
                                <div className="w-full sm:w-[300px]">
                                    <TextInput
                                        value={draft.website_url}
                                        onChange={(v) => set({ website_url: v })}
                                        placeholder="https://youragency.com"
                                        invalid={!!websiteInvalid}
                                        disabled={!canWrite}
                                        className="w-full"
                                    />
                                    <FieldError message={websiteInvalid} />
                                </div>
                            </Row>
                        </div>
                    </Section>

                    {themed && (
                        <PortalThemeEditor
                            draft={draft as BrandDraft & { theme: ThemeDraft }}
                            setDraft={setDraft}
                            canWrite={canWrite}
                            brandName={draft.display_name.trim() || fallbackName}
                            signInImage={themed.signInImage}
                            onEdit={setPreviewTab}
                        />
                    )}

                    {poweredBy && (
                        <Section
                            eyebrow="Attribution"
                            description={`Client pages end with "Powered by ${poweredBy.name}" unless your plan lets you remove it.`}
                        >
                            <Row
                                label={`Hide "Powered by ${poweredBy.name}"`}
                                description={
                                    entitled
                                        ? "Connect pages, shared reports, report emails and forms carry only your brand."
                                        : "Included with Agency Pro and Enterprise."
                                }
                            >
                                <div className="flex items-center gap-2">
                                    {!entitled && !draft.hide_powered_by && (
                                        <button
                                            type="button"
                                            onClick={() =>
                                                upgrade.open({
                                                    feature: "Full white-label",
                                                    minPlan: "enterprise",
                                                    blurb: `Remove "Powered by ${poweredBy.name}" from every page your clients see.`,
                                                })
                                            }
                                            className="h-6 px-2 rounded-md bg-sky-50 text-sky-700 text-[11.5px] font-medium hover:bg-sky-100 inline-flex items-center gap-1"
                                        >
                                            <LockIcon className="w-3 h-3" />
                                            Upgrade
                                        </button>
                                    )}
                                    <Toggle
                                        value={draft.hide_powered_by}
                                        onChange={(v) => set({ hide_powered_by: v })}
                                        disabled={!canWrite || (!entitled && !draft.hide_powered_by)}
                                        ariaLabel={`Hide "Powered by ${poweredBy.name}"`}
                                    />
                                </div>
                            </Row>
                            {!entitled && draft.hide_powered_by && (
                                <p className="text-[11.5px] text-amber-700">
                                    Your current plan does not include this, so client pages still show the attribution.
                                </p>
                            )}
                        </Section>
                    )}
                </div>
            </div>
        </div>
    );
}
