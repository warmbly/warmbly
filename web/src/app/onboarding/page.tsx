import { useNavigate } from "react-router-dom";
import { motion, AnimatePresence } from "motion/react";
import { useForm, Controller } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import toast from "react-hot-toast";
import { useQueryClient } from "@tanstack/react-query";

import AuthButton from "@/components/auth/button";
import useCompleteOnboarding from "@/lib/api/hooks/auth/useCompleteOnboarding";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

/* ── Schema ─────────────────────── */

const onboardingSchema = z.object({
    first_name: z.string().min(1, "First name is required").max(50, "50 characters max"),
    last_name: z.string().min(1, "Last name is required").max(50, "50 characters max"),
    referral_source: z.enum(["reddit", "x", "facebook", "google", "other"], {
        required_error: "Please select how you found us",
    }),
});

type OnboardingForm = z.infer<typeof onboardingSchema>;

/* ── Shared input class ─────────────────────── */

const INPUT = "w-full h-11 rounded-lg border border-sky-200 bg-white px-4 text-[15px] text-slate-800 placeholder:text-slate-300 outline-none transition-colors duration-200 focus:border-sky-400 focus:ring-4 focus:ring-sky-400/15";

/* ── Error fade ─────────────────────── */

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

/* ── Referral sources ─────────────────────── */

const referralSources = [
    { value: "reddit", label: "Reddit" },
    { value: "x", label: "X" },
    { value: "facebook", label: "Facebook" },
    { value: "google", label: "Google" },
    { value: "other", label: "Other" },
] as const;

/* ═══════════════════════════════════════════
   Onboarding Page
   ═══════════════════════════════════════════ */

export default function OnboardingPage() {
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const mutation = useCompleteOnboarding();

    const { register, handleSubmit, control, formState: { errors } } = useForm<OnboardingForm>({
        resolver: zodResolver(onboardingSchema),
    });

    const onSubmit = async (data: OnboardingForm) => {
        try {
            await mutation.mutateAsync(data);
            queryClient.removeQueries({ queryKey: ["auth", "me"] });
            navigate("/app/emails");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    return (
        <div className="animate-card-float">
            <div className="bg-white/95 backdrop-blur-sm rounded-2xl border border-sky-200/40 shadow-[0_8px_40px_-12px_rgba(56,189,248,0.15),0_4px_25px_-5px_rgba(0,0,0,0.07)] p-7 sm:p-8 md:p-10">
                <div className="space-y-6">

                    {/* ── Header with word-reveal ── */}
                    <div className="text-center overflow-hidden">
                        <h1 className="font-serif text-[32px] text-slate-800 tracking-tight leading-tight">
                            {["Almost", "there"].map((word, i) => (
                                <motion.span
                                    key={word}
                                    className="inline-block"
                                    initial={{ opacity: 0, y: 20, filter: "blur(4px)" }}
                                    animate={{ opacity: 1, y: 0, filter: "blur(0px)" }}
                                    transition={{ delay: i * 0.1, duration: 0.3 }}
                                >
                                    {word}{i === 0 ? "\u00A0" : ""}
                                </motion.span>
                            ))}
                        </h1>
                        <motion.p
                            className="text-sm text-slate-400 mt-1.5"
                            initial={{ opacity: 0 }}
                            animate={{ opacity: 1 }}
                            transition={{ delay: 0.15, duration: 0.25 }}
                        >
                            Tell us a bit about yourself to get started
                        </motion.p>
                    </div>

                    {/* ── Form ── */}
                    <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">

                        {/* First name */}
                        <div>
                            <label className="text-sm font-medium text-slate-600 pl-0.5">First name</label>
                            <input
                                type="text"
                                placeholder="John"
                                className={INPUT}
                                maxLength={50}
                                autoFocus
                                {...register("first_name")}
                            />
                            <FieldError message={errors.first_name?.message} />
                        </div>

                        {/* Last name */}
                        <div>
                            <label className="text-sm font-medium text-slate-600 pl-0.5">Last name</label>
                            <input
                                type="text"
                                placeholder="Doe"
                                className={INPUT}
                                maxLength={50}
                                {...register("last_name")}
                            />
                            <FieldError message={errors.last_name?.message} />
                        </div>

                        {/* Divider */}
                        <div className="flex items-center gap-3 pt-1">
                            <div className="flex-1 h-px bg-sky-100" />
                            <span className="text-xs text-slate-300 font-medium">how did you find us?</span>
                            <div className="flex-1 h-px bg-sky-100" />
                        </div>

                        {/* Referral source pills */}
                        <Controller
                            control={control}
                            name="referral_source"
                            render={({ field }) => (
                                <div>
                                    <div className="flex flex-wrap gap-2">
                                        {referralSources.map((source) => {
                                            const selected = field.value === source.value;
                                            return (
                                                <button
                                                    key={source.value}
                                                    type="button"
                                                    onClick={() => field.onChange(source.value)}
                                                    className={`relative px-4 py-2 rounded-lg border text-sm font-medium transition-all duration-200 cursor-pointer ${
                                                        selected
                                                            ? "border-sky-400 bg-sky-50 text-sky-700 ring-2 ring-sky-400/20"
                                                            : "border-sky-200 bg-white text-slate-500 hover:border-sky-300 hover:text-slate-600"
                                                    }`}
                                                >
                                                    {selected && (
                                                        <motion.span
                                                            layoutId="referral-pill"
                                                            className="absolute inset-0 rounded-lg bg-sky-50 border border-sky-400 ring-2 ring-sky-400/20"
                                                            transition={{ type: "spring", stiffness: 500, damping: 35 }}
                                                        />
                                                    )}
                                                    <span className="relative z-10">{source.label}</span>
                                                </button>
                                            );
                                        })}
                                    </div>
                                    <FieldError message={errors.referral_source?.message} />
                                </div>
                            )}
                        />

                        {/* Submit */}
                        <div className="pt-1">
                            <AuthButton loading={mutation.isPending}>Continue</AuthButton>
                        </div>
                    </form>
                </div>
            </div>
        </div>
    );
}
