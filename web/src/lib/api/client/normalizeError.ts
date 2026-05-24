import axios from "axios";
import { AuthError } from "@/lib/errors/auth";

export interface AppError {
    error: string;
    message: string;
    status?: number;
    redirect?: boolean;
}

export function normalizeError(error: unknown): AppError {
    if (error instanceof AuthError) {
        return {
            error: "Authentication Required",
            message: error.message,
            status: 401,
            redirect: true,
        };
    }

    if (axios.isAxiosError(error)) {
        if (!error.response) {
            // network, CORS, or timeout
            return {
                error: "Network Error",
                message: "Please check your connection.",
            };
        }

        const status = error.response.status;
        const data = error.response.data;

        if (status === 401) {
            return {
                error: data.error || "Unauthorized",
                message: data.message || "Your session is invalid or expired.",
                status,
                redirect: true,
            };
        }

        return {
            error: data.error || "Unknown Error",
            message: data.message || "Unexpected error occured.",
            status,
        }
    }

    return {
        error: "Unknown Error",
        message: "Unexpected error occurred.",
    };
}
