// BrandEditor — the fields of one brand (a workspace's or an agency's): name,
// logo, accent and website, autosaved, plus the "Powered by" switch that only
// Agency Pro, Enterprise and self-hosted instances can turn off. The preview
// beside it renders what a client sees on a connect page and a shared report.

import React from "react";
import { ImageIcon, LockIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { FieldError, Label, TextInput } from "@/components/ui/field";
import ColorPicker from "@/components/ui/color-picker";
import { DashboardImage } from "@/components/ui/dashboard-image";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { Row, Section } from "@/app/app/settings/_components/SectionShell";
import { useUpgradeDialog } from "@/hooks/context/upgrade";
import { WORKSPACE_NAME_MAX, nameError } from "@/lib/displayName";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import type { BrandInput, BrandSettings, PublicBrand } from "@/lib/api/models/app/brand/Brand";
import BrandPreview from "./BrandPreview";
import { websiteError } from "./useBrandAutosave";

const LOGO_MAX_BYTES = 2 << 20;
const DEFAULT_ACCENT = "#0284c7";

export default function BrandEditor({
    draft,
    setDraft,
    stored,
    inherited,
    fallbackName,
    poweredBy,
    entitled,
    canWrite,
    onUpload,
    onRemoveLogo,
    logoBusy,
}: {
    draft: BrandInput;
    setDraft: React.Dispatch<React.SetStateAction<BrandInput>>;
    stored: BrandSettings | undefined;
    /** The agency brand a workspace shows while it sets none of its own. */
    inherited?: BrandSettings;
    /** Shown in the preview when no name is set. */
    fallbackName: string;
    /** The deployment's attribution, absent when it configured none. */
    poweredBy?: PublicBrand["powered_by"];
    entitled: boolean;
    canWrite: boolean;
    onUpload: (file: File) => Promise<unknown>;
    onRemoveLogo: () => Promise<unknown>;
    logoBusy: boolean;
}) {
    const upgrade = useUpgradeDialog();
    const fileRef = React.useRef<HTMLInputElement>(null);
    const nameInvalid = nameError("Brand name", draft.display_name, "workspace", true);
    const websiteInvalid = websiteError(draft.website_url);
    const logoURL = stored?.logo_url ?? "";
    const set = (patch: Partial<BrandInput>) => setDraft((d) => ({ ...d, ...patch }));

    // Inheritance is all or nothing: the preview shows the agency's brand until this one names anything.
    const ownSet = !!(draft.display_name.trim() || logoURL || draft.accent_color || draft.website_url.trim());
    const shown: PublicBrand =
        !ownSet && inherited
            ? {
                  name: inherited.display_name,
                  logo_url: inherited.logo_url,
                  accent_color: inherited.accent_color,
                  website_url: inherited.website_url,
              }
            : {
                  name: draft.display_name.trim(),
                  logo_url: logoURL,
                  accent_color: draft.accent_color,
                  website_url: draft.website_url.trim(),
              };
    const hideOn = (ownSet || !inherited ? draft.hide_powered_by : inherited.hide_powered_by) && entitled;
    shown.powered_by = hideOn ? undefined : poweredBy;

    async function pick(file: File) {
        if (file.size > LOGO_MAX_BYTES) {
            toast.error("The logo must be 2 MB or smaller. Compress or resize it and try again.");
            return;
        }
        try {
            await onUpload(file);
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    async function removeLogo() {
        try {
            await onRemoveLogo();
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    return (
        <>
            <Section eyebrow="Identity" description="What clients see in the header of their connect page, shared reports and forms.">
                <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(0,420px)]">
                    <div className="space-y-4 min-w-0">
                        <Row label="Name" description="Shown beside the logo, and on its own when there is no logo." align="start">
                            <div className="w-full max-w-[280px]">
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
                            <div className="flex items-center gap-2">
                                <span className="h-9 w-20 rounded-md border border-slate-200 bg-slate-50 shrink-0 overflow-hidden inline-flex items-center justify-center">
                                    {logoURL ? (
                                        <DashboardImage src={logoURL} alt="" className="max-h-full max-w-full object-contain" />
                                    ) : (
                                        <ImageIcon className="w-3.5 h-3.5 text-slate-300" />
                                    )}
                                </span>
                                <button
                                    type="button"
                                    disabled={!canWrite || logoBusy}
                                    onClick={() => fileRef.current?.click()}
                                    className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-600 hover:bg-slate-50 disabled:opacity-50"
                                >
                                    {logoBusy ? "Working…" : logoURL ? "Replace" : "Upload"}
                                </button>
                                {logoURL && !logoBusy && (
                                    <button
                                        type="button"
                                        disabled={!canWrite}
                                        onClick={() => void removeLogo()}
                                        className="h-7 px-2 rounded-md text-[12px] text-slate-500 hover:text-rose-600 hover:bg-rose-50 disabled:opacity-50"
                                    >
                                        Remove
                                    </button>
                                )}
                                <input
                                    ref={fileRef}
                                    type="file"
                                    accept="image/png,image/jpeg"
                                    className="hidden"
                                    onChange={(e) => {
                                        const f = e.target.files?.[0];
                                        e.target.value = "";
                                        if (f) void pick(f);
                                    }}
                                />
                            </div>
                        </Row>
                        <Row label="Accent colour" description="Buttons and highlights on client pages." align="start">
                            <div className="flex items-center gap-1.5">
                                <ColorPicker
                                    value={draft.accent_color}
                                    fallback={DEFAULT_ACCENT}
                                    onChange={(hex) => set({ accent_color: hex.toLowerCase() })}
                                    disabled={!canWrite}
                                    aria-label="Accent colour"
                                />
                                {draft.accent_color && (
                                    <button
                                        type="button"
                                        disabled={!canWrite}
                                        onClick={() => set({ accent_color: "" })}
                                        aria-label="Use the default colour"
                                        className="size-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 disabled:opacity-50"
                                    >
                                        <XIcon className="w-3.5 h-3.5" />
                                    </button>
                                )}
                            </div>
                        </Row>
                        <Row label="Website" description="The name and logo link here. https:// only." align="start">
                            <div className="w-full max-w-[320px]">
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
                    <div className="min-w-0">
                        <Label>Preview</Label>
                        <BrandPreview brand={shown} fallbackName={fallbackName} />
                    </div>
                </div>
            </Section>

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
        </>
    );
}
