// The parts of an agency's brand that shape its portal: the look of the whole dashboard, the
// sign-in page, and where its support and legal links lead.

import React from "react";

import { FieldError, TextInput } from "@/components/ui/field";
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
import { Field, Group, Tiles } from "./controls";
import ImageField, { type BrandImageControl } from "./ImageField";
import { themeErrors, type BrandDraft, type ThemeDraft } from "./useBrandAutosave";

const SIGN_IN_IMAGE_MAX_BYTES = 4 << 20;

function Counter({ value, max }: { value: string; max: number }) {
    const used = Array.from(value).length;
    return (
        <span className={cn("text-[11px] tabular-nums", used > max ? "text-red-600" : "text-slate-400")}>
            {used}/{max}
        </span>
    );
}

// A sign-in page in outline: where the brand and the form sit in each layout.
function LayoutGlyph({ layout }: { layout: PortalSignInLayout }) {
    const page = "relative h-9 w-14 overflow-hidden rounded-[4px] bg-white ring-1 ring-slate-900/10";
    const form = "absolute rounded-[2px] bg-white ring-1 ring-slate-900/15";
    const line = "absolute left-[3px] right-[3px] h-[2px] rounded-full bg-slate-300";
    const card = (className: string) => (
        <span className={cn(form, className)}>
            <span className={cn(line, "top-[4px]")} />
            <span className={cn(line, "top-[8px] bg-sky-500")} />
        </span>
    );
    if (layout === "split") {
        return (
            <span aria-hidden className={page}>
                <span className="absolute inset-y-0 left-0 w-1/2 bg-sky-500" />
                {card("right-[5px] top-[10px] h-4 w-5")}
            </span>
        );
    }
    if (layout === "cover") {
        return (
            <span aria-hidden className={page}>
                <span className="absolute inset-0 bg-sky-500" />
                {card("right-[6px] top-[9px] h-[18px] w-6")}
            </span>
        );
    }
    return (
        <span aria-hidden className={page}>
            <span className="absolute left-1/2 top-[5px] size-[5px] -translate-x-1/2 rounded-[1.5px] bg-sky-500" />
            {card("left-1/2 top-[13px] h-4 w-6 -translate-x-1/2")}
        </span>
    );
}

// A button's outline, at each corner choice.
const cornerGlyph = (radius: string) => (
    <span aria-hidden className="h-5 w-12 border-2 border-slate-400 bg-white" style={{ borderRadius: radius }} />
);

const fontGlyph = (stack?: string) => (
    <span aria-hidden className="text-[20px] font-semibold leading-none text-slate-800" style={stack ? { fontFamily: stack } : undefined}>
        Aa
    </span>
);

// A window in each appearance.
const windowGlyph = (dark: boolean) => (
    <span aria-hidden className={cn("relative h-9 w-14 overflow-hidden rounded-[4px] ring-1 ring-slate-900/10", dark ? "bg-[#111214]" : "bg-white")}>
        <span className={cn("absolute inset-y-0 left-0 w-3.5", dark ? "bg-[#1b1d21]" : "bg-slate-100")} />
        <span className={cn("absolute left-5 top-2 h-[3px] w-6 rounded-full", dark ? "bg-slate-500" : "bg-slate-300")} />
        <span className="absolute left-5 top-[15px] h-[3px] w-4 rounded-full bg-sky-500" />
    </span>
);

const fill = (className: string) => <span aria-hidden className={cn("h-full w-full", className)} />;

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
            <Group title="Look" description="Carried by the whole dashboard on your portal domain, its sign-in page, and your connect pages and reports.">
                <Field label="Accent colour" hint="Buttons, links and highlights. Every shade is drawn from it and kept readable in light and dark.">
                    <AccentField
                        value={draft.accent_color}
                        onChange={(hex) => {
                            setDraft((d) => ({ ...d, accent_color: hex }));
                            onEdit("dashboard");
                        }}
                        disabled={!canWrite}
                        showScale
                    />
                </Field>
                <Field label="Font">
                    <Tiles<PortalFont>
                        label="Font"
                        cols={4}
                        value={theme.font}
                        onChange={(font) => set({ font }, "dashboard")}
                        disabled={!canWrite}
                        options={[
                            { value: "", label: "Standard", glyph: fontGlyph() },
                            { value: "system", label: "System", glyph: fontGlyph(FONT_STACKS.system) },
                            { value: "geometric", label: "Geometric", glyph: fontGlyph(FONT_STACKS.geometric) },
                            { value: "serif", label: "Serif", glyph: fontGlyph(FONT_STACKS.serif) },
                        ]}
                    />
                </Field>
                <Field label="Corners">
                    <Tiles<PortalCorners>
                        label="Corners"
                        cols={3}
                        value={theme.corners}
                        onChange={(corners) => set({ corners }, "dashboard")}
                        disabled={!canWrite}
                        options={[
                            { value: "sharp", label: "Sharp", glyph: cornerGlyph(CORNER_RADIUS.sharp) },
                            { value: "", label: "Standard", glyph: cornerGlyph("0.375rem") },
                            { value: "round", label: "Round", glyph: cornerGlyph("999px") },
                        ]}
                    />
                </Field>
                <Field label="Default appearance" hint="What a client sees until they pick light or dark themselves.">
                    <Tiles<Exclude<PortalAppearance, "">>
                        label="Default appearance"
                        cols={2}
                        value={theme.appearance === "dark" ? "dark" : "light"}
                        // Light is the default, so it is stored as no choice.
                        onChange={(v) => set({ appearance: v === "dark" ? "dark" : "" })}
                        disabled={!canWrite}
                        options={[
                            { value: "light", label: "Light", glyph: windowGlyph(false) },
                            { value: "dark", label: "Dark", glyph: windowGlyph(true) },
                        ]}
                    />
                </Field>
            </Group>

            <Group
                title="Sign-in page"
                description="The first page a client sees on your portal domain, and the page they sign in on when sign-in is on the main address."
            >
                <Field label="Layout">
                    <Tiles<PortalSignInLayout>
                        label="Sign-in layout"
                        cols={3}
                        value={theme.sign_in_layout}
                        onChange={(sign_in_layout) => set({ sign_in_layout }, "signin")}
                        disabled={!canWrite}
                        options={[
                            { value: "", label: "Centred", glyph: <LayoutGlyph layout="" /> },
                            { value: "split", label: "Split", glyph: <LayoutGlyph layout="split" /> },
                            { value: "cover", label: "Cover", glyph: <LayoutGlyph layout="cover" /> },
                        ]}
                    />
                </Field>
                <Field label="Background">
                    <Tiles<PortalSignInBackground>
                        label="Sign-in background"
                        cols={4}
                        value={theme.sign_in_background}
                        onChange={(sign_in_background) => set({ sign_in_background }, "signin")}
                        disabled={!canWrite}
                        options={[
                            { value: "", label: "Plain", glyph: fill("bg-slate-50") },
                            { value: "accent", label: "Accent", glyph: fill("bg-gradient-to-br from-sky-500 to-sky-700") },
                            { value: "dark", label: "Dark", glyph: fill("bg-[#0b0d12]") },
                            { value: "image", label: "Image", glyph: fill("bg-gradient-to-br from-slate-300 via-slate-500 to-slate-800") },
                        ]}
                    />
                </Field>
                {(theme.sign_in_background === "image" || !!signInImage.url) && (
                    <Field
                        label="Background image"
                        hint={
                            theme.sign_in_background === "image" && !signInImage.url
                                ? "No image is uploaded yet, so the page shows the accent colour. PNG or JPG, up to 4 MB and 2560px on each side."
                                : "PNG or JPG, up to 4 MB and 2560px on each side."
                        }
                    >
                        <ImageField
                            image={signInImage}
                            name="sign-in image"
                            accept={["image/png", "image/jpeg"]}
                            maxBytes={SIGN_IN_IMAGE_MAX_BYTES}
                            canWrite={canWrite}
                            shape="landscape"
                        />
                    </Field>
                )}
                <Field
                    labelled
                    label="Heading"
                    aside={<Counter value={theme.sign_in_heading} max={HEADING_MAX} />}
                    hint={
                        theme.sign_in_layout
                            ? `Shown beside the form. Left empty, it reads "Welcome to ${brandName}".`
                            : "Shown above the form. Left empty, the page shows only your logo and name."
                    }
                >
                    <TextInput
                        value={theme.sign_in_heading}
                        onChange={(sign_in_heading) => set({ sign_in_heading }, "signin")}
                        placeholder={`Welcome to ${brandName}`}
                        invalid={!!errors.sign_in_heading}
                        maxLength={HEADING_MAX}
                        disabled={!canWrite}
                        className="w-full"
                    />
                    <FieldError message={errors.sign_in_heading} />
                </Field>
                <Field
                    labelled
                    label="Message"
                    aside={<Counter value={theme.sign_in_message} max={MESSAGE_MAX} />}
                    hint="A sentence or two under the heading. Plain text, no links."
                >
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
                            "block w-full min-w-0 resize-y rounded-md border border-slate-200 bg-white px-2.5 py-1.5 text-[16px] md:text-[12.5px] leading-relaxed text-slate-900 placeholder:text-slate-400 outline-none transition-colors focus:border-sky-400 focus:ring-2 focus:ring-sky-100 disabled:bg-slate-50 disabled:text-slate-400",
                            errors.sign_in_message && "border-red-300 focus:border-red-400 focus:ring-red-100",
                        )}
                    />
                    <FieldError message={errors.sign_in_message} />
                </Field>
            </Group>

            <Group title="Support and legal" description="Where the Support, Terms and Privacy links on your portal and client pages lead. A link left empty is not shown.">
                <LinkField
                    label="Support email"
                    hint="Help links open a new message to this address."
                    value={theme.support_email}
                    onChange={(support_email) => set({ support_email })}
                    placeholder="help@youragency.com"
                    error={errors.support_email}
                    disabled={!canWrite}
                    type="email"
                />
                <LinkField
                    label="Support page"
                    hint="Used in place of the email address when both are set."
                    value={theme.support_url}
                    onChange={(support_url) => set({ support_url })}
                    placeholder="https://youragency.com/help"
                    error={errors.support_url}
                    disabled={!canWrite}
                />
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                    <LinkField
                        label="Terms"
                        value={theme.terms_url}
                        onChange={(terms_url) => set({ terms_url }, "signin")}
                        placeholder="https://youragency.com/terms"
                        error={errors.terms_url}
                        disabled={!canWrite}
                    />
                    <LinkField
                        label="Privacy"
                        value={theme.privacy_url}
                        onChange={(privacy_url) => set({ privacy_url }, "signin")}
                        placeholder="https://youragency.com/privacy"
                        error={errors.privacy_url}
                        disabled={!canWrite}
                    />
                </div>
            </Group>
        </>
    );
}

function LinkField({
    label,
    hint,
    value,
    onChange,
    placeholder,
    error,
    disabled,
    type,
}: {
    label: string;
    hint?: string;
    value: string;
    onChange: (v: string) => void;
    placeholder: string;
    error?: string;
    disabled: boolean;
    type?: string;
}) {
    return (
        <Field labelled label={label} hint={hint}>
            <TextInput value={value} onChange={onChange} placeholder={placeholder} invalid={!!error} disabled={disabled} type={type} className="w-full" />
            <FieldError message={error} />
        </Field>
    );
}
