import { Link } from "react-router-dom";
import { TurnstileModal } from "@/components/captcha/TurnstileModal";
import AuthButton from "@/components/auth/button";
import { useResetPasswordForm } from "../hooks/useResetPasswordForm";
import { Mail } from "lucide-react";

const INPUT = "w-full h-11 rounded-lg border border-slate-200 bg-white px-4 text-[15px] text-slate-900 placeholder:text-slate-400 outline-none transition-colors duration-200 focus:border-sky-400 focus:ring-4 focus:ring-sky-400/15";

export default function ResetPasswordPage() {
    const { mail, setMail, captcha, pending, sent, onSubmit, onToken } = useResetPasswordForm();

    if (sent) {
        return (
            <div className="space-y-5 text-center py-4">
                <div className="mx-auto w-14 h-14 rounded-2xl bg-sky-50 flex items-center justify-center">
                    <Mail className="w-7 h-7 text-sky-500" />
                </div>
                <div>
                    <h2 className="text-[24px] font-bold text-slate-900 tracking-tight">Check your inbox</h2>
                    <p className="text-sm text-slate-400 mt-1.5">
                        We sent a reset link to{" "}
                        <span className="text-slate-600 font-medium break-all">{mail}</span>
                    </p>
                </div>
                <Link
                    to="/auth/login"
                    className="inline-block text-sm text-sky-500 font-medium hover:text-sky-600 transition-colors pt-2"
                >
                    Back to sign in
                </Link>
            </div>
        );
    }

    return (
        <div className="space-y-6">
            <div className="text-center">
                <h1 className="text-[28px] font-bold text-slate-900 tracking-tight leading-tight">Reset password</h1>
                <p className="text-sm text-slate-400 mt-1.5">We'll send you a link to reset it</p>
            </div>

            <form onSubmit={onSubmit} className="space-y-4">
                <div className="space-y-1.5">
                    <label className="text-sm font-medium text-slate-600 pl-0.5">Email</label>
                    <input type="email" value={mail} onChange={(e) => setMail(e.target.value)} placeholder="name@company.com" required className={INPUT} />
                </div>

                <div className="pt-1">
                    <AuthButton loading={pending}>Send reset link</AuthButton>
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
