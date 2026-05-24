import type Token from "../api/models/auth/Token";
import { TOKEN_KEY } from "../information";
import reviveDates from "./reviveDates";

export default function getToken(): Token | null {
    const raw = localStorage.getItem(TOKEN_KEY)
    if (!raw) return null;

    try {
        const parsed = JSON.parse(raw)
        return reviveDates(parsed)
    } catch {
        return null
    }
}
