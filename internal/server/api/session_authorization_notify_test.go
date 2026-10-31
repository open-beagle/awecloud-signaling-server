package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type authorizationNotifyFunc func()

func (f authorizationNotifyFunc) NotifySessionAuthorizationChange() { f() }

func TestSessionAuthorizationNotificationFollowsSuccessfulWrite(t *testing.T) {
	for _, tc := range []struct {
		method string
		status int
		want   int
	}{
		{http.MethodPost, 200, 1}, {http.MethodDelete, 204, 1},
		{http.MethodPut, 200, 1}, {http.MethodPatch, 200, 1},
		{http.MethodPost, 409, 0}, {http.MethodPost, 500, 0},
		{http.MethodGet, 200, 0}, {http.MethodOptions, 204, 0},
	} {
		t.Run(tc.method+http.StatusText(tc.status), func(t *testing.T) {
			committed, calls := false, 0
			router := gin.New()
			router.Use(NotifySessionAuthorizationAfterWrite(authorizationNotifyFunc(func() {
				require.True(t, committed, "notification must not race the write transaction")
				calls++
			})))
			router.Handle(tc.method, "/write", func(c *gin.Context) {
				committed = tc.status < 300
				c.Status(tc.status)
			})
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tc.method, "/write", nil))
			require.Equal(t, tc.want, calls)
		})
	}
}
