package middleware

import "github.com/gin-gonic/gin"

// APIVersion is the current public API version. The customer API is served under
// /<APIVersion> (and, for now, also at the bare path as a deprecated alias).
const APIVersion = "v1"

// APIVersionMiddleware stamps every response with the current API version so a
// client can detect which surface it is talking to without parsing the URL.
func APIVersionMiddleware(version string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("API-Version", version)
		c.Next()
	}
}
