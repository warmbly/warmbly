import Client from "../Client";
import type Token from "../../models/auth/Token";
import getToken from "@/lib/helper/getToken";
import isExpired from "@/lib/helper/isExpired";
import { saveTokens } from "@/lib/auth";

let bootstrapPromise: Promise<Token> | null = null;

export async function ensureConfengeOperatorSession(force = false): Promise<Token> {
    const current = getToken();
    if (!force && current?.access_token && !isExpired(current.access_token_expires_at)) {
        return current;
    }

    if (bootstrapPromise) return bootstrapPromise;

    bootstrapPromise = Client.post<Token>("/auth/confenge-operator/session")
        .then(({ data }) => {
            saveTokens(data as unknown as Record<string, unknown>);
            return getToken() ?? data;
        })
        .finally(() => {
            bootstrapPromise = null;
        });

    return bootstrapPromise;
}
