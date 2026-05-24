import type React from "react";
import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import useLogin from "@/lib/api/hooks/auth/useLogin";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export function useLoginForm() {
    const params = useParams();
    const navigate = useNavigate();
    const login = useLogin();

    const actionType = params["action"] ?? "";
    const [mail, setMail] = useState("");
    const [password, setPassword] = useState("");
    const [captcha, setCaptcha] = useState(false);
    const [pending, setPending] = useState(false);

    const submit = async (token: string) => {
        setPending(true);
        try {
            const r = await toast.promise(
                login.mutateAsync({ email: mail, password, turnstile: token }),
                { loading: "Loading...", error: (err: AppError) => buildError(err) }
            );
            navigate(`/auth/login/confirm?session=${r.session}&to=${mail}`);
        } finally { setPending(false); }
    };

    const onSubmit = async (e: React.FormEvent) => { e.preventDefault(); if (!pending) setCaptcha(true); };
    const onToken = async (t: string) => { setCaptcha(false); await submit(t); };

    return { actionType, mail, setMail, password, setPassword, captcha, pending, onSubmit, onToken };
}
