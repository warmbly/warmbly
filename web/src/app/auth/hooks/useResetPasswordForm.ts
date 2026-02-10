import React, { useState } from "react";
import { useTurnstile } from "react-turnstile";
import useResetPassword from "@/lib/api/hooks/auth/useResetPassword";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export function useResetPasswordForm() {
    const turnstile = useTurnstile();
    const resetPassword = useResetPassword();

    const [mail, setMail] = useState("");
    const [tk, setTk] = useState("");
    const [captcha, setCaptcha] = useState(false);
    const [pending, setPending] = useState(false);
    const [sent, setSent] = useState(false);

    const reset = () => { setTk(""); turnstile.reset(); };

    const submit = async () => {
        if (tk === "") { setCaptcha(true); return; }
        setPending(true);
        try {
            await toast.promise(
                resetPassword.mutateAsync({ email: mail, turnstile: tk }),
                { loading: "Loading...", success: "Email successfully sent.", error: (err: AppError) => buildError(err) }
            );
            setSent(true);
        } finally { setPending(false); reset(); }
    };

    const onSubmit = async (e: React.FormEvent) => { e.preventDefault(); if (!pending) await submit(); };
    const onToken = (t: string) => { setTk(t); if (captcha) { setCaptcha(false); if (tk) submit(); } };

    return { mail, setMail, captcha, pending, sent, onSubmit, onToken };
}
