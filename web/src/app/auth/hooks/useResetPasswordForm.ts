import type React from "react";
import { useState } from "react";
import useResetPassword from "@/lib/api/hooks/auth/useResetPassword";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export function useResetPasswordForm() {
    const resetPassword = useResetPassword();

    const [mail, setMail] = useState("");
    const [captcha, setCaptcha] = useState(false);
    const [pending, setPending] = useState(false);
    const [sent, setSent] = useState(false);

    const submit = async (token: string) => {
        setPending(true);
        try {
            await toast.promise(
                resetPassword.mutateAsync({ email: mail, turnstile: token }),
                { loading: "Loading...", success: "Email successfully sent.", error: (err: AppError) => buildError(err) }
            );
            setSent(true);
        } finally { setPending(false); }
    };

    const onSubmit = async (e: React.FormEvent) => { e.preventDefault(); if (!pending) setCaptcha(true); };
    const onToken = async (t: string) => { setCaptcha(false); await submit(t); };

    return { mail, setMail, captcha, pending, sent, onSubmit, onToken };
}
