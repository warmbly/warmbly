import type { Bundle, ZObject } from './lib/types';
import {
  api,
  APP_BASE,
  DEFAULT_SCOPES,
  clientId,
  clientSecret,
} from './lib/client';

// Exchange the authorization code for tokens. Warmbly's token endpoint is
// RFC 6749: form-encoded body, confidential client (client_secret always
// required), and it returns a rotating refresh token.
const getAccessToken = async (z: ZObject, bundle: Bundle) => {
  const response = await z.request({
    url: api('/oauth/token'),
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body: {
      grant_type: 'authorization_code',
      code: bundle.inputData.code,
      client_id: clientId(),
      client_secret: clientSecret(),
      redirect_uri: bundle.inputData.redirect_uri,
    },
    skipThrowForStatus: true,
  });

  if (response.status !== 200) {
    throw new z.errors.Error(
      `Unable to fetch access token: ${response.content}`,
      'AuthError',
      response.status,
    );
  }

  const data = response.data;
  return {
    access_token: data.access_token,
    refresh_token: data.refresh_token,
  };
};

// Refresh swaps the (rotating) refresh token for a fresh access+refresh pair.
// Returning both lets Zapier persist the new refresh token; otherwise the next
// refresh would fail once the old one is consumed.
const refreshAccessToken = async (z: ZObject, bundle: Bundle) => {
  const response = await z.request({
    url: api('/oauth/token'),
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body: {
      grant_type: 'refresh_token',
      refresh_token: (bundle.authData as any).refresh_token,
      client_id: clientId(),
      client_secret: clientSecret(),
    },
    skipThrowForStatus: true,
  });

  if (response.status !== 200) {
    throw new z.errors.RefreshAuthError(
      `Unable to refresh access token: ${response.content}`,
    );
  }

  const data = response.data;
  return {
    access_token: data.access_token,
    refresh_token: data.refresh_token,
  };
};

// Connection test + label source. GET /v1/me is reachable by an OAuth token
// (combined-auth, no specific scope) and returns org + user identity.
const testAuth = async (z: ZObject, _bundle: Bundle) => {
  const response = await z.request({ url: api('/me') });
  return response.data;
};

export default {
  type: 'oauth2',
  test: testAuth,
  // Rendered from the GET /v1/me response, e.g. "Acme Inc (jane@acme.com)".
  connectionLabel: '{{organization_name}} ({{email}})',
  oauth2Config: {
    authorizeUrl: {
      url: `${APP_BASE}/oauth/authorize`,
      params: {
        client_id: '{{process.env.CLIENT_ID}}',
        state: '{{bundle.inputData.state}}',
        redirect_uri: '{{bundle.inputData.redirect_uri}}',
        response_type: 'code',
        scope: DEFAULT_SCOPES,
      },
    },
    getAccessToken,
    refreshAccessToken,
    autoRefresh: true,
    scope: DEFAULT_SCOPES,
  },
};
