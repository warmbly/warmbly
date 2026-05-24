import type Token from "../api/models/auth/Token";
import { TOKEN_KEY } from "../information";
import serializeDates from "./serializeDates";

export default function setToken(token: Token | null) {
    if (!token) {
        localStorage.removeItem(TOKEN_KEY)
    } else {
        localStorage.setItem(TOKEN_KEY, JSON.stringify(serializeDates(token)))
    }
}
