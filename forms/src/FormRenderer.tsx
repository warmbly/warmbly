// The live form: multi-page (or focus-mode) rendering with per-screen
// validation, field state via TanStack Form, submission to the same-origin
// /api. Client checks are a courtesy; the backend re-validates everything and
// its message wins the error box.

import { useEffect, useRef, useState } from "react";
import { useForm } from "@tanstack/react-form";

import type { FormField, PublicForm } from "./api";
import { personalToken, submitForm, StalePageError, SubmitRejectedError } from "./api";
import { track } from "./observability";
import type { ResolvedDesign } from "./design";
import { focusSteps, pageIndexOf, splitPages } from "./design";
import { postSubmitted, redirect } from "./embed";
import type { Tracker } from "./events";
import { visitorKey } from "./events";
import type { AnswerValue } from "./fields";
import { FieldControl } from "./fields";
import { Turnstile } from "./Turnstile";
import { resetTurnstile } from "./turnstileScript";

type Answers = Record<string, AnswerValue>;

const LAYOUT_TYPES = new Set(["heading", "paragraph", "divider", "hidden", "page_break"]);

function isInput(f: FormField): boolean {
    return !LAYOUT_TYPES.has(f.type);
}

function prefillValue(f: FormField, raw: string): AnswerValue | undefined {
    switch (f.type) {
        case "checkbox":
            return ["yes", "true", "1", "on"].includes(raw.toLowerCase()) ? true : undefined;
        case "checkboxes": {
            const opts = new Set(f.options ?? []);
            const picked = raw.split(", ").filter((v) => opts.has(v));
            return picked.length > 0 ? picked : undefined;
        }
        case "select":
        case "radio":
            return (f.options ?? []).includes(raw) ? raw : undefined;
        default:
            return raw;
    }
}

function defaultsFor(fields: FormField[], prefill?: Record<string, string>): Answers {
    const out: Answers = {};
    for (const f of fields) {
        if (!isInput(f)) continue;
        out[f.id] = f.type === "checkboxes" ? [] : f.type === "checkbox" ? false : "";
        const raw = prefill?.[f.id];
        if (raw) {
            const v = prefillValue(f, raw);
            if (v !== undefined) out[f.id] = v;
        }
    }
    return out;
}

function validateField(f: FormField, v: AnswerValue): string | undefined {
    const empty =
        f.type === "checkboxes" ? !Array.isArray(v) || v.length === 0 : f.type === "checkbox" ? v !== true : typeof v !== "string" || v.trim() === "";
    if (f.required && empty) return `${f.label || "This field"} is required.`;
    if (f.type === "email" && typeof v === "string" && v.trim() !== "" && !/^\S+@\S+\.\S+$/.test(v.trim())) {
        return "Enter a valid email address.";
    }
    return undefined;
}

function buildAnswers(fields: FormField[], value: Answers): Record<string, string[]> {
    const out: Record<string, string[]> = {};
    for (const f of fields) {
        if (f.type === "hidden") {
            if (f.value) out[f.id] = [f.value];
            continue;
        }
        if (!isInput(f)) continue;
        const v = value[f.id];
        if (f.type === "checkboxes") {
            if (Array.isArray(v) && v.length > 0) out[f.id] = v;
        } else if (f.type === "checkbox") {
            if (v === true) out[f.id] = ["yes"];
        } else if (typeof v === "string" && v.trim() !== "") {
            out[f.id] = [v];
        }
    }
    return out;
}

function SuccessBlock({ message }: { message: string }) {
    return (
        <div className="ok">
            <svg
                width="40"
                height="40"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
            >
                <circle cx="12" cy="12" r="10"></circle>
                <path d="m9 12 2 2 4-4"></path>
            </svg>
            <p>{message}</p>
        </div>
    );
}

interface Screen {
    fields: FormField[];
    title: string;
    /** The classic page this screen belongs to, for funnel reporting. */
    pageIndex: number;
}

// buildScreens flattens the two modes into one shape: classic mode shows a
// page of fields per screen, focus mode one question per screen.
function buildScreens(fields: FormField[], mode: string): Screen[] {
    const pageOf = pageIndexOf(fields);
    if (mode === "focus") {
        return focusSteps(fields).map((step) => {
            const anchor = step[step.length - 1];
            return { fields: step, title: "", pageIndex: pageOf[anchor.id] ?? 0 };
        });
    }
    return splitPages(fields).map((p, i) => ({ fields: p.fields, title: p.title, pageIndex: i }));
}

export function FormRenderer({
    form: def,
    design,
    tracker,
}: {
    form: PublicForm;
    design: ResolvedDesign;
    tracker: Tracker;
}) {
    // Bots submit near-instantly after render; the backend discards those.
    const renderedAt = useRef(Math.floor(Date.now() / 1000));
    const honeypot = useRef<HTMLInputElement>(null);
    const stepRef = useRef<HTMLDivElement>(null);
    const [captchaToken, setCaptchaToken] = useState("");
    const [serverError, setServerError] = useState("");
    const [done, setDone] = useState<string | null>(null);
    const [screen, setScreen] = useState(0);
    const [dir, setDir] = useState<"fwd" | "back">("fwd");
    const [screenErrors, setScreenErrors] = useState<Record<string, string>>({});

    const screens = buildScreens(def.fields, design.mode);
    const current = screens[Math.min(screen, screens.length - 1)];
    const isLast = screen >= screens.length - 1;

    const form = useForm({
        defaultValues: defaultsFor(def.fields, def.prefill),
        onSubmit: async ({ value }) => {
            setServerError("");
            try {
                const res = await submitForm(def.public_id, {
                    answers: buildAnswers(def.fields, value),
                    website: honeypot.current?.value ?? "",
                    _wt: renderedAt.current,
                    captcha_token: captchaToken || undefined,
                    source_url: document.referrer || window.location.href,
                    link_token: personalToken() ?? undefined,
                    visitor_key: visitorKey(),
                });
                postSubmitted(def.public_id);
                track("form_submitted", def.public_id);
                if (res.redirect_url) {
                    redirect(res.redirect_url);
                    return;
                }
                setDone(res.message || "Thanks!");
            } catch (e) {
                if (e instanceof StalePageError) {
                    setServerError("This page has been open for a while. Refresh it and try again.");
                } else {
                    setServerError(e instanceof SubmitRejectedError ? e.message : "Something went wrong. Try again.");
                }
                resetTurnstile();
                setCaptchaToken("");
            }
        },
    });

    // Focus mode puts the visitor straight into the question.
    useEffect(() => {
        if (design.mode !== "focus") return;
        const el = stepRef.current?.querySelector<HTMLElement>("input:not([type=hidden]), select, textarea");
        el?.focus();
    }, [screen, design.mode]);

    if (done !== null) return <SuccessBlock message={done} />;

    // validateScreen is the only client validation: every screen is checked on
    // the way forward, so the final submit only ever re-checks the last one.
    const validateScreen = (s: Screen): boolean => {
        const errs: Record<string, string> = {};
        for (const f of s.fields) {
            if (!isInput(f)) continue;
            const msg = validateField(f, form.state.values[f.id]);
            if (msg) errs[f.id] = msg;
        }
        setScreenErrors(errs);
        return Object.keys(errs).length === 0;
    };

    const goNext = () => {
        if (!validateScreen(current) || isLast) return;
        setDir("fwd");
        const next = screen + 1;
        setScreen(next);
        tracker.page(screens[next].pageIndex);
        stepRef.current?.closest(".wf-main")?.scrollTo?.(0, 0);
        window.scrollTo(0, 0);
    };

    const goBack = () => {
        if (screen === 0) return;
        setScreenErrors({});
        setDir("back");
        setScreen(screen - 1);
    };

    const submitCurrent = () => {
        if (!validateScreen(current)) return;
        void form.handleSubmit();
    };

    const showProgress = design.showProgress && screens.length > 1;
    const focusMode = design.mode === "focus";

    return (
        <form
            noValidate
            onFocusCapture={() => tracker.start()}
            onChangeCapture={() => tracker.start()}
            onKeyDown={(e) => {
                if (!focusMode || e.key !== "Enter" || e.shiftKey) return;
                const target = e.target as HTMLElement;
                if (target instanceof HTMLTextAreaElement) return;
                e.preventDefault();
                if (isLast) submitCurrent();
                else goNext();
            }}
            onSubmit={(e) => {
                e.preventDefault();
                e.stopPropagation();
                submitCurrent();
            }}
        >
            <div className="hpwrap" aria-hidden="true">
                <input ref={honeypot} type="text" name="website" tabIndex={-1} autoComplete="off" />
            </div>
            {showProgress && (
                <div className="wf-progress" role="presentation">
                    <span style={{ width: `${((screen + 1) / screens.length) * 100}%` }} />
                </div>
            )}
            <div key={screen} ref={stepRef} className="wf-step" data-dir={dir}>
                <div className="grid">
                    {serverError && <div className="err">{serverError}</div>}
                    {!focusMode && current.title && <p className="wf-pagetitle">{current.title}</p>}
                    {current.fields.map((f) => {
                        switch (f.type) {
                            case "heading":
                                return (
                                    <div key={f.id} className="fld">
                                        <h2 className="h">{f.label}</h2>
                                    </div>
                                );
                            case "paragraph":
                                return (
                                    <div key={f.id} className="fld">
                                        <p className="p">{f.value}</p>
                                    </div>
                                );
                            case "divider":
                                return <hr key={f.id} className="d" />;
                            case "hidden":
                            case "page_break":
                                return null;
                            default:
                                return (
                                    <form.Field key={f.id} name={f.id}>
                                        {(field) => (
                                            <div className={f.width === "half" ? "fld half" : "fld"}>
                                                {f.type !== "checkbox" && (
                                                    <label className="l" htmlFor={`f-${f.id}`}>
                                                        {f.label}
                                                        {f.required && <span className="req"> *</span>}
                                                    </label>
                                                )}
                                                <FieldControl
                                                    field={f}
                                                    value={field.state.value}
                                                    onChange={(v) => {
                                                        field.handleChange(v);
                                                        if (screenErrors[f.id]) {
                                                            setScreenErrors((prev) => {
                                                                const rest = { ...prev };
                                                                delete rest[f.id];
                                                                return rest;
                                                            });
                                                        }
                                                    }}
                                                    onBlur={field.handleBlur}
                                                />
                                                {screenErrors[f.id] && <p className="fielderr">{screenErrors[f.id]}</p>}
                                                {f.help_text && <p className="help">{f.help_text}</p>}
                                            </div>
                                        )}
                                    </form.Field>
                                );
                        }
                    })}
                    {isLast && def.captcha_site_key && (
                        <Turnstile siteKey={def.captcha_site_key} onToken={setCaptchaToken} />
                    )}
                    {isLast ? (
                        <div className={screen > 0 ? "wf-pagenav" : "btnrow"}>
                            {screen > 0 && (
                                <button type="button" className="wf-back" onClick={goBack}>
                                    Back
                                </button>
                            )}
                            <form.Subscribe selector={(s) => s.isSubmitting}>
                                {(isSubmitting) => (
                                    <button
                                        className={design.btnFullWidth ? "submit full" : "submit"}
                                        type="submit"
                                        disabled={isSubmitting}
                                    >
                                        {design.btnLabel}
                                    </button>
                                )}
                            </form.Subscribe>
                        </div>
                    ) : (
                        <div className="wf-pagenav">
                            {screen > 0 ? (
                                <button type="button" className="wf-back" onClick={goBack}>
                                    Back
                                </button>
                            ) : (
                                <span />
                            )}
                            <button type="button" className="submit" onClick={goNext}>
                                Next
                            </button>
                        </div>
                    )}
                    {focusMode && <p className="wf-hint">Press Enter to continue</p>}
                </div>
            </div>
        </form>
    );
}
