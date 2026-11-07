import React from "react";
import { Navigate, Outlet, useMatches, useNavigate } from "@tanstack/react-router";
import { AnimatePresence, motion } from "motion/react";
import { APP_URL } from "@/lib/information";
import useBrand from "@/hooks/useBrand";
import BrandMark from "@/components/shared/BrandMark";
import getToken from "@/lib/helper/getToken";
import AuthShowcase from "./_components/AuthShowcase";

/* ═══════════════════════════════════════════
   Auth layout.

   Desktop : two panes — the airy-sky showcase (left, logo pinned in white)
             and the form (right) on a light page.
   Mobile  : the whole page becomes the airy sky, with a clean white form
             card floating on it (logo in white above, footer below). Same
             "air" feel as desktop, no cramped showcase banner.
   ═══════════════════════════════════════════ */

export default function AuthLayout({
    redirectIfAuthenticated = true,
}: { redirectIfAuthenticated?: boolean } = {}) {
    const navigate = useNavigate();
    // The rendered page's path, which changes with the Outlet rather than ahead of it.
    const pathname = useMatches({ select: (m) => m[m.length - 1]?.pathname ?? "" });
    const brand = useBrand();

    React.useEffect(() => {
        const receiveMessage = (event: MessageEvent) => {
            if (event.origin !== APP_URL) return;
            if (event.data?.type === "auth") navigate({ to: "/app/emails" });
        };
        window.addEventListener("message", receiveMessage);
        return () => window.removeEventListener("message", receiveMessage);
    }, [navigate]);

    // A reset link is opened from mail, often in the browser that is signed in.
    const opensSignedIn = pathname.startsWith("/auth/reset-password");
    if (redirectIfAuthenticated && !opensSignedIn && getToken()) {
        return <Navigate to="/app/emails" replace />;
    }

    return (
        <div className="relative flex min-h-dvh w-full items-center justify-center px-4 py-8 text-slate-900 sm:px-5 sm:py-10 lg:bg-slate-50">
            {/* Mobile airy-sky backdrop (desktop gets the sky inside the card instead) */}
            <div className="absolute inset-0 overflow-hidden lg:hidden" aria-hidden="true">
                <div className="sky-base" />
                <div className="sky-breathe" />
                <div className="sun-glow" />
                <img src="/backdrops/cloud-3.webp" alt="" aria-hidden="true" decoding="async" className="cloud-drift cloud-1 absolute select-none" style={{ top: "4%", left: "-16%", width: 280, opacity: 0.6, height: "auto" }} />
                <img src="/backdrops/cloud-4.webp" alt="" aria-hidden="true" decoding="async" className="cloud-drift cloud-2 absolute select-none" style={{ bottom: "12%", right: "-14%", width: 240, opacity: 0.5, height: "auto" }} />
            </div>

            <div className="relative z-10 w-full max-w-[400px] lg:max-w-[900px]">
                {/* Mobile logo — white, on the sky, above the card */}
                <BrandMark className="mb-5 flex w-fit items-center gap-2.5 mx-auto lg:hidden" />

                {/* Card */}
                <div className="animate-card-float grid overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04),0_30px_70px_-32px_rgba(15,23,42,0.32)] lg:grid-cols-2 lg:min-h-[580px]">
                    {/* Showcase — desktop only */}
                    <div className="relative hidden lg:block lg:border-r lg:border-slate-200">
                        <AuthShowcase />
                        <BrandMark className="absolute left-8 top-8 z-20 flex items-center gap-2.5" />
                    </div>

                    {/* Form column */}
                    <div className="flex flex-col px-6 py-9 sm:px-10">
                        <div className="mx-auto flex w-full max-w-[360px] flex-1 flex-col">
                            <div className="flex flex-1 items-center">
                                <div className="w-full">
                                    {/* Animate full route changes too (login → forgot password).
                                        Enter only: an exiting <Outlet /> would already render the next page. */}
                                    <AnimatePresence initial={false}>
                                        <motion.div
                                            key={pathname}
                                            initial={{ opacity: 0, x: 12 }}
                                            animate={{ opacity: 1, x: 0 }}
                                            transition={{ duration: 0.28, ease: [0.16, 1, 0.3, 1] }}
                                        >
                                            <Outlet />
                                        </motion.div>
                                    </AnimatePresence>
                                </div>
                            </div>

                            {/* Footer — desktop, inside the card */}
                            <div className="hidden items-center gap-3 pt-9 text-[12px] text-slate-400 lg:flex">
                                {brand.terms_url && <a href={brand.terms_url} target="_blank" rel="noopener noreferrer" className="hover:text-slate-700 transition-colors">Terms</a>}
                                {brand.privacy_url && <a href={brand.privacy_url} target="_blank" rel="noopener noreferrer" className="hover:text-slate-700 transition-colors">Privacy</a>}
                                <span className="ml-auto">© {new Date().getFullYear()} {brand.name}</span>
                            </div>
                        </div>
                    </div>
                </div>

                {/* Footer — mobile, on the sky below the card */}
                <div className="theme-solid mt-5 flex items-center justify-center gap-3 text-[12px] text-white/70 lg:hidden">
                    {brand.terms_url && <>
                        <a href={brand.terms_url} target="_blank" rel="noopener noreferrer" className="hover:text-white transition-colors">Terms</a>
                        <span className="text-white/40">·</span>
                    </>}
                    {brand.privacy_url && <>
                        <a href={brand.privacy_url} target="_blank" rel="noopener noreferrer" className="hover:text-white transition-colors">Privacy</a>
                        <span className="text-white/40">·</span>
                    </>}
                    <span>© {new Date().getFullYear()} {brand.name}</span>
                </div>
            </div>
        </div>
    );
}
