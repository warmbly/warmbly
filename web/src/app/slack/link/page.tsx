import { DashboardImage } from "@/components/ui/dashboard-image";

// /slack/link?code=…: where the Warmbly app in Slack sends a member to link
// their Slack account. Standalone on the auth screen's sky, like /connect.

import React from "react";
import { Link, Navigate } from "@tanstack/react-router";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, ExternalLinkIcon, LinkIcon, Loader2Icon, TriangleAlertIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import BrandMark from "@/components/shared/BrandMark";
import { Logo } from "@/components/svg";
import ProviderGlyph from "@/app/app/integrations/_components/ProviderGlyph";
import { useSearchParams } from "@/hooks/useSearchParams";
import getToken from "@/lib/helper/getToken";
import { authorizeInPopup } from "@/lib/integrations/oauthPopup";
import { startSlackLinkVerify } from "@/lib/api/client/app/integrations/slack";
import { useConfirmSlackLink, useSlackLinkPreview } from "@/lib/api/hooks/app/integrations/useSlack";
import type { SlackLinkPreview, SlackUserLink } from "@/lib/api/models/app/integrations/Slack";
import type { AppError } from "@/lib/api/client/normalizeError";
import { errorMessage } from "@/lib/errors/message";

import HelpLink from "@/components/shared/HelpLink";
import { helpLink } from "@/lib/helpLink";

const LINKING_DOCS = "https://docs.warmbly.com/guides/slack/#link-your-slack-account";

export default function SlackLinkPage() {
    if (!getToken()) {
        const next = window.location.pathname + window.location.search;
        return <Navigate to="/auth/login" search={{ next }} replace />;
    }
    return <SlackLinkInner />;
}

function SlackLinkInner() {
    const [params] = useSearchParams();
    const code = (params.get("code") ?? "").trim();
    const preview = useSlackLinkPreview(code);
    const confirm = useConfirmSlackLink();
    const [linked, setLinked] = React.useState<SlackUserLink | null>(null);
    const [verifying, setVerifying] = React.useState(false);

    function onError(err: unknown) {
        const e = err as AppError;
        if (e?.status === 404 || e?.code === "slack_link_invalid") {
            toast.error("This link has expired. Ask Warmbly in Slack for a new one.");
            void preview.refetch();
            return;
        }
        if (err instanceof Error && err.message === "access_denied") {
            toast.error("Slack sign-in was cancelled.");
            return;
        }
        if (e?.code === "slack_link_email_mismatch") void preview.refetch();
        toast.error(errorMessage(err, "Could not link your Slack account"));
    }

    async function onConnect() {
        try {
            setLinked(await confirm.mutateAsync({ code }));
        } catch (err) {
            onError(err);
        }
    }

    async function onVerify() {
        setVerifying(true);
        try {
            const res = await authorizeInPopup(() => startSlackLinkVerify(code).then((r) => r.url));
            setLinked(await confirm.mutateAsync({ code, slack_code: res.code, state: res.state }));
        } catch (err) {
            onError(err);
        } finally {
            setVerifying(false);
        }
    }

    let key: string;
    let content: React.ReactNode;
    if (!code) {
        key = "missing";
        content = <Problem title="This link is missing its code" body="Open the link from the message Warmbly sent you in Slack, or mention @Warmbly to get a new one." />;
    } else if (linked) {
        key = "done";
        content = <Done link={linked} p={preview.data} />;
    } else if (preview.isPending) {
        key = "loading";
        content = (
            <div className="py-20 flex justify-center">
                <Loader2Icon className="w-5 h-5 animate-spin text-slate-400" />
            </div>
        );
    } else if (preview.isError || !preview.data) {
        const e = preview.error as unknown as AppError | null;
        const expired = e?.status === 404 || e?.code === "slack_link_invalid";
        key = "error";
        content = (
            <Problem
                title={expired ? "This link has expired" : "Could not open this link"}
                body={expired ? "Links from Slack work once, for 15 minutes. Mention @Warmbly in Slack and it will send you a fresh one." : errorMessage(e, "Try again in a moment.")}
            />
        );
    } else if (!preview.data.is_member) {
        const p = preview.data;
        key = "not-member";
        content = (
            <Problem
                title={`You're not in ${p.organization_name || "this workspace"}`}
                body={`Only members of ${p.organization_name || "the workspace"} can link a Slack account to it${p.user_email ? `, and you're signed in as ${p.user_email}` : ""}. Ask an admin to invite you, or sign in with the account that belongs to it.`}
            />
        );
    } else {
        key = "review";
        content = <Review p={preview.data} connecting={confirm.isPending && !verifying} verifying={verifying} onConnect={() => void onConnect()} onVerify={() => void onVerify()} />;
    }

    return (
        <div className="relative min-h-dvh w-full overflow-hidden flex flex-col items-center justify-center px-4 py-8 sm:py-10">
            <div className="absolute inset-0" aria-hidden="true">
                <div className="sky-base" />
                <div className="sky-breathe" />
                <div className="sun-glow" />
                <DashboardImage src="/backdrops/cloud-3.webp" alt="" decoding="async" className="cloud-drift cloud-1 absolute select-none" style={{ top: "6%", left: "-10%", width: 360, opacity: 0.55, height: "auto" }} />
                <DashboardImage src="/backdrops/cloud-4.webp" alt="" decoding="async" className="cloud-drift cloud-2 absolute select-none" style={{ bottom: "8%", right: "-8%", width: 320, opacity: 0.5, height: "auto" }} />
            </div>
            <div className="relative z-10 w-full max-w-[460px]">
                <BrandMark className="mb-5 flex w-fit items-center gap-2.5 mx-auto" />
                <motion.div
                    initial={{ y: 14, opacity: 0 }}
                    animate={{ y: 0, opacity: 1 }}
                    transition={{ duration: 0.4, ease: [0.16, 1, 0.3, 1] }}
                    className="animate-card-float rounded-3xl border border-slate-200 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04),0_30px_70px_-32px_rgba(15,23,42,0.32)] overflow-hidden"
                >
                    <AnimatePresence mode="wait" initial={false}>
                        <motion.div
                            key={key}
                            initial={{ opacity: 0, y: 8 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: -8 }}
                            transition={{ duration: 0.22, ease: [0.16, 1, 0.3, 1] }}
                            className="px-6 py-8 sm:px-9 sm:py-9"
                        >
                            {content}
                        </motion.div>
                    </AnimatePresence>
                </motion.div>
                <div className="theme-solid mt-5 flex items-center justify-center gap-3 text-[12px] text-white/90">
                    <Link to="/app/integrations" className="hover:text-white transition-colors">
                        Back to dashboard
                    </Link>
                    {helpLink(LINKING_DOCS) && <span className="text-white/60">·</span>}
                    <HelpLink href={LINKING_DOCS} className="hover:text-white transition-colors" portalLabel="Support">
                        How linking works
                    </HelpLink>
                </div>
            </div>
        </div>
    );
}

function Pair({ avatar }: { avatar?: string }) {
    return (
        <div className="flex items-center justify-center">
            <span className="size-14 rounded-2xl border border-slate-200 bg-white shadow-sm flex items-center justify-center overflow-hidden">
                {avatar ? <DashboardImage src={avatar} alt="" referrerPolicy="no-referrer" className="size-full object-cover" /> : <ProviderGlyph provider="slack" name="Slack" size={10} />}
            </span>
            <span className="relative w-16 flex items-center">
                <span className="w-full border-t border-dashed border-slate-300" />
                <span className="absolute left-1/2 -translate-x-1/2 size-7 rounded-full bg-sky-50 text-sky-600 ring-4 ring-white flex items-center justify-center">
                    <LinkIcon className="w-3.5 h-3.5" />
                </span>
            </span>
            <span className="size-14 rounded-2xl bg-sky-600 shadow-sm flex items-center justify-center">
                <Logo className="w-7 text-white" />
            </span>
        </div>
    );
}

function Review({ p, connecting, verifying, onConnect, onVerify }: { p: SlackLinkPreview; connecting: boolean; verifying: boolean; onConnect: () => void; onVerify: () => void }) {
    const busy = connecting || verifying;
    const expires = new Date(p.expires_at).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
    return (
        <div>
            <Pair avatar={p.slack_user_avatar} />
            <h1 className="mt-6 text-center text-[22px] sm:text-[24px] font-semibold tracking-[-0.03em] leading-[1.15] text-slate-900">Connect Slack to Warmbly</h1>
            <p className="mt-2 text-center text-[13.5px] text-slate-500 leading-relaxed max-w-sm mx-auto">
                Warmbly will answer you in Slack as yourself, with your permissions in <span className="font-medium text-slate-700">{p.organization_name}</span>.
            </p>

            <div className="mt-6 rounded-xl border border-slate-200 divide-y divide-slate-100">
                <Account
                    glyph={
                        p.slack_user_avatar ? (
                            <DashboardImage src={p.slack_user_avatar} alt="" referrerPolicy="no-referrer" className="size-7 rounded-md object-cover" />
                        ) : (
                            <ProviderGlyph provider="slack" name="Slack" size={7} />
                        )
                    }
                    label="Slack"
                    title={p.slack_user_name || "Your Slack account"}
                    sub={p.slack_team_name || "Slack workspace"}
                />
                <Account
                    glyph={
                        <span className="size-7 rounded-md bg-sky-600 flex items-center justify-center">
                            <Logo className="w-4 text-white" />
                        </span>
                    }
                    label="Warmbly"
                    title={p.user_email || "Your Warmbly account"}
                    sub={p.organization_name}
                />
            </div>

            <div className="mt-6">
                {p.email_matches || !p.verify_available ? (
                    <button
                        type="button"
                        onClick={onConnect}
                        disabled={busy || !p.email_matches}
                        className="w-full h-11 rounded-lg bg-sky-600 hover:bg-sky-700 text-white text-[14px] font-medium inline-flex items-center justify-center gap-2 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                        {connecting && <Loader2Icon className="w-4 h-4 animate-spin" />}
                        Connect account
                    </button>
                ) : (
                    <button
                        type="button"
                        onClick={onVerify}
                        disabled={busy}
                        className="w-full h-11 rounded-lg border border-slate-200 bg-white hover:bg-slate-50 hover:border-slate-300 text-slate-900 text-[14px] font-medium inline-flex items-center justify-center gap-2.5 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                        <span aria-hidden="true" className="inline-flex">{verifying ? <Loader2Icon className="w-4 h-4 animate-spin text-slate-500" /> : <ProviderGlyph provider="slack" name="Slack" size={7} />}</span>
                        Continue with Slack
                    </button>
                )}
                <p className="mt-3 text-center text-[12px] text-slate-500 leading-relaxed">
                    {p.email_matches
                        ? "Your Slack and Warmbly emails match."
                        : p.verify_available
                          ? "Your Slack email is different, so Slack will confirm this account is yours. It takes one click."
                          : "Your Slack email is not the one you use for Warmbly. Sign in to Warmbly with the email on your Slack profile to link."}
                </p>
            </div>

            <p className="mt-6 text-center text-[11.5px] text-slate-500">This link works once and expires at {expires}.</p>
        </div>
    );
}

function Account({ glyph, label, title, sub }: { glyph: React.ReactNode; label: string; title: string; sub: string }) {
    return (
        <div className="flex items-center gap-3 px-4 py-3">
            <span className="shrink-0">{glyph}</span>
            <div className="min-w-0 flex-1">
                <div className="text-[13.5px] font-medium text-slate-900 truncate">{title}</div>
                <div className="text-[12px] text-slate-500 truncate">{sub}</div>
            </div>
            <span className="shrink-0 text-[10.5px] uppercase tracking-[0.14em] font-medium text-slate-500">{label}</span>
        </div>
    );
}

function Done({ link, p }: { link: SlackUserLink; p?: SlackLinkPreview }) {
    return (
        <div className="flex flex-col items-center text-center py-2">
            <motion.span
                initial={{ scale: 0.5, opacity: 0 }}
                animate={{ scale: 1, opacity: 1 }}
                transition={{ type: "spring", stiffness: 380, damping: 20, delay: 0.1 }}
                className="size-16 rounded-full bg-emerald-50 text-emerald-600 inline-flex items-center justify-center"
            >
                <CheckIcon className="w-8 h-8" />
            </motion.span>
            <h1 className="mt-5 text-[22px] sm:text-[24px] font-semibold tracking-[-0.03em] leading-[1.15] text-slate-900">You're connected</h1>
            <p className="mt-2.5 text-[13.5px] text-slate-500 leading-relaxed max-w-sm">
                <span className="font-medium text-slate-700">{p?.slack_user_name || "Your Slack account"}</span> is linked to{" "}
                <span className="font-medium text-slate-700">{p?.organization_name || "your Warmbly workspace"}</span>. Warmbly sent you a confirmation in Slack and will answer
                anything you asked while linking.
            </p>
            <div className="mt-7 flex flex-wrap items-center justify-center gap-2">
                <a
                    href={`https://app.slack.com/client/${encodeURIComponent(link.slack_team_id)}`}
                    className="h-10 px-4 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[13.5px] font-medium inline-flex items-center gap-1.5 transition-colors"
                >
                    Return to Slack <ExternalLinkIcon className="w-3.5 h-3.5" />
                </a>
                <Link to="/app/integrations" className="h-10 px-4 rounded-md border border-slate-200 hover:border-slate-300 text-slate-800 text-[13.5px] font-medium inline-flex items-center transition-colors">
                    Slack settings
                </Link>
            </div>
        </div>
    );
}

function Problem({ title, body }: { title: string; body: string }) {
    return (
        <div className="flex flex-col items-center text-center py-2">
            <span className="size-14 rounded-full bg-amber-50 text-amber-600 inline-flex items-center justify-center">
                <TriangleAlertIcon className="w-6 h-6" />
            </span>
            <h1 className="mt-5 text-[20px] font-semibold tracking-[-0.02em] text-slate-900">{title}</h1>
            <p className="mt-2 text-[13.5px] text-slate-500 leading-relaxed max-w-sm">{body}</p>
        </div>
    );
}
