import { Navigate } from "react-router-dom";
import getToken from "@/lib/helper/getToken";
import AuthLayout from "@/app/auth/layout";

export default function OnboardingLayout() {
    const token = getToken();
    if (!token) {
        return <Navigate to="/auth/login" replace />;
    }

    // Reuse the AuthLayout chrome but skip its "already signed in →
    // /app/emails" guard. Without `redirectIfAuthenticated={false}`,
    // AuthLayout sees the token, redirects to /app/emails, and
    // UserProvider bounces straight back here when onboarding isn't
    // complete — a ping-pong that spams history.replaceState until
    // Firefox throws "operation is insecure".
    return <AuthLayout redirectIfAuthenticated={false} />;
}
