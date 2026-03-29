package api

import (
	"net/http"
	"strings"

	"multikube-manager/internal/model"

	"github.com/gin-gonic/gin"
)

const currentUserContextKey = "currentUser"
const sessionCookieName = "kubefeel_session"

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		tokenFromHeader := false
		if token == header {
			token = ""
		} else if token != "" {
			tokenFromHeader = true
		}
		if token == "" {
			if cookieToken, err := c.Cookie(sessionCookieName); err == nil {
				token = strings.TrimSpace(cookieToken)
			}
		}
		if token == "" {
			respondError(c, http.StatusUnauthorized, "missing auth token")
			return
		}

		claims, err := s.jwtManager.Parse(token)
		if err != nil {
			respondError(c, http.StatusUnauthorized, "invalid token")
			return
		}

		user, err := s.loadUserByID(claims.UserID)
		if err != nil {
			respondError(c, http.StatusUnauthorized, "user not found")
			return
		}

		if !user.Active {
			respondError(c, http.StatusForbidden, "user disabled")
			return
		}

		c.Set(currentUserContextKey, user)
		if tokenFromHeader {
			setSessionCookie(c, token, claims.ExpiresAt.Time)
		}
		c.Next()
	}
}

func (s *Server) requirePermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := currentUserFromContext(c)
		if user == nil {
			respondError(c, http.StatusUnauthorized, "user context missing")
			return
		}

		if hasPermission(user, permission) {
			c.Next()
			return
		}

		respondError(c, http.StatusForbidden, "permission denied")
	}
}

func currentUserFromContext(c *gin.Context) *model.User {
	raw, ok := c.Get(currentUserContextKey)
	if !ok {
		return nil
	}

	user, ok := raw.(*model.User)
	if !ok {
		return nil
	}

	return user
}

func hasPermission(user *model.User, permission string) bool {
	if user.HasRole("admin") {
		return true
	}

	for _, key := range user.PermissionKeys() {
		if key == permission {
			return true
		}
	}

	return false
}
