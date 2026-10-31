package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type SessionAuthorizationNotifier interface {
	NotifySessionAuthorizationChange()
}

// NotifySessionAuthorizationAfterWrite runs after the handler's transaction has
// committed, never inside a nested grant/session transaction. Cover management
// and legacy admin writes (including membership, device and upstream changes).
func NotifySessionAuthorizationAfterWrite(notifier SessionAuthorizationNotifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if notifier == nil || c.Writer.Status() < 200 || c.Writer.Status() >= 300 {
			return
		}
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			notifier.NotifySessionAuthorizationChange()
		}
	}
}
