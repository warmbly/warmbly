package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/aitools"
)

func TestBindCallerKeyNamesOnlyAWorkspaceAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	keyID := uuid.New()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(middleware.AuthTypeKey, middleware.AuthTypeAPIKey)
	c.Set(middleware.APIKeyIDKey, keyID.String())

	var inv aitools.Invocation
	bindCallerKey(c, &inv)
	if inv.CallerKeyID != keyID {
		t.Fatalf("caller key = %s, want %s", inv.CallerKeyID, keyID)
	}

	for _, auth := range []string{middleware.AuthTypeOAuth, middleware.AuthTypeJWT} {
		c.Set(middleware.AuthTypeKey, auth)
		inv = aitools.Invocation{}
		bindCallerKey(c, &inv)
		if inv.CallerKeyID != uuid.Nil {
			t.Fatalf("%s received caller key %s", auth, inv.CallerKeyID)
		}
	}
}
