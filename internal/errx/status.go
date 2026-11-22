package errx

import "github.com/gin-gonic/gin"

// HTTPStatus is the status JSON answers this error with.
func (e *Error) HTTPStatus() int {
	status, _ := e.resolve()
	return status
}

// ClientMessage is the message JSON shows the caller: a server-class error's real text is logged and replaced.
func ClientMessage(c *gin.Context, e *Error) string { return clientMessage(c, e) }
