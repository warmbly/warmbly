import type React from "react";
import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import useRegisterConfirm from "@/lib/api/hooks/auth/useRegisterConfirm";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export function useRegisterConfirmForm() {
    const params = useParams();
    const navigate = useNavigate();
    const registerConfirm = useRegisterConfirm();

    const mail = params["to"] ?? "";
    const session = params["session"] ?? "";
    const [otp, setOtp] = useState<string[]>(Array(6).fill(""));
    const [captcha, setCaptcha] = useState(false);
    const [pending, setPending] = useState(false);

    const submit = async (token: string) => {
        setPending(true);
        try {
            await toast.promise(
                registerConfirm.mutateAsync({ session, code: otp.map(v => v || "0").join(""), turnstile: token }),
                { loading: "Loading...", success: "Account successfully created.", error: (err: AppError) => buildError(err) }
            );
            navigate("/auth/login?action=0");
        } finally { setPending(false); }
    };

    const onSubmit = async (e: React.FormEvent) => { e.preventDefault(); if (!pending) setCaptcha(true); };
    const onToken = async (t: string) => { setCaptcha(false); await submit(t); };

    return { mail, otp, setOtp, captcha, pending, onSubmit, onToken };
}
