import setToken from "./helper/setToken";
import type Token from "./api/models/auth/Token";

export function isStrongPassword(password: string): boolean {
  const minLength = 8;
  const hasUpperCase = /[A-Z]/.test(password);
  const hasLowerCase = /[a-z]/.test(password);
  const hasNumber = /\d/.test(password);

  return (
    password.length >= minLength &&
    hasUpperCase &&
    hasLowerCase &&
    hasNumber
  );
}

const ACCESS_TOKEN = "access_token"
const ACCESS_TOKEN_EXPIRATION = "access_token_expires_at"
const REFRESH_TOKEN = "refresh_token"
const REFRESH_TOKEN_EXPIRATION = "refresh_token_expires_at"

export const TOKENS = [
  ACCESS_TOKEN, ACCESS_TOKEN_EXPIRATION, REFRESH_TOKEN, REFRESH_TOKEN_EXPIRATION
]

const toDate = (value: unknown): Date => value instanceof Date ? value : new Date(String(value));

export const saveTokens = (data: Record<string, unknown>) => {
  TOKENS.forEach((k) => {
    const value = data[k];
    if (value === null || value === undefined) {
      localStorage.removeItem(k);
      return;
    }

    const str = value instanceof Date ? value.toISOString() : String(value);
    localStorage.setItem(k, str);
  });

  const access = data[ACCESS_TOKEN];
  const accessExp = data[ACCESS_TOKEN_EXPIRATION];
  const refresh = data[REFRESH_TOKEN];
  const refreshExp = data[REFRESH_TOKEN_EXPIRATION];

  if (access && accessExp && refresh && refreshExp) {
    const token: Token = {
      access_token: String(access),
      access_token_expires_at: toDate(accessExp),
      refresh_token: String(refresh),
      refresh_token_expires_at: toDate(refreshExp),
    };
    setToken(token);
  }
}

export const clearTokens = () => {
  TOKENS.forEach((k) => localStorage.removeItem(k));
  setToken(null);
}
