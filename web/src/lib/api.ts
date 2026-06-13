import { saveTokens, TOKENS, clearTokens } from "./auth";
import { API_BASE_URL } from "./information";

export const isAuthenticated = (): boolean => {
  for (const key of TOKENS) {
    const val = localStorage.getItem(key);
    if (!val) return false;
  }
  return true;
};

export const Logout = async () => {
  try {
    await Call("/auth/logout", "POST")
  } finally {
    deleteTokens();
  }
}

export const LogoutAll = async () => {
  try {
    await Call("/auth/logout/all", "POST")
  } finally {
    deleteTokens();
  }
}

export const deleteTokens = () => {
  clearTokens();
};

export const isTokenExpired = () => {
  const expStr = localStorage.getItem('access_token_expires_at');
  if (!expStr) return true;
  return new Date(expStr) < new Date();
};

export const refreshToken = async () => {
  const expStr = localStorage.getItem('refresh_token_expires_at');
  if (!expStr) throw new UnauthorizedError("refresh_token_expires_at not found");
  if (new Date(expStr) < new Date()) {
    deleteTokens();
    throw new UnauthorizedError("Refresh token expired");
  }
  const token = localStorage.getItem('refresh_token');

  const resp = await fetch(`${API_BASE_URL}/auth/refresh`, {
    method: "POST",
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      refresh_token: token
    })
  })
  if (!resp.ok) {
    const { error } = await resp.json()
    if (resp.status === 400) {
      throw new UnauthorizedError(error)
    } else {
      throw new Error(error || "Request Failed")
    }
  } else {
    const data = await resp.json()
    console.log('Refreshed tokens', data);
    saveTokens(data)
  }
}


export class UnauthorizedError extends Error { }
export class APIError<T = unknown> extends Error {
  status: number;
  body?: T;
  constructor(message: string, status: number, body?: T) {
    super(message);
    this.status = status;
    this.body = body;
  }
}

type FetchMethod = 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH';

export async function Call(
  endpoint: string,
  method: FetchMethod = 'GET',
  body?: object,
  nocontent = false,
) {
  if (isTokenExpired()) {
    await refreshToken();
  }

  const token = localStorage.getItem('access_token');

  const res = await fetch(`${API_BASE_URL}${endpoint}`, {
    method,
    headers: {
      'Content-Type': 'application/json',
      ...(token && { Authorization: `Bearer ${token}` }),
    },
    ...(body && { body: JSON.stringify(body) }),
  });

  if (!res.ok) {
    const msg = await res.json().catch(() => ({}));

    if (res.status === 401) {
      await refreshToken();
      return await Call(endpoint, method, body)
    }

    throw new APIError(msg.error || 'Request failed', res.status, msg);
  }
  if (!nocontent) {
    return await res.json();
  }
  return
}
