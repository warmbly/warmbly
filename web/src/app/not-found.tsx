import { Link } from "react-router-dom";
import { motion } from "framer-motion";
import { MailX, LayoutDashboard, LogIn } from "lucide-react";
import { Logo } from "@/components/svg";

export default function NotFound() {
  return (
    <div className="min-h-screen flex items-center justify-center bg-white px-6">
      <motion.div
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.2, ease: "easeOut" }}
        className="w-full max-w-[440px] flex flex-col items-center text-center"
      >
        {/* Brand wordmark */}
        <div className="flex items-center gap-2">
          <Logo className="w-7 text-slate-900" />
          <span
            className="text-[15px] font-medium text-slate-900"
            style={{ fontFamily: "var(--font-display)" }}
          >
            Warmbly
          </span>
        </div>

        {/* Glyph: MailX in a slate ring + one sky stamp dot */}
        <div className="relative mt-10 mb-6">
          <div className="flex size-12 items-center justify-center rounded-xl border border-slate-200 bg-slate-50/80">
            <MailX className="w-5 h-5 text-slate-400" strokeWidth={1.6} />
          </div>
          <span className="absolute -top-1 -right-1 size-2.5 rounded-full bg-sky-500 ring-2 ring-white" />
        </div>

        {/* Eyebrow */}
        <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
          Undeliverable · 404
        </span>

        {/* Headline */}
        <h1
          className="mt-2 text-[28px] leading-[1.15] font-light text-slate-900"
          style={{ fontFamily: "var(--font-display)" }}
        >
          This page bounced
        </h1>

        {/* Subhead */}
        <p className="mt-2 max-w-[42ch] text-[12.5px] leading-relaxed text-slate-400">
          We couldn&apos;t find a page at that address. The link may be broken, or the page may have
          moved. Head back to a safe place below.
        </p>

        {/* Actions */}
        <div className="mt-6 flex flex-wrap items-center justify-center gap-1.5">
          <Link
            to="/app"
            className="h-8 px-3 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium bg-sky-600 hover:bg-sky-700 text-white transition-colors focus:outline-none focus:ring-2 focus:ring-sky-100"
          >
            <LayoutDashboard className="w-3.5 h-3.5" strokeWidth={1.6} /> Back to dashboard
          </Link>
          <Link
            to="/auth/login"
            className="h-8 px-3 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium border border-slate-200 hover:border-slate-300 bg-white text-slate-700 hover:text-slate-900 transition-colors focus:outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
          >
            <LogIn className="w-3.5 h-3.5" strokeWidth={1.6} /> Sign in
          </Link>
        </div>
      </motion.div>
    </div>
  );
}
