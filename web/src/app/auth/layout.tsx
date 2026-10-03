import React from "react";
import { Navigate, useLocation, useNavigate, useOutlet } from "react-router-dom";
import { AnimatePresence, motion } from "motion/react";
import { APP_URL } from "@/lib/information";
import useBrand from "@/hooks/useBrand";
import getToken from "@/lib/helper/getToken";

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
    const location = useLocation();
    const outlet = useOutlet();
    const brand = useBrand();

    React.useEffect(() => {
        const receiveMessage = (event: MessageEvent) => {
            if (event.origin !== APP_URL) return;
            if (event.data?.type === "auth") navigate("/app/emails");
        };
        window.addEventListener("message", receiveMessage);
        return () => window.removeEventListener("message", receiveMessage);
    }, [navigate]);

    if (redirectIfAuthenticated && getToken()) {
        return <Navigate to="/app/emails" replace />;
    }

    return (
        <div className="relative flex min-h-dvh w-full items-center justify-center bg-slate-50 px-4 py-12 text-slate-900 sm:px-6">
            <div className="relative z-10 w-full max-w-[420px]">
                {/* Card */}
                <div className="rounded-2xl border border-slate-200 bg-white p-6 sm:p-8 shadow-sm">
                    {/* Animate route changes */}
                    <AnimatePresence mode="wait" initial={false}>
                        <motion.div
                            key={location.pathname}
                            initial={{ opacity: 0, y: 6 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: -6 }}
                            transition={{ duration: 0.2 }}
                        >
                            {outlet}
                        </motion.div>
                    </AnimatePresence>
                </div>

                {/* Footer */}
                <div className="mt-6 flex items-center justify-center gap-3 text-xs text-slate-400">
                    {brand.terms_url && (
                        <a href={brand.terms_url} target="_blank" rel="noopener noreferrer" className="hover:text-slate-600 transition-colors">
                            Terms
                        </a>
                    )}
                    {brand.privacy_url && (
                        <a href={brand.privacy_url} target="_blank" rel="noopener noreferrer" className="hover:text-slate-600 transition-colors">
                            Privacy
                        </a>
                    )}
                </div>
            </div>
        </div>
    );
}
