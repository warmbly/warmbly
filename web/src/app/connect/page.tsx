import { DashboardImage } from "@/components/ui/dashboard-image";

// app.warmbly.com/connect: where older self-hosted instances still send people to approve a link. Warmbly Cloud links no new instances.

import { Link } from "@tanstack/react-router";
import { motion } from "framer-motion";
import { ArrowRightIcon, CloudIcon } from "lucide-react";
import BrandMark from "@/components/shared/BrandMark";

export default function ConnectPage() {
    return (
        <div className="relative min-h-dvh w-full overflow-hidden flex flex-col items-center justify-center px-4 py-8 sm:py-10">
            <div className="absolute inset-0" aria-hidden="true">
                <div className="sky-base" />
                <div className="sky-breathe" />
                <div className="sun-glow" />
                <DashboardImage src="/backdrops/cloud-3.webp" alt="" decoding="async" className="cloud-drift cloud-1 absolute select-none" style={{ top: "6%", left: "-10%", width: 360, opacity: 0.55, height: "auto" }} />
                <DashboardImage src="/backdrops/cloud-4.webp" alt="" decoding="async" className="cloud-drift cloud-2 absolute select-none" style={{ bottom: "8%", right: "-8%", width: 320, opacity: 0.5, height: "auto" }} />
            </div>

            <div className="relative z-10 w-full max-w-[520px]">
                <BrandMark className="mb-5 flex w-fit items-center gap-2.5 mx-auto" />

                <motion.div
                    initial={{ y: 14, opacity: 0 }}
                    animate={{ y: 0, opacity: 1 }}
                    transition={{ duration: 0.4, ease: [0.16, 1, 0.3, 1] }}
                    className="rounded-3xl border border-slate-200 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04),0_30px_70px_-32px_rgba(15,23,42,0.32)] px-6 py-8 sm:px-10 sm:py-10"
                >
                    <span className="size-11 rounded-lg bg-sky-600 text-white inline-flex items-center justify-center">
                        <CloudIcon className="w-5 h-5" />
                    </span>
                    <h1 className="mt-4 text-[22px] sm:text-[26px] font-semibold tracking-[-0.03em] leading-[1.1] text-slate-900">
                        Warmbly Cloud no longer links self-hosted instances
                    </h1>
                    <p className="mt-3 text-[13.5px] leading-relaxed text-slate-500">
                        Connections made earlier keep working, and Settings shows them under Linked instances. To warm mailboxes in the Warmbly pool,
                        connect them to a workspace here.
                    </p>
                    <div className="mt-6 flex flex-wrap items-center gap-2">
                        <Link to="/app/emails" className="h-10 px-4 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[13.5px] font-medium inline-flex items-center gap-1.5 transition-colors">
                            Go to mailboxes
                            <ArrowRightIcon className="w-4 h-4" />
                        </Link>
                    </div>
                </motion.div>
            </div>
        </div>
    );
}
