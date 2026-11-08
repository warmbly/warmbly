// useBrandAutosave keeps a brand's editable fields in a draft and autosaves it,
// refusing locally what the server would refuse.

import React from "react";
import toast from "react-hot-toast/headless";

import { useAutosave, type AutosaveHandle } from "@/hooks/useAutosave";
import { useRegisterUnsaved } from "@/hooks/context/unsaved";
import { nameError, normalizeName } from "@/lib/displayName";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import type { BrandInput, BrandSettings } from "@/lib/api/models/app/brand/Brand";

const ACCENT_RE = /^#[0-9a-f]{6}$/;

const EMPTY: BrandInput = { display_name: "", accent_color: "", website_url: "", hide_powered_by: false };

function toInput(b: BrandSettings | undefined): BrandInput {
    if (!b) return EMPTY;
    return {
        display_name: b.display_name,
        accent_color: b.accent_color,
        website_url: b.website_url,
        hide_powered_by: b.hide_powered_by,
    };
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

function inputError(v: BrandInput): string | null {
    return (
        nameError("Brand name", v.display_name, "workspace", true) ??
        (v.accent_color && !ACCENT_RE.test(v.accent_color) ? "The accent colour must be a hex colour like #0284c7." : null) ??
        websiteError(v.website_url)
    );
}

const sameInput = (a: BrandInput, b: BrandInput) =>
    a.display_name === b.display_name &&
    a.accent_color === b.accent_color &&
    a.website_url === b.website_url &&
    a.hide_powered_by === b.hide_powered_by;

export function useBrandAutosave(
    stored: BrandSettings | undefined,
    save: (input: BrandInput) => Promise<unknown>,
): { draft: BrandInput; setDraft: React.Dispatch<React.SetStateAction<BrandInput>>; autosave: AutosaveHandle<BrandInput> } {
    const [draft, setDraft] = React.useState<BrandInput>(() => toInput(stored));
    const value = React.useMemo(
        () => ({ ...draft, display_name: normalizeName(draft.display_name), website_url: draft.website_url.trim() }),
        [draft],
    );
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
    const storedKey = stored ? JSON.stringify(toInput(stored)) : "";
    React.useEffect(() => {
        if (!stored) return;
        const next = toInput(stored);
        autosave.markSaved(next);
        if (!autosave.dirty) setDraft(next);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [storedKey]);
    return { draft, setDraft, autosave };
}
