import React from "react";
import { Navigate, Outlet, useNavigate } from "react-router-dom";
import { APP_URL, WEBSITE_URL } from "@/lib/information";
import getToken from "@/lib/helper/getToken";
import { Logo } from "@/components/svg";
import AuthShowcase from "./_components/AuthShowcase";

/* ═══════════════════════════════════════════
   Auth layout — one box, two panes.

   Left  : the airy-sky showcase. The Warmbly logo is pinned to it as a
           persistent element, so something stays put while the feature
           cards slide. Hidden on small screens.
   Right : a clean form column with a minimal footer at the bottom.
   ═══════════════════════════════════════════ */

export default function AuthLayout({
    redirectIfAuthenticated = true,
}: { redirectIfAuthenticated?: boolean } = {}) {
    const navigate = useNavigate();

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
        <div className="flex min-h-dvh w-full items-center justify-center bg-slate-50 px-5 py-10 text-slate-900">
            <div className="animate-card-float grid w-full max-w-[400px] overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04),0_30px_70px_-32px_rgba(15,23,42,0.32)] lg:max-w-[900px] lg:grid-cols-2 lg:min-h-[580px]">
                {/* Left: sky showcase with a persistent logo */}
                <div className="relative hidden lg:block">
                    <AuthShowcase />
                    <a href={WEBSITE_URL} className="absolute left-9 top-8 z-20 flex items-center gap-2.5">
                        <Logo className="w-7 text-white" />
                        <span className="font-extrabold text-[18px] tracking-tight text-white">Warmbly</span>
                    </a>
                </div>

                {/* Right: form column + minimal footer */}
                <div className="flex flex-col px-6 py-9 sm:px-10">
                    <div className="mx-auto flex w-full max-w-[360px] flex-1 flex-col">
                        {/* Logo for the mobile layout (no sky pane there) */}
                        <a href={WEBSITE_URL} className="mb-8 flex w-fit items-center gap-2.5 lg:hidden">
                            <Logo className="w-7 text-slate-900" />
                            <span className="font-extrabold text-[18px] tracking-tight text-slate-900">Warmbly</span>
                        </a>

                        <div className="flex flex-1 items-center">
                            <div className="w-full">
                                <Outlet />
                            </div>
                        </div>

                        <div className="flex items-center gap-3 pt-8 text-[12px] text-slate-400">
                            <a href={`${WEBSITE_URL}/terms`} target="_blank" rel="noopener noreferrer" className="hover:text-slate-700 transition-colors">Terms</a>
                            <a href={`${WEBSITE_URL}/privacy`} target="_blank" rel="noopener noreferrer" className="hover:text-slate-700 transition-colors">Privacy</a>
                            <span className="ml-auto">© {new Date().getFullYear()} Warmbly</span>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    );
}
