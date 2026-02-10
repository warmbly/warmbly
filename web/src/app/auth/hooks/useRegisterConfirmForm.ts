import React, { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useTurnstile } from "react-turnstile";
import useRegisterConfirm from "@/lib/api/hooks/auth/useRegisterConfirm";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export function useRegisterConfirmForm() {
    const params = useParams();
    const navigate = useNavigate();
    const turnstile = useTurnstile();
    const registerConfirm = useRegisterConfirm();

    const mail = params["to"] ?? "";
    const session = params["session"] ?? "";
    const [otp, setOtp] = useState<string[]>(Array(6).fill(""));
    const [tk, setTk] = useState("");
    const [captcha, setCaptcha] = useState(false);
    const [pending, setPending] = useState(false);

    const reset = () => { setTk(""); turnstile.reset(); };

    const submit = async () => {
        if (pending) return;
        if (tk === "") { setCaptcha(true); return; }
        setPending(true);
        try {
            await toast.promise(
                registerConfirm.mutateAsync({ session, code: otp.map(v => v || "0").join(""), turnstile: tk }),
                { loading: "Loading...", success: "Account successfully created.", error: (err: AppError) => buildError(err) }
            );
            navigate("/auth/login?action=0");
        } finally { setPending(false); reset(); }
    };

    const onSubmit = async (e: React.FormEvent) => { e.preventDefault(); await submit(); };
    const onToken = (t: string) => { setTk(t); if (captcha) { setCaptcha(false); if (tk) submit(); } };

    return { mail, otp, setOtp, captcha, pending, onSubmit, onToken };
}
