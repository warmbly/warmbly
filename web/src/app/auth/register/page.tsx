import { Link } from "react-router-dom";
import { TurnstileModal } from "@/components/captcha/TurnstileModal";
import AuthButton from "@/components/auth/button";
import ExternalLogin from "@/components/auth/external";
import { useRegisterForm } from "../hooks/useRegisterForm";
import { WEBSITE_URL } from "@/lib/information";

const INPUT = "w-full h-11 rounded-lg border border-slate-200 bg-white px-4 text-[15px] text-slate-900 placeholder:text-slate-400 outline-none transition-colors duration-200 focus:border-sky-400 focus:ring-4 focus:ring-sky-400/15";

export default function RegisterPage() {
    const { mail, setMail, password, setPassword, password2, setPassword2, acceptTerms, setAcceptTerms, captcha, pending, onSubmit, onToken } = useRegisterForm();

    return (
        <div className="space-y-6">
            <div className="text-center">
                <h1 className="text-[28px] font-bold text-slate-900 tracking-tight leading-tight">Create your account</h1>
                <p className="text-sm text-slate-400 mt-1.5">Get started with Warmbly for free</p>
            </div>

            <form onSubmit={onSubmit} className="space-y-4">
                <div className="space-y-1.5">
                    <label className="text-sm font-medium text-slate-600 pl-0.5">Email</label>
                    <input type="email" value={mail} onChange={(e) => setMail(e.target.value)} placeholder="name@company.com" required className={INPUT} />
                </div>

                <div className="space-y-1.5">
                    <label className="text-sm font-medium text-slate-600 pl-0.5">Password</label>
                    <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Create a password" required className={INPUT} />
                </div>

                <div className="space-y-1.5">
                    <label className="text-sm font-medium text-slate-600 pl-0.5">Confirm password</label>
                    <input type="password" value={password2} onChange={(e) => setPassword2(e.target.value)} placeholder="Confirm your password" required className={INPUT} />
                </div>

                <label className="flex items-start gap-3 pt-0.5 cursor-pointer">
                    <div className="relative mt-0.5 shrink-0">
                        <input type="checkbox" checked={acceptTerms} onChange={(e) => setAcceptTerms(e.target.checked)} className="peer sr-only" />
                        <div className="size-[18px] rounded-md border-2 border-slate-300 bg-white peer-checked:bg-sky-500 peer-checked:border-sky-500 peer-focus-visible:ring-4 peer-focus-visible:ring-sky-400/15 transition-all duration-200 flex items-center justify-center">
                            {acceptTerms && (
                                <svg className="size-3 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={3}>
                                    <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                                </svg>
                            )}
                        </div>
                    </div>
                    <span className="text-[13px] text-slate-400 leading-relaxed">
                        I agree to the{" "}
                        <a href={`${WEBSITE_URL}/terms`} target="_blank" rel="noopener noreferrer" className="text-sky-500 hover:text-sky-600 font-medium transition-colors">Terms of Service</a>
                        {" "}and{" "}
                        <a href={`${WEBSITE_URL}/privacy`} target="_blank" rel="noopener noreferrer" className="text-sky-500 hover:text-sky-600 font-medium transition-colors">Privacy Policy</a>
                    </span>
                </label>

                <div className="pt-1">
                    <AuthButton loading={pending}>Create account</AuthButton>
                </div>

                <TurnstileModal visible={captcha} onToken={onToken} />
            </form>

            <div className="flex items-center gap-3">
                <div className="flex-1 h-px bg-slate-200" />
                <span className="text-xs text-slate-300 font-medium">or continue with</span>
                <div className="flex-1 h-px bg-slate-200" />
            </div>

            <ExternalLogin />

            <p className="text-center text-sm text-slate-400 pt-1">
                Already have an account?{" "}
                <Link to="/auth/login" className="text-sky-500 font-medium hover:text-sky-600 transition-colors">Sign in</Link>
            </p>
        </div>
    );
}
