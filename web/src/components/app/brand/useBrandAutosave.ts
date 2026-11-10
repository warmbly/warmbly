// useBrandAutosave keeps a brand's editable fields in a draft and autosaves it,
// refusing locally what the server would refuse.

import React from "react";
import toast from "react-hot-toast/headless";

import { useAutosave, type AutosaveHandle } from "@/hooks/useAutosave";
import { useRegisterUnsaved } from "@/hooks/context/unsaved";
import { nameError, normalizeName } from "@/lib/displayName";
import { supportEmail } from "@/lib/portalTheme";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import type { BrandInput, BrandSettings, PortalTheme } from "@/lib/api/models/app/brand/Brand";

const ACCENT_RE = /^#[0-9a-f]{6}$/;

/** A theme with every field present, so each control is controlled; "" is the default. */
export type ThemeDraft = Required<PortalTheme>;

/** A brand's editable fields. Only an agency's brand has a theme, and its PUT carries it with the rest. */
export type BrandDraft = BrandInput & { theme?: ThemeDraft };

const EMPTY: BrandInput = { display_name: "", accent_color: "", website_url: "", hide_powered_by: false };

export const EMPTY_THEME: ThemeDraft = {
    font: "",
    corners: "",
    appearance: "",
    sign_in_layout: "",
    sign_in_background: "",
    sign_in_heading: "",
    sign_in_message: "",
    support_email: "",
    support_url: "",
    terms_url: "",
    privacy_url: "",
};

function toDraft(b: BrandSettings | undefined, theme: PortalTheme | undefined): BrandDraft {
    const input: BrandInput = b
        ? {
              display_name: b.display_name,
              accent_color: b.accent_color,
              website_url: b.website_url,
              hide_powered_by: b.hide_powered_by,
          }
        : EMPTY;
    return theme ? { ...input, theme: { ...EMPTY_THEME, ...theme } } : input;
}

/** The refusal the server would give, worded for the field. */
export function websiteError(v: string): string | null {
    const s = v.trim();
    if (!s) return null;
    try {
        const u = new URL(s);
        if (u.protocol !== "https:" || !u.hostname.includes(".") || u.username || u.password) throw new Error();
        return null;
    } catch {
        return "Use a full https:// address, like https://example.com.";
    }
}

export type ThemeErrors = Partial<Record<keyof ThemeDraft, string>>;

/** What the server would refuse in a theme, by field. */
export function themeErrors(t: ThemeDraft): ThemeErrors {
    const out: ThemeErrors = {};
    const put = (key: keyof ThemeDraft, message: string | null) => {
        if (message) out[key] = message;
    };
    put("sign_in_heading", nameError("The heading", t.sign_in_heading, "heading", true));
    put("sign_in_message", nameError("The message", t.sign_in_message, "message", true));
    put(
        "support_email",
        t.support_email.trim() && !supportEmail(t.support_email) ? "Use a plain email address, like help@example.com." : null,
    );
    put("support_url", websiteError(t.support_url));
    put("terms_url", websiteError(t.terms_url));
    put("privacy_url", websiteError(t.privacy_url));
    return out;
}

function inputError(v: BrandDraft): string | null {
    return (
        nameError("Brand name", v.display_name, "workspace", true) ??
        (v.accent_color && !ACCENT_RE.test(v.accent_color) ? "The accent colour must be a hex colour like #0284c7." : null) ??
        websiteError(v.website_url) ??
        (v.theme ? (Object.values(themeErrors(v.theme))[0] ?? null) : null)
    );
}

function normalize(d: BrandDraft): BrandDraft {
    const out: BrandDraft = { ...d, display_name: normalizeName(d.display_name), website_url: d.website_url.trim() };
    if (d.theme) {
        out.theme = {
            ...d.theme,
            sign_in_heading: normalizeName(d.theme.sign_in_heading),
            sign_in_message: normalizeName(d.theme.sign_in_message),
            support_email: d.theme.support_email.trim(),
            support_url: d.theme.support_url.trim(),
            terms_url: d.theme.terms_url.trim(),
            privacy_url: d.theme.privacy_url.trim(),
        };
    }
    return out;
}

const sameInput = (a: BrandDraft, b: BrandDraft) =>
    a.display_name === b.display_name &&
    a.accent_color === b.accent_color &&
    a.website_url === b.website_url &&
    a.hide_powered_by === b.hide_powered_by &&
    JSON.stringify(a.theme ?? null) === JSON.stringify(b.theme ?? null);

export function useBrandAutosave(
    stored: BrandSettings | undefined,
    save: (input: BrandDraft) => Promise<unknown>,
    /** An agency's stored theme; leave out for a brand that has none. */
    storedTheme?: PortalTheme,
): {
    draft: BrandDraft;
    setDraft: React.Dispatch<React.SetStateAction<BrandDraft>>;
    autosave: AutosaveHandle<BrandDraft>;
    /** The fields another write to the same brand carries: the draft when the server would take it, else what was last saved. */
    payload: BrandDraft;
} {
    const [draft, setDraft] = React.useState<BrandDraft>(() => toDraft(stored, storedTheme));
    const value = React.useMemo(() => normalize(draft), [draft]);
    const autosave = useAutosave({
        value,
        debounceMs: 700,
        isEqual: sameInput,
        save: async (v) => {
            const invalid = inputError(v);
            if (invalid) throw new Error(invalid);
            try {
                await save(v);
            } catch (err) {
                toast.error(buildError(err as AppError));
                throw err;
            }
        },
    });
    useRegisterUnsaved(autosave, () => setDraft(autosave.savedValue));

    // Server data moves the baseline; the editor only re-seeds while nothing is pending.
    const storedKey = stored ? JSON.stringify(toDraft(stored, storedTheme)) : "";
    React.useEffect(() => {
        if (!stored) return;
        const next = toDraft(stored, storedTheme);
        autosave.markSaved(next);
        if (!autosave.dirty) setDraft(next);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [storedKey]);

    const payload = inputError(value) ? autosave.savedValue : value;
    return { draft, setDraft, autosave, payload };
}
