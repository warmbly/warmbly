import React from "react";
import { Navigate, Outlet, useNavigate } from "react-router-dom";
import { APP_URL, WEBSITE_URL } from "@/lib/information";
import getToken from "@/lib/helper/getToken";
import { Logo } from "@/components/svg";
import { Github } from "lucide-react";
import AuthShowcase from "./_components/AuthShowcase";

/* ═══════════════════════════════════════════
   Auth layout — one self-contained card, two panes.

   Left  : an auto-rotating product showcase on an airy sky (decorative,
           hidden on small screens).
   Right : a clean column — logo on top, the auth form in the middle,
           legal + extra links on the bottom. Everything lives inside the
           card so nothing floats awkwardly around it.
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
            <div
                className="animate-card-float grid w-full max-w-[400px] overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04),0_30px_70px_-32px_rgba(15,23,42,0.32)] lg:max-w-[900px] lg:grid-cols-2 lg:min-h-[600px]"
            >
                {/* Left: rotating showcase */}
                <div className="hidden lg:block">
                    <AuthShowcase />
                </div>

                {/* Right: logo · form · footer */}
                <div className="flex flex-col">
                    <header className="flex justify-center px-6 pt-8 pb-2">
                        <a href={WEBSITE_URL} className="flex items-center gap-2.5">
                            <Logo className="w-7 text-slate-900" />
                            <span className="font-extrabold text-[18px] tracking-tight text-slate-900">Warmbly</span>
                        </a>
                    </header>

                    <main className="flex flex-1 items-center justify-center px-6 py-4 sm:px-10">
                        <div className="w-full max-w-[360px]">
                            <Outlet />
                        </div>
                    </main>

                    <footer className="flex flex-wrap items-center justify-center gap-x-3 gap-y-1 px-6 pb-8 pt-3 text-[12px] text-slate-400">
                        <span>© {new Date().getFullYear()} Warmbly</span>
                        <span className="text-slate-300">·</span>
                        <a href={`${WEBSITE_URL}/terms`} target="_blank" rel="noopener noreferrer" className="hover:text-slate-700 transition-colors">Terms</a>
                        <a href={`${WEBSITE_URL}/privacy`} target="_blank" rel="noopener noreferrer" className="hover:text-slate-700 transition-colors">Privacy</a>
                        <span className="text-slate-300">·</span>
                        <a href="https://github.com/warmbly/warmbly" target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 hover:text-slate-700 transition-colors">
                            <Github className="w-3.5 h-3.5" /> GitHub
                        </a>
                    </footer>
                </div>
            </div>
        </div>
    );
}
