import React, { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useTurnstile } from "react-turnstile";
import useLogin from "@/lib/api/hooks/auth/useLogin";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export function useLoginForm() {
    const params = useParams();
    const navigate = useNavigate();
    const turnstile = useTurnstile();
    const login = useLogin();

    const actionType = params["action"] ?? "";
    const [mail, setMail] = useState("");
    const [password, setPassword] = useState("");
    const [tk, setTk] = useState("");
    const [captcha, setCaptcha] = useState(false);
    const [pending, setPending] = useState(false);

    const reset = () => { setTk(""); turnstile.reset(); };

    const submit = async () => {
        if (tk === "") { setCaptcha(true); return; }
        setPending(true);
        try {
            const r = await toast.promise(
                login.mutateAsync({ email: mail, password, turnstile: tk }),
                { loading: "Loading...", error: (err: AppError) => buildError(err) }
            );
            navigate(`/auth/login/confirm?session=${r.session}&to=${mail}`);
        } finally { setPending(false); reset(); }
    };

    const onSubmit = async (e: React.FormEvent) => { e.preventDefault(); if (!pending) await submit(); };
    const onToken = (t: string) => { setTk(t); if (captcha) { setCaptcha(false); if (tk) submit(); } };

    return { actionType, mail, setMail, password, setPassword, captcha, pending, onSubmit, onToken };
}
