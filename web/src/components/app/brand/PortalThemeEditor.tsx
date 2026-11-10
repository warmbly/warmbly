// The parts of an agency's brand that shape its portal: the look of the whole dashboard, the
// sign-in page, and where its support and legal links lead.

import React from "react";

import { FieldError, TextInput } from "@/components/ui/field";
import { OptionSelect } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { Row, Section } from "@/app/app/settings/_components/SectionShell";
import type {
    PortalAppearance,
    PortalCorners,
    PortalFont,
    PortalSignInBackground,
    PortalSignInLayout,
} from "@/lib/api/models/app/brand/Brand";
import { HEADING_MAX, MESSAGE_MAX } from "@/lib/displayName";
import { CORNER_RADIUS, FONT_STACKS, loadThemeFont } from "@/lib/portalTheme";
import { cn } from "@/lib/utils";
import AccentField from "./AccentField";
import type { PreviewTab } from "./BrandPreview";
import ImageField, { type BrandImageControl } from "./ImageField";
import { themeErrors, type BrandDraft, type ThemeDraft } from "./useBrandAutosave";

const SIGN_IN_IMAGE_MAX_BYTES = 4 << 20;

// A label and its help above a control that takes the full width.
function Stack({ label, description, children }: { label: string; description?: React.ReactNode; children: React.ReactNode }) {
    return (
        <div>
            <div className="text-[12.5px] font-medium leading-tight text-slate-900">{label}</div>
            {description && <div className="text-[11.5px] text-slate-500 leading-tight mt-0.5">{description}</div>}
            <div className="mt-2">{children}</div>
        </div>
    );
}

// A disabled fieldset disables every control inside it.
function Choices({ disabled, children }: { disabled: boolean; children: React.ReactNode }) {
    return (
        <fieldset disabled={disabled} className="min-w-0 disabled:opacity-60">
            {children}
        </fieldset>
    );
}

function Counter({ value, max }: { value: string; max: number }) {
    const used = Array.from(value).length;
    return (
        <span className={cn("shrink-0 text-[11px] tabular-nums", used > max ? "text-red-600" : "text-slate-400")}>
            {used}/{max}
        </span>
    );
}

// A page in outline: where the brand panel and the form sit in each layout.
function LayoutGlyph({ layout }: { layout: PortalSignInLayout }) {
    const frame = "relative h-7 w-10 shrink-0 overflow-hidden rounded-[3px] border border-slate-300 bg-white";
    const form = "absolute rounded-[1.5px] bg-slate-400";
    if (layout === "split") {
        return (
            <span aria-hidden className={frame}>
                <span className="absolute inset-y-0 left-0 w-1/2 bg-sky-500" />
                <span className={cn(form, "right-[5px] top-[7px] h-3 w-2.5")} />
            </span>
        );
    }
    if (layout === "cover") {
        return (
            <span aria-hidden className={frame}>
                <span className="absolute inset-0 bg-sky-500" />
                <span className="absolute right-[5px] top-[6px] h-3.5 w-3.5 rounded-[1.5px] bg-white" />
            </span>
        );
    }
    return (
        <span aria-hidden className={frame}>
            <span className="absolute inset-x-0 top-0 h-2.5 bg-sky-500" />
            <span className="absolute left-1/2 top-[6px] h-4 w-4 -translate-x-1/2 rounded-[1.5px] border border-slate-300 bg-white" />
        </span>
    );
}

function Labelled({ glyph, children }: { glyph: React.ReactNode; children: React.ReactNode }) {
    return (
        <span className="flex items-center gap-2">
            {glyph}
            <span className="min-w-0">{children}</span>
        </span>
    );
}

const cornerGlyph = (radius: string) => (
    <span aria-hidden className="size-4 shrink-0 border-l-2 border-t-2 border-current opacity-70" style={{ borderTopLeftRadius: radius }} />
);

const swatch = (className: string) => <span aria-hidden className={cn("size-4 shrink-0 rounded-[4px] ring-1 ring-slate-900/10", className)} />;

export default function PortalThemeEditor({
    draft,
    setDraft,
    canWrite,
    brandName,
    signInImage,
    onEdit,
}: {
    draft: BrandDraft & { theme: ThemeDraft };
    setDraft: React.Dispatch<React.SetStateAction<BrandDraft>>;
    canWrite: boolean;
    /** Named in the heading's placeholder. */
    brandName: string;
    signInImage: BrandImageControl;
    /** Says which page an edit shows on, so the preview can turn to it. */
    onEdit: (tab: PreviewTab) => void;
}) {
    const theme = draft.theme;
    const errors = themeErrors(theme);
    const set = (patch: Partial<ThemeDraft>, shows?: PreviewTab) => {
        setDraft((d) => ({ ...d, theme: { ...(d.theme ?? theme), ...patch } }));
        if (shows) onEdit(shows);
    };

    // The font choices are written in their own faces.
    React.useEffect(() => {
        void loadThemeFont("geometric");
        void loadThemeFont("serif");
    }, []);

    return (
        <>
            <Section
                eyebrow="Look"
                description="Applies to the whole dashboard on your portal domain, its sign-in page, and your connect pages and reports."
            >
                <div className="space-y-5">
                    <Row
                        label="Accent colour"
                        description="Buttons, links and highlights. Every shade is drawn from it and kept readable, in light and dark."
                        align="start"
                    >
                        <AccentField
                            value={draft.accent_color}
                            onChange={(hex) => {
                                setDraft((d) => ({ ...d, accent_color: hex }));
                                onEdit("dashboard");
                            }}
                            disabled={!canWrite}
                            showScale
                        />
                    </Row>
                    <Stack label="Font">
                        <Choices disabled={!canWrite}>
                            <OptionSelect<PortalFont>
                                aria-label="Font"
                                cols={2}
                                value={theme.font}
                                onChange={(font) => set({ font }, "dashboard")}
                                options={[
                                    { value: "", label: "Standard", hint: "The default." },
                                    {
                                        value: "system",
                                        label: <span style={{ fontFamily: FONT_STACKS.system }}>System</span>,
                                        hint: "Each device's own font.",
                                    },
                                    {
                                        value: "geometric",
                                        label: <span style={{ fontFamily: FONT_STACKS.geometric }}>Geometric</span>,
                                        hint: "Manrope. Round and modern.",
                                    },
                                    {
                                        value: "serif",
                                        label: <span style={{ fontFamily: FONT_STACKS.serif }}>Serif</span>,
                                        hint: "Source Serif. Classic and editorial.",
                                    },
                                ]}
                            />
                        </Choices>
                    </Stack>
                    <Stack label="Corners" description="How rounded buttons, fields and cards are.">
                        <Choices disabled={!canWrite}>
                            <OptionSelect<PortalCorners>
                                aria-label="Corners"
                                cols={3}
                                value={theme.corners}
                                onChange={(corners) => set({ corners }, "dashboard")}
                                options={[
                                    { value: "sharp", label: <Labelled glyph={cornerGlyph(CORNER_RADIUS.sharp)}>Sharp</Labelled> },
                                    { value: "", label: <Labelled glyph={cornerGlyph("0.375rem")}>Standard</Labelled> },
                                    { value: "round", label: <Labelled glyph={cornerGlyph(CORNER_RADIUS.round)}>Round</Labelled> },
                                ]}
                            />
                        </Choices>
                    </Stack>
                    <Stack label="Default appearance" description="What a client sees until they pick light or dark themselves.">
                        <Choices disabled={!canWrite}>
                            <OptionSelect<Exclude<PortalAppearance, "">>
                                aria-label="Default appearance"
                                cols={2}
                                value={theme.appearance === "dark" ? "dark" : "light"}
                                // Light is the default, so it is stored as no choice.
                                onChange={(v) => set({ appearance: v === "dark" ? "dark" : "" })}
                                options={[
                                    { value: "light", label: <Labelled glyph={swatch("bg-white")}>Light</Labelled> },
                                    { value: "dark", label: <Labelled glyph={swatch("bg-[#111214]")}>Dark</Labelled> },
                                ]}
                            />
                        </Choices>
                    </Stack>
                </div>
            </Section>

            <Section
                eyebrow="Sign-in page"
                description="The first page a client sees on your portal domain, and the page they sign in on when sign-in is on the main address."
            >
                <div className="space-y-5">
                    <Stack label="Layout">
                        <Choices disabled={!canWrite}>
                            <OptionSelect<PortalSignInLayout>
                                aria-label="Sign-in layout"
                                cols={3}
                                value={theme.sign_in_layout}
                                onChange={(sign_in_layout) => set({ sign_in_layout }, "signin")}
                                options={[
                                    { value: "", label: <Labelled glyph={<LayoutGlyph layout="" />}>Centred</Labelled>, hint: "A card under your logo." },
                                    {
                                        value: "split",
                                        label: <Labelled glyph={<LayoutGlyph layout="split" />}>Split</Labelled>,
                                        hint: "Your brand beside the form.",
                                    },
                                    {
                                        value: "cover",
                                        label: <Labelled glyph={<LayoutGlyph layout="cover" />}>Cover</Labelled>,
                                        hint: "The form over a full page.",
                                    },
                                ]}
                            />
                        </Choices>
                    </Stack>
                    <Stack label="Background">
                        <Choices disabled={!canWrite}>
                            <OptionSelect<PortalSignInBackground>
                                aria-label="Sign-in background"
                                cols={2}
                                value={theme.sign_in_background}
                                onChange={(sign_in_background) => set({ sign_in_background }, "signin")}
                                options={[
                                    { value: "", label: <Labelled glyph={swatch("bg-slate-50")}>Plain</Labelled> },
                                    { value: "accent", label: <Labelled glyph={swatch("bg-sky-600")}>Accent colour</Labelled> },
                                    { value: "dark", label: <Labelled glyph={swatch("bg-[#0b0d12]")}>Dark</Labelled> },
                                    {
                                        value: "image",
                                        label: (
                                            <Labelled glyph={swatch("bg-gradient-to-br from-slate-300 via-slate-500 to-slate-800")}>Image</Labelled>
                                        ),
                                    },
                                ]}
                            />
                        </Choices>
                    </Stack>
                    <Row
                        label="Image"
                        description="Shown when the background is Image. PNG or JPG, up to 4 MB and 2560px on each side."
                        align="start"
                    >
                        <ImageField
                            image={signInImage}
                            name="sign-in image"
                            accept={["image/png", "image/jpeg"]}
                            maxBytes={SIGN_IN_IMAGE_MAX_BYTES}
                            canWrite={canWrite}
                            shape="landscape"
                        />
                    </Row>
                    {theme.sign_in_background === "image" && !signInImage.url && (
                        <p className="text-[11.5px] text-amber-700">No image is uploaded yet, so the page shows the accent colour.</p>
                    )}
                    <Stack
                        label="Heading"
                        description={
                            theme.sign_in_layout
                                ? `Shown beside the form. Left empty, it reads "Welcome to ${brandName}".`
                                : "Shown above the form. Left empty, the page shows only your logo and name."
                        }
                    >
                        <div className="flex items-center gap-2">
                            <TextInput
                                value={theme.sign_in_heading}
                                onChange={(sign_in_heading) => set({ sign_in_heading }, "signin")}
                                placeholder={`Welcome to ${brandName}`}
                                invalid={!!errors.sign_in_heading}
                                maxLength={HEADING_MAX}
                                disabled={!canWrite}
                                className="w-full max-w-[420px]"
                            />
                            <Counter value={theme.sign_in_heading} max={HEADING_MAX} />
                        </div>
                        <FieldError message={errors.sign_in_heading} />
                    </Stack>
                    <Stack label="Message" description="A sentence or two under the heading. Plain text, no links.">
                        <div className="flex items-start gap-2">
                            <textarea
                                value={theme.sign_in_message}
                                onChange={(e) => set({ sign_in_message: e.target.value.replace(/\n/g, " ") }, "signin")}
                                placeholder="Your campaigns, replies and reports in one place."
                                maxLength={MESSAGE_MAX}
                                rows={3}
                                disabled={!canWrite}
                                aria-label="Sign-in message"
                                aria-invalid={!!errors.sign_in_message || undefined}
                                className={cn(
                                    "w-full max-w-[420px] min-w-0 resize-y rounded-md border border-slate-200 bg-white px-2.5 py-1.5 text-[16px] md:text-[12.5px] leading-relaxed text-slate-900 placeholder:text-slate-400 outline-none transition-colors focus:border-sky-400 focus:ring-2 focus:ring-sky-100 disabled:bg-slate-50 disabled:text-slate-400",
                                    errors.sign_in_message && "border-red-300 focus:border-red-400 focus:ring-red-100",
                                )}
                            />
                            <Counter value={theme.sign_in_message} max={MESSAGE_MAX} />
                        </div>
                        <FieldError message={errors.sign_in_message} />
                    </Stack>
                </div>
            </Section>

            <Section
                eyebrow="Support and legal"
                description="Where the Support, Terms and Privacy links on your portal and client pages lead. A link left empty is not shown."
            >
                <div className="space-y-4">
                    <LinkRow
                        label="Support email"
                        description="Help links open a new message to this address."
                        value={theme.support_email}
                        onChange={(support_email) => set({ support_email })}
                        placeholder="help@youragency.com"
                        error={errors.support_email}
                        disabled={!canWrite}
                        type="email"
                    />
                    <LinkRow
                        label="Support page"
                        description="Used in place of the email address when both are set. https:// only."
                        value={theme.support_url}
                        onChange={(support_url) => set({ support_url })}
                        placeholder="https://youragency.com/help"
                        error={errors.support_url}
                        disabled={!canWrite}
                    />
                    <LinkRow
                        label="Terms"
                        description="Your terms of service. https:// only."
                        value={theme.terms_url}
                        onChange={(terms_url) => set({ terms_url }, "signin")}
                        placeholder="https://youragency.com/terms"
                        error={errors.terms_url}
                        disabled={!canWrite}
                    />
                    <LinkRow
                        label="Privacy"
                        description="Your privacy policy. https:// only."
                        value={theme.privacy_url}
                        onChange={(privacy_url) => set({ privacy_url }, "signin")}
                        placeholder="https://youragency.com/privacy"
                        error={errors.privacy_url}
                        disabled={!canWrite}
                    />
                </div>
            </Section>
        </>
    );
}

function LinkRow({
    label,
    description,
    value,
    onChange,
    placeholder,
    error,
    disabled,
    type,
}: {
    label: string;
    description: string;
    value: string;
    onChange: (v: string) => void;
    placeholder: string;
    error?: string;
    disabled: boolean;
    type?: string;
}) {
    return (
        <Row label={label} description={description} align="start">
            <div className="w-full sm:w-[300px]">
                <TextInput value={value} onChange={onChange} placeholder={placeholder} invalid={!!error} disabled={disabled} type={type} className="w-full" />
                <FieldError message={error} />
            </div>
        </Row>
    );
}
