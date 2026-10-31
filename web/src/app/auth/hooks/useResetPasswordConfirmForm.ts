import type React from "react";
import { useState, useEffect } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "@/hooks/useSearchParams";
import useResetPasswordConfirm from "@/lib/api/hooks/auth/useResetPasswordConfirm";
import { usePasswordStrength } from "@/hooks/usePasswordStrength";
import toast from "react-hot-toast/headless";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import resetPasswordSession from "@/lib/api/client/auth/resetPasswordSession";
import { resetLinkToken } from "@/lib/resetLinkToken";
import getToken from "@/lib/helper/getToken";
import { clearClientSession } from "@/lib/session";

export function useResetPasswordConfirmForm() {
    // The link token is exchanged for a reset session held in memory, and cleared from the address bar.
    const [params, setParams] = useSearchParams();
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const resetConfirm = useResetPasswordConfirm();
    const { evaluate } = usePasswordStrength();

    const [linkToken] = useState(() => resetLinkToken(window.location.hash, params));
    const [session, setSession] = useState("");
    const [linkState, setLinkState] = useState<"checking" | "ready" | "invalid">(linkToken ? "checking" : "invalid");
    const isValidToken = linkState !== "invalid";

    useEffect(() => {
        if (!linkToken) return;
        let cancelled = false;
        resetPasswordSession(linkToken)
            .then((r) => {
                if (cancelled) return;
                setSession(r.session);
                setLinkState("ready");
            })
            .catch((err: AppError) => {
                if (cancelled) return;
                const status = err?.status ?? 0;
                if (status >= 400 && status < 500 && status !== 404) {
                    setLinkState("invalid");
                    return;
                }
                // A backend without the exchange still takes the link token itself.
                setSession(linkToken);
                setLinkState("ready");
            });
        setParams({}, { replace: true });
        return () => { cancelled = true; };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [linkToken]);

    const [password, setPassword] = useState("");
    const [password2, setPassword2] = useState("");
    const [captcha, setCaptcha] = useState(false);
    const [pending, setPending] = useState(false);
    const [strength, setStrength] = useState<{ score: 0 | 1 | 2 | 3 | 4; warning: string }>({ score: 0, warning: "" });

    useEffect(() => {
        if (!password) { setStrength({ score: 0, warning: "" }); return; }
        let cancelled = false;
        evaluate(password).then((r) => { if (!cancelled) setStrength({ score: r.score, warning: r.warning }); });
        return () => { cancelled = true; };
    }, [password, evaluate]);

    const submit = async (turnstileToken: string) => {
        setPending(true);
        try {
            await toast.promise(
                resetConfirm.mutateAsync({ session, password, turnstile: turnstileToken }),
                { loading: "Loading...", success: "Password successfully changed", error: (err: AppError) => buildError(err) }
            );
            // The reset ended every session, this browser's included.
            if (getToken()) clearClientSession(queryClient);
            navigate({ to: "/auth/login", search: { action: "1" } });
        } catch (err) {
            // The link died meanwhile (used, expired, or the password changed): show the request-a-new-one screen.
            if ((err as AppError)?.code === "reset_link_invalid") setLinkState("invalid");
        } finally { setPending(false); }
    };

    const onSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (pending) return;
        if (linkState !== "ready") { toast.error("Still checking your reset link. Try again in a moment."); return; }
        if (password.length < 8) { toast.error("Password must be at least 8 characters long."); return; }
        if (password !== password2) { toast.error("Passwords don't match. Please make sure you type the same password twice."); return; }
        const result = await evaluate(password);
        if (result.score < 2) { toast.error(result.warning || "Please choose a stronger password."); return; }
        setCaptcha(true);
    };
    const onToken = async (t: string) => { setCaptcha(false); await submit(t); };

    return { isValidToken, password, setPassword, password2, setPassword2, strength, captcha, pending, onSubmit, onToken };
}
