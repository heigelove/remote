package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/remote/server/internal/auth"
	"github.com/remote/server/internal/model"
	"github.com/remote/server/pkg/response"
)

const (
	CtxUserID   = "user_id"
	CtxUsername = "username"
	CtxRole     = "role"
)

func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

func Auth(jwtSvc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := parseBearer(c, jwtSvc)
		if !ok {
			return
		}
		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxUsername, claims.Username)
		c.Set(CtxRole, claims.Role)
		c.Next()
	}
}

func AdminAuth(jwtSvc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := parseBearer(c, jwtSvc)
		if !ok {
			return
		}
		if claims.Role != model.RoleAdmin {
			response.Forbidden(c, "admin role required")
			c.Abort()
			return
		}
		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxUsername, claims.Username)
		c.Set(CtxRole, claims.Role)
		c.Next()
	}
}

func parseBearer(c *gin.Context, jwtSvc *auth.Service) (*auth.Claims, bool) {
	h := c.GetHeader("Authorization")
	if h == "" {
		// also allow query token for websocket upgrades
		h = c.Query("token")
		if h != "" && !strings.HasPrefix(strings.ToLower(h), "bearer ") {
			h = "Bearer " + h
		}
	}
	if h == "" || !strings.HasPrefix(strings.ToLower(h), "bearer ") {
		response.Unauthorized(c, "missing authorization")
		c.Abort()
		return nil, false
	}
	token := strings.TrimSpace(h[7:])
	claims, err := jwtSvc.Parse(token)
	if err != nil {
		response.Unauthorized(c, "invalid token")
		c.Abort()
		return nil, false
	}
	return claims, true
}

func UserID(c *gin.Context) uint64 {
	v, _ := c.Get(CtxUserID)
	id, _ := v.(uint64)
	return id
}
