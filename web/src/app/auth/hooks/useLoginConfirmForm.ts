import React, { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useTurnstile } from "react-turnstile";
import useLoginConfirm from "@/lib/api/hooks/auth/useLoginConfirm";
import { saveTokens } from "@/lib/auth";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export function useLoginConfirmForm() {
    const params = useParams();
    const navigate = useNavigate();
    const turnstile = useTurnstile();
    const loginConfirm = useLoginConfirm();

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
            const r = await toast.promise(
                loginConfirm.mutateAsync({ session, code: otp.map(v => v || "0").join(""), turnstile: tk }),
                { loading: "Loading...", success: "Successfully authorized.", error: (err: AppError) => buildError(err) }
            );
            saveTokens(Object.fromEntries(Object.entries(r).map(([k, v]) => [k, String(v)])));
            navigate("/app/emails");
        } finally { setPending(false); reset(); }
    };

    const onSubmit = async (e: React.FormEvent) => { e.preventDefault(); await submit(); };
    const onToken = (t: string) => { setTk(t); if (captcha) { setCaptcha(false); if (tk) submit(); } };

    return { mail, otp, setOtp, captcha, pending, onSubmit, onToken };
}
