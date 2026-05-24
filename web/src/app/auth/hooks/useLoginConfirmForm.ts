import type React from "react";
import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import useLoginConfirm from "@/lib/api/hooks/auth/useLoginConfirm";
import { saveTokens } from "@/lib/auth";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export function useLoginConfirmForm() {
    const params = useParams();
    const navigate = useNavigate();
    const loginConfirm = useLoginConfirm();

    const mail = params["to"] ?? "";
    const session = params["session"] ?? "";
    const [otp, setOtp] = useState<string[]>(Array(6).fill(""));
    const [captcha, setCaptcha] = useState(false);
    const [pending, setPending] = useState(false);

    const submit = async (token: string) => {
        setPending(true);
        try {
            const r = await toast.promise(
                loginConfirm.mutateAsync({ session, code: otp.map(v => v || "0").join(""), turnstile: token }),
                { loading: "Loading...", success: "Successfully authorized.", error: (err: AppError) => buildError(err) }
            );
            saveTokens(Object.fromEntries(Object.entries(r).map(([k, v]) => [k, String(v)])));
            navigate("/app/emails");
        } finally { setPending(false); }
    };

    const onSubmit = async (e: React.FormEvent) => { e.preventDefault(); if (!pending) setCaptcha(true); };
    const onToken = async (t: string) => { setCaptcha(false); await submit(t); };

    return { mail, otp, setOtp, captcha, pending, onSubmit, onToken };
}
