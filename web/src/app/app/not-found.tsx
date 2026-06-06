import { Link, useNavigate, useLocation } from "react-router-dom";
import { motion } from "framer-motion";
import {
  MailX,
  ArrowLeft,
  LayoutDashboard,
  Search,
  ArrowRight,
  MailIcon,
  MegaphoneIcon,
  UsersIcon,
  BarChart3Icon,
} from "lucide-react";
import { useAppStore } from "@/stores";
import { cn } from "@/lib/utils";

const dests = [
  { title: "Accounts", url: "/app/emails", icon: MailIcon, hint: "mailboxes & senders" },
  { title: "Campaigns", url: "/app/campaigns", icon: MegaphoneIcon, hint: "sequences & sends" },
  { title: "Contacts", url: "/app/contacts", icon: UsersIcon, hint: "people & lists" },
  { title: "Analytics", url: "/app/analytics", icon: BarChart3Icon, hint: "opens, clicks, replies" },
];

export default function DashboardNotFound() {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const setCommandPaletteOpen = useAppStore((s) => s.setCommandPaletteOpen);

  const goBack = () => {
    if (window.history.state?.idx > 0) navigate(-1);
    else navigate("/app");
  };

  return (
    <div className="min-h-full flex items-center justify-center bg-white px-6 py-16">
      <motion.div
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.2, ease: "easeOut" }}
        className="w-full max-w-[460px] flex flex-col items-center text-center"
      >
        {/* Glyph: MailX in a slate ring + one sky stamp dot */}
        <div className="relative mb-6">
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
          moved. Nothing was lost — pick up where you left off below.
        </p>

        {/* Bounce-report chip */}
        <div className="mt-5 w-full min-w-0 rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2">
          <div className="flex items-baseline justify-center gap-2 min-w-0">
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium shrink-0">
              to
            </span>
            <code className="font-mono text-[11px] text-slate-700 truncate min-w-0">{pathname}</code>
          </div>
          <div className="mt-1 flex items-center justify-center gap-1.5 font-mono text-[10.5px] tabular-nums text-slate-400">
            <span className="text-red-500">404</span>
            <span>·</span>
            <span>no_such_route</span>
          </div>
        </div>

        {/* Actions */}
        <div className="mt-6 flex flex-wrap items-center justify-center gap-1.5">
          <button
            type="button"
            onClick={goBack}
            className="h-8 px-3 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium border border-slate-200 hover:border-slate-300 bg-white text-slate-700 hover:text-slate-900 transition-colors focus:outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
          >
            <ArrowLeft className="w-3.5 h-3.5" strokeWidth={1.6} /> Go back
          </button>
          <Link
            to="/app"
            className="h-8 px-3 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium bg-sky-600 hover:bg-sky-700 text-white transition-colors focus:outline-none focus:ring-2 focus:ring-sky-100"
          >
            <LayoutDashboard className="w-3.5 h-3.5" strokeWidth={1.6} /> Dashboard
          </Link>
          <button
            type="button"
            onClick={() => setCommandPaletteOpen(true)}
            className="h-8 px-3 rounded-md inline-flex items-center gap-2 text-[12px] font-medium bg-sky-50 text-sky-700 hover:bg-sky-100 transition-colors focus:outline-none focus:ring-2 focus:ring-sky-100"
          >
            <Search className="w-3.5 h-3.5" strokeWidth={1.6} /> Search
            <kbd className="hidden md:inline-flex h-4 items-center px-1 rounded border border-sky-200 bg-white/70 font-mono text-[10px] text-sky-600 ml-0.5">
              ⌘K
            </kbd>
          </button>
        </div>

        {/* Popular destinations */}
        <div className="mt-8 w-full border-t border-slate-200/60 pt-4 text-left">
          <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
            Popular destinations
          </span>
          <div className="mt-2 -mx-2">
            {dests.map((d, i) => (
              <Link
                key={d.url}
                to={d.url}
                className={cn(
                  "group h-11 px-2 flex items-center gap-3 transition-colors hover:bg-slate-50/80 rounded-md",
                  i !== dests.length - 1 && "border-b border-slate-200/60 rounded-none",
                )}
              >
                <span className="size-7 shrink-0 inline-flex items-center justify-center rounded-md border border-slate-200 bg-white text-slate-500 group-hover:text-sky-600 group-hover:border-sky-200 transition-colors">
                  <d.icon className="w-4 h-4" strokeWidth={1.6} />
                </span>
                <span className="text-[12.5px] font-medium text-slate-700 group-hover:text-slate-900 shrink-0">
                  {d.title}
                </span>
                <span className="text-[11.5px] text-slate-400 truncate min-w-0">{d.hint}</span>
                <ArrowRight
                  className="w-3.5 h-3.5 ml-auto shrink-0 text-slate-300 opacity-100 md:opacity-0 md:group-hover:opacity-100 group-hover:text-slate-500 transition-all"
                  strokeWidth={1.6}
                />
              </Link>
            ))}
          </div>
        </div>
      </motion.div>
    </div>
  );
}
