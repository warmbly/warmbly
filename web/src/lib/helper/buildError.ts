import type { AppError } from "../api/client/normalizeError";

export default function buildError(err: AppError): string {
    return `${err.status && `${err.status} `}${err.error}:\n${err.message}`
}
