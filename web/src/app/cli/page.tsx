// app.warmbly.com/cli?code=XXXX-XXXX — a signed-in member authorizes the
// `warmbly` CLI running on one of their machines. Approving mints an ordinary
// API key in the workspace they pick, which the CLI collects on its next poll.
// Standalone on the auth screen's sky: enter code, review, done.

import React from "react";
import { Link, Navigate, useSearchParams } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import { REGEXP_ONLY_DIGITS_AND_CHARS } from "input-otp";
import toast from "react-hot-toast";
import {
    ArrowLeftIcon,
    ArrowRightIcon,
    BuildingIcon,
    CheckIcon,
    ExternalLinkIcon,
    KeyRoundIcon,
    Loader2Icon,
    LockIcon,
    MonitorIcon,
    TerminalIcon,
    XIcon,
} from "lucide-react";
import { Logo } from "@/components/svg";
import { InputOTP, InputOTPGroup, InputOTPSlot } from "@/components/ui/input-otp";
import { WEBSITE_URL } from "@/lib/information";
import getToken from "@/lib/helper/getToken";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import type Organization from "@/lib/api/models/app/organizations/Organization";
import useOrganizations from "@/lib/api/hooks/app/organizations/useOrganizations";
import type { CLIAuthCode } from "@/lib/api/models/app/cliauth/CLIAuth";
import { useApproveCLIAuthCode, useCLIAuthCode, useDenyCLIAuthCode } from "@/lib/api/hooks/app/cliauth/useCLIAuth";

const CODE_LENGTH = 8;

function clean(raw: string): string {
    return raw.toUpperCase().replace(/[^A-Z0-9]/g, "").slice(0, CODE_LENGTH);
}

function dashed(code: string): string {
    return code.length > 4 ? `${code.slice(0, 4)}-${code.slice(4)}` : code;
}

const slide = {
    enter: (dir: number) => ({ opacity: 0, x: dir > 0 ? 28 : -28 }),
    center: { opacity: 1, x: 0 },
    exit: (dir: number) => ({ opacity: 0, x: dir > 0 ? -28 : 28 }),
};
const slideTransition = { duration: 0.28, ease: [0.16, 1, 0.3, 1] as const };

export default function CLIAuthPage() {
    if (!getToken()) {
        const next = encodeURIComponent(window.location.pathname + window.location.search);
        return <Navigate to={`/auth/login?next=${next}`} replace />;
    }
    return <CLIAuthInner />;
}

function CLIAuthInner() {
    const [params] = useSearchParams();
    const [code, setCode] = React.useState(() => clean(params.get("code") ?? ""));
    const complete = code.length === CODE_LENGTH;
    const lookup = useCLIAuthCode(dashed(code));
    const orgs = useOrganizations();
    const approve = useApproveCLIAuthCode();
    const deny = useDenyCLIAuthCode();
    const [orgId, setOrgId] = React.useState("");
    const [outcome, setOutcome] = React.useState<"approved" | "denied" | null>(null);
    const [dir, setDir] = React.useState(1);

    React.useEffect(() => {
        if (!orgId && orgs.data && orgs.data.length > 0) setOrgId(orgs.data[0].id);
    }, [orgs.data, orgId]);

    const info = complete ? lookup.data : undefined;
    const step: "code" | "review" | "done" = outcome ? "done" : info ? "review" : "code";

    const reset = () => {
        setDir(-1);
        setCode("");
        setOutcome(null);
    };

    const doApprove = async () => {
        try {
            setDir(1);
            await approve.mutateAsync({ code: dashed(code), organizationId: orgId });
            setOutcome("approved");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };
    const doDeny = async () => {
        try {
            setDir(1);
            await deny.mutateAsync(dashed(code));
            setOutcome("denied");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    return (
        <div className="relative min-h-dvh w-full overflow-hidden flex flex-col items-center justify-center px-4 py-8 sm:py-10">
            <div className="absolute inset-0" aria-hidden="true">
                <div className="sky-base" />
                <div className="sky-breathe" />
                <div className="sun-glow" />
                <img src="/backdrops/cloud-3.webp" alt="" decoding="async" className="cloud-drift cloud-1 absolute select-none" style={{ top: "6%", left: "-10%", width: 360, opacity: 0.55, height: "auto" }} />
                <img src="/backdrops/cloud-4.webp" alt="" decoding="async" className="cloud-drift cloud-2 absolute select-none" style={{ bottom: "8%", right: "-8%", width: 320, opacity: 0.5, height: "auto" }} />
                <img src="/backdrops/cloud-1.webp" alt="" decoding="async" className="cloud-drift cloud-1 absolute select-none" style={{ top: "44%", right: "14%", width: 220, opacity: 0.35, height: "auto" }} />
            </div>

            <div className="relative z-10 w-full max-w-[560px]">
                <a href={WEBSITE_URL} className="mb-5 flex w-fit items-center gap-2.5 mx-auto">
                    <Logo className="w-7 text-white" />
                    <span className="font-extrabold text-[18px] tracking-tight text-white">Warmbly</span>
                </a>

                <motion.div
                    initial={{ y: 14, opacity: 0 }}
                    animate={{ y: 0, opacity: 1 }}
                    transition={{ duration: 0.4, ease: [0.16, 1, 0.3, 1] }}
                    className="animate-card-float rounded-3xl border border-slate-200 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04),0_30px_70px_-32px_rgba(15,23,42,0.32)] overflow-hidden"
                >
                    <Steps current={step} />

                    <div className="px-6 pb-7 pt-2 sm:px-10 sm:pb-9 overflow-hidden">
                        <AnimatePresence mode="wait" initial={false} custom={dir}>
                            {step === "code" && (
                                <motion.div key="code" custom={dir} variants={slide} initial="enter" animate="center" exit="exit" transition={slideTransition}>
                                    <CodeStep code={code} setCode={setCode} loading={complete && lookup.isLoading} error={complete && lookup.isError ? (lookup.error as unknown as AppError) : null} onRetry={reset} />
                                </motion.div>
                            )}
                            {step === "review" && info && (
                                <motion.div key="review" custom={dir} variants={slide} initial="enter" animate="center" exit="exit" transition={slideTransition}>
                                    <ReviewStep
                                        info={info}
                                        orgs={orgs.data ?? []}
                                        orgsLoading={orgs.isLoading}
                                        orgId={orgId}
                                        setOrgId={setOrgId}
                                        busy={approve.isPending || deny.isPending}
                                        approving={approve.isPending}
                                        onApprove={doApprove}
                                        onDeny={doDeny}
                                        onBack={reset}
                                    />
                                </motion.div>
                            )}
                            {step === "done" && info && (
                                <motion.div key="done" custom={dir} variants={slide} initial="enter" animate="center" exit="exit" transition={slideTransition}>
                                    <DoneStep approved={outcome === "approved"} info={info} orgName={orgs.data?.find((o) => o.id === orgId)?.name} onAnother={reset} />
                                </motion.div>
                            )}
                        </AnimatePresence>
                    </div>
                </motion.div>

                <div className="mt-5 flex items-center justify-center gap-3 text-[12px] text-white/70">
                    <Link to="/app/emails" className="hover:text-white transition-colors">Back to dashboard</Link>
                    <span className="text-white/40">·</span>
                    <a href="https://docs.warmbly.com/api/cli/" target="_blank" rel="noreferrer" className="hover:text-white transition-colors">About the CLI</a>
                </div>
            </div>
        </div>
    );
}

const STEPS: { key: "code" | "review" | "done"; label: string }[] = [
    { key: "code", label: "Code" },
    { key: "review", label: "Review" },
    { key: "done", label: "Signed in" },
];

function Steps({ current }: { current: "code" | "review" | "done" }) {
    const idx = STEPS.findIndex((s) => s.key === current);
    return (
        <div className="px-6 sm:px-10 pt-7 pb-5 flex items-center gap-2">
            {STEPS.map((s, i) => {
                const state = i < idx ? "done" : i === idx ? "current" : "todo";
                return (
                    <React.Fragment key={s.key}>
                        <div className="flex items-center gap-2">
                            <motion.span
                                animate={{
                                    backgroundColor: state === "todo" ? "#f1f5f9" : "#0284c7",
                                    color: state === "todo" ? "#94a3b8" : "#ffffff",
                                }}
                                className="size-6 rounded-full inline-flex items-center justify-center text-[11px] font-semibold"
                            >
                                {state === "done" ? <CheckIcon className="w-3 h-3" /> : i + 1}
                            </motion.span>
                            <span className={`text-[12px] font-medium ${state === "todo" ? "text-slate-400" : "text-slate-900"}`}>{s.label}</span>
                        </div>
                        {i < STEPS.length - 1 && (
                            <span className="relative flex-1 h-px bg-slate-200 overflow-hidden rounded-full">
                                <motion.span animate={{ width: i < idx ? "100%" : "0%" }} transition={{ duration: 0.4 }} className="absolute inset-y-0 left-0 bg-sky-600" />
                            </span>
                        )}
                    </React.Fragment>
                );
            })}
        </div>
    );
}

function CodeStep({ code, setCode, loading, error, onRetry }: { code: string; setCode: (c: string) => void; loading: boolean; error: AppError | null; onRetry: () => void }) {
    return (
        <div>
            <div className="text-center">
                <span className="inline-flex items-center gap-1.5 h-6 px-2.5 rounded-full bg-sky-50 text-sky-700 text-[11px] font-medium">
                    <TerminalIcon className="w-3 h-3" /> Warmbly CLI
                </span>
                <h1 className="mt-4 text-[24px] sm:text-[28px] font-semibold tracking-[-0.03em] leading-[1.1] text-slate-900">Sign in to the CLI</h1>
                <p className="mt-2.5 text-[13.5px] text-slate-500 leading-relaxed max-w-md mx-auto">
                    Enter the eight character code your terminal is showing. Approving it creates an API key for that machine, which you can revoke here at any time.
                </p>
            </div>

            <div className="mt-8 flex justify-center">
                <InputOTP maxLength={CODE_LENGTH} value={code} onChange={(v) => setCode(clean(v))} pattern={REGEXP_ONLY_DIGITS_AND_CHARS} pasteTransformer={clean} autoFocus containerClassName="gap-1.5 sm:gap-2" disabled={loading}>
                    <InputOTPGroup className="gap-1.5 sm:gap-2">
                        {[0, 1, 2, 3].map((i) => (
                            <Slot key={i} index={i} />
                        ))}
                    </InputOTPGroup>
                    <span className="w-3 h-px bg-slate-300 mx-0.5 sm:mx-1" aria-hidden="true" />
                    <InputOTPGroup className="gap-1.5 sm:gap-2">
                        {[4, 5, 6, 7].map((i) => (
                            <Slot key={i} index={i} />
                        ))}
                    </InputOTPGroup>
                </InputOTP>
            </div>

            <div className="mt-5 min-h-[44px] flex items-center justify-center">
                <AnimatePresence mode="wait" initial={false}>
                    {loading && (
                        <motion.p key="loading" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="inline-flex items-center gap-2 text-[12.5px] text-slate-500">
                            <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> Looking up your terminal
                        </motion.p>
                    )}
                    {error && !loading && (
                        <motion.div key="error" initial={{ opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0 }} className="w-full rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-center">
                            <p className="text-[13px] font-medium text-rose-700">{error.message || "That code is unknown or has expired."}</p>
                            <p className="mt-0.5 text-[12px] text-rose-600/80">
                                Run <span className="font-mono">warmbly auth login</span> again for a fresh one, then{" "}
                                <button type="button" onClick={onRetry} className="font-medium underline underline-offset-2 hover:text-rose-800">
                                    enter it here
                                </button>
                                .
                            </p>
                        </motion.div>
                    )}
                    {!loading && !error && (
                        <motion.p key="hint" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="text-[12px] text-slate-400 text-center">
                            You can paste the whole code. Codes expire ten minutes after the terminal printed them.
                        </motion.p>
                    )}
                </AnimatePresence>
            </div>
        </div>
    );
}

function Slot({ index }: { index: number }) {
    return (
        <InputOTPSlot
            index={index}
            className="!w-10 !h-12 sm:!w-12 sm:!h-14 !rounded-lg !border !border-slate-200 !shadow-none first:!rounded-lg last:!rounded-lg text-[20px] font-semibold font-mono text-slate-900 data-[active=true]:!border-sky-400 data-[active=true]:!ring-sky-400/15"
        />
    );
}

// The scope names the API returns are SCREAMING_SNAKE; the reviewer reads prose.
function scopeLabel(name: string): string {
    const words = name.toLowerCase().replace(/_/g, " ");
    return words.charAt(0).toUpperCase() + words.slice(1);
}

function ReviewStep({
    info,
    orgs,
    orgsLoading,
    orgId,
    setOrgId,
    busy,
    approving,
    onApprove,
    onDeny,
    onBack,
}: {
    info: CLIAuthCode;
    orgs: Organization[];
    orgsLoading: boolean;
    orgId: string;
    setOrgId: (id: string) => void;
    busy: boolean;
    approving: boolean;
    onApprove: () => void;
    onDeny: () => void;
    onBack: () => void;
}) {
    const pending = info.status === "pending";
    const sends = info.scope_names.includes("SEND_CAMPAIGNS") || info.scope_names.includes("WRITE_UNIBOX");

    return (
        <div>
            <button type="button" onClick={onBack} className="inline-flex items-center gap-1 text-[12px] text-slate-500 hover:text-slate-900 transition-colors">
                <ArrowLeftIcon className="w-3.5 h-3.5" /> Different code
            </button>

            <h1 className="mt-3 text-[22px] sm:text-[26px] font-semibold tracking-[-0.03em] leading-[1.1] text-slate-900">
                {pending ? "Authorize this terminal" : "This code was already used"}
            </h1>

            <div className="mt-5 flex items-center gap-4 rounded-xl border border-slate-200 bg-gradient-to-b from-sky-50/60 to-white px-4 py-4">
                <span className="size-11 rounded-lg bg-slate-900 text-white inline-flex items-center justify-center shrink-0">
                    <TerminalIcon className="w-5 h-5" />
                </span>
                <div className="min-w-0 flex-1">
                    <p className="text-[15px] font-semibold text-slate-900 truncate">{info.client_name || "Warmbly CLI"}</p>
                    <p className="text-[12px] text-slate-500 truncate inline-flex items-center gap-1.5">
                        <MonitorIcon className="w-3 h-3 shrink-0" />
                        {info.hostname || "Machine name not shared"}
                        {info.cli_version && <span className="text-slate-400">· v{info.cli_version}</span>}
                    </p>
                </div>
                <span className="hidden sm:inline-flex font-mono text-[13px] tracking-[0.18em] text-slate-400">{info.user_code}</span>
            </div>

            {!pending ? (
                <div className="mt-5">
                    <p className="text-[13px] text-slate-500 leading-relaxed">
                        {info.status === "denied"
                            ? "The request was declined. Run `warmbly auth login` again if you changed your mind."
                            : "It is signed in already. If the terminal is still waiting, run `warmbly auth login` again for a fresh code."}
                    </p>
                    <div className="mt-5 flex items-center gap-2">
                        <Link to="/app/api-keys" className="h-10 px-4 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[13px] font-medium inline-flex items-center gap-1.5 transition-colors">
                            API keys <ArrowRightIcon className="w-3.5 h-3.5" />
                        </Link>
                    </div>
                </div>
            ) : (
                <>
                    <div className="mt-6">
                        <p className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Sign in to workspace</p>
                        <div className="mt-2 space-y-1.5">
                            {orgsLoading && (
                                <div className="h-12 rounded-lg border border-slate-200 flex items-center justify-center text-slate-400">
                                    <Loader2Icon className="w-4 h-4 animate-spin" />
                                </div>
                            )}
                            {orgs.map((o) => {
                                const on = o.id === orgId;
                                return (
                                    <button
                                        key={o.id}
                                        type="button"
                                        onClick={() => setOrgId(o.id)}
                                        className={`w-full flex items-center gap-3 rounded-lg border px-3 py-2.5 text-left transition-colors ${
                                            on ? "border-sky-400 bg-sky-50/60 ring-2 ring-sky-100" : "border-slate-200 hover:border-slate-300"
                                        }`}
                                    >
                                        <span className={`size-8 rounded-md inline-flex items-center justify-center shrink-0 overflow-hidden ${on ? "bg-sky-600 text-white" : "bg-slate-100 text-slate-600"}`}>
                                            {o.avatar ? <img src={o.avatar} alt="" className="size-full object-cover" /> : <BuildingIcon className="w-4 h-4" />}
                                        </span>
                                        <span className="min-w-0 flex-1">
                                            <span className="block text-[13.5px] font-medium text-slate-900 truncate">{o.name}</span>
                                            <span className="block text-[11.5px] text-slate-500 capitalize">{o.role}</span>
                                        </span>
                                        <span className={`size-4 rounded-full border inline-flex items-center justify-center ${on ? "border-sky-600 bg-sky-600 text-white" : "border-slate-300"}`}>
                                            {on && <CheckIcon className="w-2.5 h-2.5" />}
                                        </span>
                                    </button>
                                );
                            })}
                            {!orgsLoading && orgs.length === 0 && <p className="text-[12.5px] text-slate-500">You are not a member of any workspace yet.</p>}
                        </div>
                    </div>

                    <div className="mt-5">
                        <p className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                            This terminal will be able to
                        </p>
                        <ul className="mt-2 flex flex-wrap gap-1.5">
                            {info.scope_names.map((s) => (
                                <li key={s} className="h-6 px-2 rounded-md bg-slate-100 text-slate-700 text-[11.5px] inline-flex items-center">
                                    {scopeLabel(s)}
                                </li>
                            ))}
                            {info.scope_names.length === 0 && <li className="text-[12.5px] text-slate-500">Nothing. The CLI asked for no scopes.</li>}
                        </ul>
                    </div>

                    {sends && (
                        <p className="mt-4 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2.5 text-[12.5px] text-amber-800 leading-relaxed">
                            These scopes include sending. A CLI signed in with them can start campaigns and send replies, which puts real mail on the wire.
                        </p>
                    )}

                    <ul className="mt-5 grid sm:grid-cols-2 gap-2.5">
                        <Perm icon={KeyRoundIcon} title="What this creates" body="One API key named for this machine, listed under API keys, revocable there or with `warmbly auth logout`." />
                        <Perm icon={LockIcon} title="What it is not" body="Not your password and not a session. It only carries the scopes above, in the workspace you pick." />
                    </ul>

                    <div className="mt-6 flex items-center gap-2">
                        <button
                            type="button"
                            onClick={onDeny}
                            disabled={busy}
                            className="h-10 px-4 rounded-md border border-slate-200 hover:border-slate-300 text-[13px] text-slate-700 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                        >
                            <XIcon className="w-3.5 h-3.5" /> Decline
                        </button>
                        <button
                            type="button"
                            onClick={onApprove}
                            disabled={!orgId || busy}
                            className="flex-1 h-10 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[13.5px] font-medium inline-flex items-center justify-center gap-1.5 transition-colors disabled:opacity-60"
                        >
                            {approving ? <Loader2Icon className="w-4 h-4 animate-spin" /> : <CheckIcon className="w-4 h-4" />}
                            Authorize terminal
                        </button>
                    </div>
                </>
            )}
        </div>
    );
}

function Perm({ icon: Icon, title, body }: { icon: React.ComponentType<{ className?: string }>; title: string; body: string }) {
    return (
        <li className="rounded-lg border border-slate-200 px-3 py-2.5 flex items-start gap-2.5">
            <Icon className="w-4 h-4 mt-0.5 text-sky-600 shrink-0" />
            <span>
                <span className="block text-[12px] font-semibold text-slate-900">{title}</span>
                <span className="block text-[12px] text-slate-500 leading-relaxed">{body}</span>
            </span>
        </li>
    );
}

function DoneStep({ approved, info, orgName, onAnother }: { approved: boolean; info: CLIAuthCode; orgName?: string; onAnother: () => void }) {
    return (
        <div className="flex flex-col items-center text-center py-2">
            <motion.span
                initial={{ scale: 0.5, opacity: 0 }}
                animate={{ scale: 1, opacity: 1 }}
                transition={{ type: "spring", stiffness: 380, damping: 20, delay: 0.1 }}
                className={`size-16 rounded-full inline-flex items-center justify-center ${approved ? "bg-emerald-50 text-emerald-600" : "bg-slate-100 text-slate-500"}`}
            >
                {approved ? <CheckIcon className="w-8 h-8" /> : <XIcon className="w-8 h-8" />}
            </motion.span>
            <h1 className="mt-5 text-[24px] sm:text-[28px] font-semibold tracking-[-0.03em] leading-[1.1] text-slate-900">
                {approved ? "Terminal authorized" : "Request declined"}
            </h1>
            <p className="mt-2.5 text-[13.5px] text-slate-500 leading-relaxed max-w-sm">
                {approved ? (
                    <>
                        <span className="font-medium text-slate-700">{info.hostname || "Your terminal"}</span> picks this up on its own within a few seconds
                        {orgName ? (
                            <>
                                {" "}
                                and is now signed in to <span className="font-medium text-slate-700">{orgName}</span>.
                            </>
                        ) : (
                            "."
                        )}{" "}
                        You can close this tab.
                    </>
                ) : (
                    "Nothing was created. The terminal will show that the request was declined."
                )}
            </p>
            <div className="mt-7 flex flex-wrap items-center justify-center gap-2">
                <Link
                    to="/app/api-keys"
                    className="h-10 px-4 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[13.5px] font-medium inline-flex items-center gap-1.5 transition-colors"
                >
                    API keys <ArrowRightIcon className="w-3.5 h-3.5" />
                </Link>
                <a
                    href="https://docs.warmbly.com/api/cli/"
                    target="_blank"
                    rel="noreferrer"
                    className="h-10 px-4 rounded-md border border-slate-200 hover:border-slate-300 text-slate-800 text-[13.5px] font-medium inline-flex items-center gap-1.5 transition-colors"
                >
                    CLI docs <ExternalLinkIcon className="w-3.5 h-3.5" />
                </a>
            </div>
            <button type="button" onClick={onAnother} className="mt-5 text-[12px] text-slate-500 hover:text-slate-900 transition-colors">
                Authorize another terminal
            </button>
        </div>
    );
}
