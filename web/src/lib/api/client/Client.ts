import axios from "axios";
import { API_BASE_URL } from "@/lib/information";
import { noteStep } from "@/lib/observability";
import { normalizeError } from "./normalizeError";

const Client = axios.create({
    baseURL: API_BASE_URL,
})

Client.interceptors.response.use(
    (response) => response,
    (error) => {
        const normalized = normalizeError(error);
        noteFailure(error, normalized);
        throw normalized;
    }
);

// noteFailure leaves the failed call on the trail the next exception carries.
//
// It is the single most useful thing an issue can say: a render that throws
// because a query came back empty is unreadable on its own and obvious next to
// "GET /campaigns/:id 500 req-abc123". The request id is the API's own, so the
// same incident is findable in the backend's logs.
//
// The path and nothing else about the call travels: no query string, no body,
// no header.
function noteFailure(error: unknown, normalized: { status?: number; code?: string; request_id?: string }): void {
    if (!axios.isAxiosError(error)) return;

    const method = error.config?.method?.toUpperCase() ?? "REQUEST";
    const path = error.config?.url?.split("?")[0] ?? "";
    const status = normalized.status ?? 0;

    const properties: Record<string, string | number | boolean> = { method, path, status };
    if (normalized.code) properties.code = normalized.code;
    if (normalized.request_id) properties.request_id = normalized.request_id;

    noteStep(`${method} ${path} ${status || "failed"}`, properties);
}

export default Client;
