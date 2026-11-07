import { useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { motion, AnimatePresence } from "motion/react";
import { useForm, Controller } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import toast from "react-hot-toast/headless";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";

import AuthButton from "@/components/auth/button";
import useCompleteOnboarding from "@/lib/api/hooks/auth/useCompleteOnboarding";
import useUpdateOrganization from "@/lib/api/hooks/app/organizations/useUpdateOrganization";
import useCurrentOrganization from "@/lib/api/hooks/app/organizations/useCurrentOrganization";
import type Organization from "@/lib/api/models/app/organizations/Organization";
import { browserTimezone } from "@/lib/timezone";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { postAuthNext } from "@/lib/helper/safeNext";
import { hrefTarget } from "@/lib/routerSearch";
import { PERSON_NAME_MAX, WORKSPACE_NAME_MAX, nameError, normalizeName, type NameKind } from "@/lib/displayName";

/* ── Schema ─────────────────────── */

const displayName = (label: string, kind: NameKind) =>
    z.string().superRefine((value, ctx) => {
        const message = nameError(label, value, kind);
        if (message) ctx.addIssue({ code: "custom", message });
    });

const schema = z.object({
    first_name: displayName("First name", "person"),
    last_name: displayName("Last name", "person"),
    workspace: displayName("Workspace name", "workspace"),
    role: z.enum(["founder", "sales", "marketing", "agency", "recruiter", "other"], {
        error: "Pick the closest one",
    }),
    team_size: z.enum(["just_me", "2-10", "11-50", "51-200", "200+"], {
        error: "Pick a team size",
    }),
    referral_source: z.enum(["reddit", "x", "facebook", "google", "other"], {
        error: "Let us know how you found us",
    }),
});

type OnboardingForm = z.infer<typeof schema>;

const INPUT = "w-full h-11 rounded-lg border border-slate-200 bg-white px-4 text-[15px] text-slate-900 placeholder:text-slate-400 outline-none transition-colors duration-200 focus:border-sky-400 focus:ring-4 focus:ring-sky-400/15";

const STEPS = [
    {
        fields: ["first_name", "last_name"] as const,
        title: "Welcome to Warmbly",
        subtitle: "Let's set up your account. First, your name.",
    },
    {
        fields: ["workspace"] as const,
        title: "Name your workspace",
        subtitle: "Where your team, mailboxes and campaigns live. You can rename it later.",
    },
    {
        fields: ["role", "team_size", "referral_source"] as const,
        title: "A few quick questions",
        subtitle: "This helps us tailor Warmbly to how you send.",
    },
];

const ROLES = [
    { value: "founder", label: "Founder" },
    { value: "sales", label: "Sales" },
    { value: "marketing", label: "Marketing" },
    { value: "agency", label: "Agency" },
    { value: "recruiter", label: "Recruiter" },
    { value: "other", label: "Other" },
] as const;

const TEAM_SIZES = [
    { value: "just_me", label: "Just me" },
    { value: "2-10", label: "2-10" },
    { value: "11-50", label: "11-50" },
    { value: "51-200", label: "51-200" },
    { value: "200+", label: "200+" },
] as const;

const REFERRALS = [
    { value: "reddit", label: "Reddit" },
    { value: "x", label: "X" },
    { value: "facebook", label: "Facebook" },
    { value: "google", label: "Google" },
    { value: "other", label: "Other" },
] as const;

/* ── Bits ─────────────────────── */

function FieldError({ message }: { message?: string }) {
    return (
        <AnimatePresence>
            {message && (
                <motion.p
                    initial={{ opacity: 0, y: -4 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, y: -4 }}
                    transition={{ duration: 0.2 }}
                    className="text-xs text-rose-500 mt-1 pl-0.5"
                >
                    {message}
                </motion.p>
            )}
        </AnimatePresence>
    );
}

function Pills({
    options,
    value,
    onChange,
}: {
    options: ReadonlyArray<{ value: string; label: string }>;
    value?: string;
    onChange: (v: string) => void;
}) {
    return (
        <div className="flex flex-wrap gap-2">
            {options.map((o) => {
                const selected = value === o.value;
                return (
                    <button
                        key={o.value}
                        type="button"
                        onClick={() => onChange(o.value)}
                        className={`px-3.5 py-2 rounded-lg border text-sm font-medium transition-all duration-200 cursor-pointer ${
                            selected
                                ? "border-sky-400 bg-sky-50 text-sky-700 ring-2 ring-sky-400/20"
                                : "border-slate-200 bg-white text-slate-500 hover:border-slate-300 hover:text-slate-700"
                        }`}
                    >
                        {o.label}
                    </button>
                );
            })}
        </div>
    );
}

const FieldLabel = ({ children }: { children: React.ReactNode }) => (
    <label className="block text-sm font-medium text-slate-600 mb-1.5 pl-0.5">{children}</label>
);

/* ═══════════════════════════════════════════
   Onboarding — a 3-step wizard
   ═══════════════════════════════════════════ */

export default function OnboardingPage() {
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const completeOnboarding = useCompleteOnboarding();
    const updateOrganization = useUpdateOrganization();
    const { data: org, refetch: refetchOrg } = useCurrentOrganization();


    const [step, setStep] = useState(0);
    const isLast = step === STEPS.length - 1;

    const { register, control, trigger, getValues, setValue, formState: { errors } } = useForm<OnboardingForm>({
        resolver: zodResolver(schema),
        mode: "onTouched",
    });

    // Prefill the workspace name with the org auto-created at signup.
    useEffect(() => {
        if (org?.name && !getValues("workspace")) {
            setValue("workspace", org.name);
        }
    }, [org?.name, getValues, setValue]);

    const pending = completeOnboarding.isPending || updateOrganization.isPending;

    const finish = async () => {
        const data = getValues();
        try {
            // Rename the auto-created workspace if the user changed it, and give
            // it this browser's timezone when it has none, so campaigns and
            // warmup windows start in the user's own day. Best effort: a hiccup
            // here must never block completing onboarding.
            const workspace = normalizeName(data.workspace);
            // A submit that beats the organization query still has to know what to patch.
            const current = org ?? (await refetchOrg().catch(() => undefined))?.data;
            const patch: Partial<Organization> = {};
            if (current?.name && current.name !== workspace) patch.name = workspace;
            const zone = browserTimezone();
            if (current && !current.timezone && zone) patch.timezone = zone;
            if (Object.keys(patch).length > 0) {
                try {
                    await updateOrganization.mutateAsync(patch);
                } catch {
                    /* keep going — onboarding completion matters more */
                }
            }
            await completeOnboarding.mutateAsync({
                first_name: normalizeName(data.first_name),
                last_name: normalizeName(data.last_name),
                referral_source: data.referral_source,
                role: data.role,
                team_size: data.team_size,
            });
            queryClient.removeQueries({ queryKey: ["auth", "me"] });
            navigate(hrefTarget(postAuthNext(new URLSearchParams(window.location.search).get("next"))));
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    const onSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (pending) return;
        const ok = await trigger(STEPS[step].fields as unknown as (keyof OnboardingForm)[]);
        if (!ok) return;
        if (isLast) await finish();
        else setStep((s) => s + 1);
    };

    return (
        <div className="space-y-6">
            {/* Progress */}
            <div className="flex items-center gap-3">
                {step > 0 ? (
                    <button
                        type="button"
                        onClick={() => setStep((s) => s - 1)}
                        className="flex items-center justify-center w-7 h-7 -ml-1 rounded-lg text-slate-400 hover:text-slate-700 hover:bg-slate-100 transition-colors cursor-pointer"
                        aria-label="Back"
                    >
                        <ArrowLeft className="w-4 h-4" />
                    </button>
                ) : (
                    <span className="w-7 h-7 -ml-1" />
                )}
                <div className="flex-1 flex gap-1.5">
                    {STEPS.map((_: unknown, i: number) => (
                        <span key={i} className="h-1 flex-1 rounded-full bg-slate-200 overflow-hidden">
                            <motion.span
                                className="block h-full rounded-full bg-sky-500"
                                initial={false}
                                animate={{ width: i <= step ? "100%" : "0%" }}
                                transition={{ duration: 0.35, ease: "easeOut" }}
                            />
                        </span>
                    ))}
                </div>
                <span className="text-xs font-medium text-slate-400 tabular-nums">{step + 1}/{STEPS.length}</span>
            </div>

            <form onSubmit={onSubmit} className="space-y-6">
                <AnimatePresence mode="wait" initial={false}>
                    <motion.div
                        key={step}
                        initial={{ opacity: 0, x: 16 }}
                        animate={{ opacity: 1, x: 0 }}
                        exit={{ opacity: 0, x: -16 }}
                        transition={{ duration: 0.25, ease: [0.16, 1, 0.3, 1] }}
                        className="space-y-5"
                    >
                        <div className="text-center">
                            <h1 className="text-[26px] md:text-[28px] font-bold text-slate-900 tracking-tight leading-tight">
                                {STEPS[step].title}
                            </h1>
                            <p className="text-sm text-slate-400 mt-1.5">{STEPS[step].subtitle}</p>
                        </div>

                        {step === 0 && (
                            <div className="space-y-4">
                                <div>
                                    <FieldLabel>First name</FieldLabel>
                                    <input type="text" placeholder="John" className={INPUT} maxLength={PERSON_NAME_MAX} autoComplete="given-name" autoFocus {...register("first_name")} />
                                    <FieldError message={errors.first_name?.message} />
                                </div>
                                <div>
                                    <FieldLabel>Last name</FieldLabel>
                                    <input type="text" placeholder="Doe" className={INPUT} maxLength={PERSON_NAME_MAX} autoComplete="family-name" {...register("last_name")} />
                                    <FieldError message={errors.last_name?.message} />
                                </div>
                            </div>
                        )}

                        {step === 1 && (
                            <div>
                                <FieldLabel>Workspace name</FieldLabel>
                                <input type="text" placeholder="Acme Inc." className={INPUT} maxLength={WORKSPACE_NAME_MAX} autoFocus {...register("workspace")} />
                                <FieldError message={errors.workspace?.message} />
                            </div>
                        )}

                        {step === 2 && (
                            <div className="space-y-5">
                                <Controller
                                    control={control}
                                    name="role"
                                    render={({ field }) => (
                                        <div>
                                            <FieldLabel>What best describes you?</FieldLabel>
                                            <Pills options={ROLES} value={field.value} onChange={field.onChange} />
                                            <FieldError message={errors.role?.message} />
                                        </div>
                                    )}
                                />
                                <Controller
                                    control={control}
                                    name="team_size"
                                    render={({ field }) => (
                                        <div>
                                            <FieldLabel>How big is your team?</FieldLabel>
                                            <Pills options={TEAM_SIZES} value={field.value} onChange={field.onChange} />
                                            <FieldError message={errors.team_size?.message} />
                                        </div>
                                    )}
                                />
                                <Controller
                                    control={control}
                                    name="referral_source"
                                    render={({ field }) => (
                                        <div>
                                            <FieldLabel>How did you find us?</FieldLabel>
                                            <Pills options={REFERRALS} value={field.value} onChange={field.onChange} />
                                            <FieldError message={errors.referral_source?.message} />
                                        </div>
                                    )}
                                />
                            </div>
                        )}

                    </motion.div>
                </AnimatePresence>

                <AuthButton loading={isLast && pending}>{isLast ? "Get started" : "Continue"}</AuthButton>

            </form>
        </div>
    );
}
