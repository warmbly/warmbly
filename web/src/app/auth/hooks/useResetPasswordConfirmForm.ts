import React, { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useTurnstile } from "react-turnstile";
import useResetPasswordConfirm from "@/lib/api/hooks/auth/useResetPasswordConfirm";
import { is64ByteHex } from "@/lib/token";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export function useResetPasswordConfirmForm() {
    const params = useParams();
    const navigate = useNavigate();
    const turnstile = useTurnstile();
    const resetConfirm = useResetPasswordConfirm();

    const token = params["token"] ?? "";
    const isValidToken = is64ByteHex(token);
    const [password, setPassword] = useState("");
    const [password2, setPassword2] = useState("");
    const [tk, setTk] = useState("");
    const [captcha, setCaptcha] = useState(false);
    const [pending, setPending] = useState(false);

    const reset = () => { setTk(""); turnstile.reset(); };

    const submit = async () => {
        if (tk === "") { setCaptcha(true); }
        if (password !== password2) { toast.error("Passwords don't match. Please make sure you type the same password twice."); return; }
        setPending(true);
        try {
            await toast.promise(
                resetConfirm.mutateAsync({ token, password, turnstile: tk }),
                { loading: "Loading...", success: "Password successfully changed", error: (err: AppError) => buildError(err) }
            );
            navigate("/auth/login?action=1");
        } finally { setPending(false); reset(); }
    };

    const onSubmit = async (e: React.FormEvent) => { e.preventDefault(); if (!pending) await submit(); };
    const onToken = (t: string) => { setTk(t); if (captcha) { setCaptcha(false); submit(); } };

    return { isValidToken, password, setPassword, password2, setPassword2, captcha, pending, onSubmit, onToken };
}
