import { Link } from "react-router-dom";
import { TurnstileModal } from "@/components/captcha/TurnstileModal";
import AuthButton from "@/components/auth/button";
import { useResetPasswordConfirmForm } from "../../hooks/useResetPasswordConfirmForm";
import { AlertTriangle } from "lucide-react";

const INPUT = "w-full h-11 rounded-lg border border-sky-200 bg-white px-4 text-[15px] text-slate-800 placeholder:text-slate-300 outline-none transition-all duration-200 focus:border-sky-400 focus:ring-4 focus:ring-sky-400/15";

export default function ResetPasswordConfirmPage() {
    const { isValidToken, password, setPassword, password2, setPassword2, captcha, pending, onSubmit, onToken } = useResetPasswordConfirmForm();

    if (!isValidToken) {
        return (
            <div className="space-y-5 text-center py-4">
                <div className="mx-auto w-14 h-14 rounded-2xl bg-rose-50 flex items-center justify-center">
                    <AlertTriangle className="w-7 h-7 text-rose-500" />
                </div>
                <div>
                    <h2 className="font-serif text-[28px] text-slate-800 tracking-tight">Link expired</h2>
                    <p className="text-sm text-slate-400 mt-1.5">This password reset link is no longer valid.</p>
                </div>
                <Link
                    to="/auth/reset-password"
                    className="inline-block text-sm text-sky-500 font-medium hover:text-sky-600 transition-colors pt-2"
                >
                    Request a new link
                </Link>
            </div>
        );
    }

    return (
        <div className="space-y-6">
            <div className="text-center">
                <h1 className="font-serif text-[32px] text-slate-800 tracking-tight leading-tight">New password</h1>
                <p className="text-sm text-slate-400 mt-1.5">Choose a strong password for your account</p>
            </div>

            <form onSubmit={onSubmit} className="space-y-4">
                <div className="space-y-1.5">
                    <label className="text-sm font-medium text-slate-600 pl-0.5">New password</label>
                    <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Enter new password" required className={INPUT} />
                </div>

                <div className="space-y-1.5">
                    <label className="text-sm font-medium text-slate-600 pl-0.5">Confirm password</label>
                    <input type="password" value={password2} onChange={(e) => setPassword2(e.target.value)} placeholder="Confirm new password" required className={INPUT} />
                </div>

                <div className="pt-1">
                    <AuthButton loading={pending}>Reset password</AuthButton>
                </div>

                <TurnstileModal visible={captcha} onToken={onToken} />
            </form>

            <p className="text-center text-sm text-slate-400 pt-1">
                Remember your password?{" "}
                <Link to="/auth/login" className="text-sky-500 font-medium hover:text-sky-600 transition-colors">Sign in</Link>
            </p>
        </div>
    );
}
