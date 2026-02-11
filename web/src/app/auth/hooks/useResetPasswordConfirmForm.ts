import React, { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import useResetPasswordConfirm from "@/lib/api/hooks/auth/useResetPasswordConfirm";
import { is64ByteHex } from "@/lib/token";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export function useResetPasswordConfirmForm() {
    const params = useParams();
    const navigate = useNavigate();
    const resetConfirm = useResetPasswordConfirm();

    const token = params["token"] ?? "";
    const isValidToken = is64ByteHex(token);
    const [password, setPassword] = useState("");
    const [password2, setPassword2] = useState("");
    const [captcha, setCaptcha] = useState(false);
    const [pending, setPending] = useState(false);

    const submit = async (turnstileToken: string) => {
        setPending(true);
        try {
            await toast.promise(
                resetConfirm.mutateAsync({ token, password, turnstile: turnstileToken }),
                { loading: "Loading...", success: "Password successfully changed", error: (err: AppError) => buildError(err) }
            );
            navigate("/auth/login?action=1");
        } finally { setPending(false); }
    };

    const onSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (pending) return;
        if (password !== password2) { toast.error("Passwords don't match. Please make sure you type the same password twice."); return; }
        setCaptcha(true);
    };
    const onToken = async (t: string) => { setCaptcha(false); await submit(t); };

    return { isValidToken, password, setPassword, password2, setPassword2, captcha, pending, onSubmit, onToken };
}
