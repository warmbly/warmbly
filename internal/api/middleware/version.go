package middleware

import "github.com/gin-gonic/gin"

// APIVersion is the current public API version. The customer API is served under
// /<APIVersion> (and, for now, also at the bare path as a deprecated alias).
const APIVersion = "v1"

// apiVersionSunset is when the unversioned bare-path aliases are scheduled to be
// removed. Integrators should migrate to the /v1 paths before this date. It is a
// conservative far-future target, not a hard cut tomorrow; revise as policy
// firms up.
const apiVersionSunset = "Mon, 13 Dec 2027 00:00:00 GMT"

// APIVersionMiddleware stamps every response with the current API version so a
// client can detect which surface it is talking to without parsing the URL.
func APIVersionMiddleware(version string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("API-Version", version)
		c.Next()
	}
}

// DeprecatedAliasMiddleware marks responses served from the unversioned bare
// paths as deprecated and points clients at the /v1 successor (RFC 8594
// Deprecation + Sunset, plus a 299 Warning for older clients). Applied only to
// the bare-path alias mount, never to /v1.
//
// The nudge targets EXTERNAL integrators only: it emits just for API-key callers.
// The first-party dashboard authenticates with a JWT and shares one HTTP client
// with the unversioned /auth routes, so it stays on the bare paths without being
// told it is deprecated. It must run after the auth middleware (it reads the
// resolved auth type), which it does in the route group ordering.
func DeprecatedAliasMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if GetAuthType(c) == AuthTypeAPIKey {
			c.Header("Deprecation", "true")
			c.Header("Sunset", apiVersionSunset)
			c.Header("Link", `</v1>; rel="successor-version"`)
			c.Header("Warning", `299 - "Unversioned Warmbly API paths are deprecated; migrate to /v1"`)
		}
		c.Next()
	}
}
