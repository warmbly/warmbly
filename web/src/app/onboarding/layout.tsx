import { Navigate, Outlet } from "react-router-dom";
import getToken from "@/lib/helper/getToken";
import AuthLayout from "@/app/auth/layout";

export default function OnboardingLayout() {
    const token = getToken();
    if (!token) {
        return <Navigate to="/auth/login" replace />;
    }

    return <AuthLayout />;
}
