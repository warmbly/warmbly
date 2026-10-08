import setToken from "./helper/setToken";
import type Token from "./api/models/auth/Token";
import { clearBrowseState, resumeBrowseState } from "./browseState";

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
    resumeBrowseState();
  }
}

// Everything written to browser storage that belongs to the signed-in person
// rather than to the browser. Prefix-matched because the keys embed ids.
//
// Reply drafts are the reason this exists: a draft holds the full body,
// subject and recipients of an unsent email, keyed by user, org and thread, and
// it survived signing out on a shared machine. Column widths and dismissed
// banners are deliberately not here; they are not the user's data.
const SESSION_SCOPED_KEY_PREFIXES = [
  "warmbly-reply-draft:",
];

const SESSION_SCOPED_KEYS = [
  "sso_binding",
];

// clearTokens ends the client's half of the session: the tokens, and the
// content those tokens were used to fetch. Called from logout and from every
// path that discovers the session is gone, so neither leaves the other behind.
export const clearTokens = () => {
  clearBrowseState();
  TOKENS.forEach((k) => localStorage.removeItem(k));
  SESSION_SCOPED_KEYS.forEach((k) => {
    localStorage.removeItem(k);
    sessionStorage.removeItem(k);
  });

  // Collect first, then remove: removing while iterating shifts the indices.
  const stale: string[] = [];
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i);
    if (key && SESSION_SCOPED_KEY_PREFIXES.some((p) => key.startsWith(p))) stale.push(key);
  }
  stale.forEach((k) => localStorage.removeItem(k));

  setToken(null);
}

// SESSION_ENDED_EVENT is how the api client, which is not a component and
// holds no router, says the session is over.
//
// Clearing the tokens was never enough on its own. The three queries
// UserProvider watches are cached and none of them polls, so nothing re-ran to
// notice, and the app stayed on a page it could no longer authenticate while
// the socket retried its handshake every few seconds for as long as the tab
// was open.
export const SESSION_ENDED_EVENT = "warmbly:session-ended";

// announceSessionEnded says the session is over without touching storage.
// Separate from endSession because the "there is no token at all" path has
// nothing to clear, and clearing there would take the SSO binding and the
// reply drafts with it on a path that also runs before anyone has signed in.
export const announceSessionEnded = () => {
  if (typeof window !== "undefined") {
    window.dispatchEvent(new Event(SESSION_ENDED_EVENT));
  }
};

// endSession ends the session and says so. Use it wherever the server has
// refused the credentials; clearTokens alone is for a sign-out that already
// owns the navigation.
export const endSession = () => {
  clearTokens();
  announceSessionEnded();
};
