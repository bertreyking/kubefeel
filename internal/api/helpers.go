package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type apiError struct {
	Message string `json:"message"`
}

func respondData(c *gin.Context, status int, data any) {
	c.JSON(status, gin.H{"data": data})
}

func respondError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, apiError{Message: message})
}

func respondNoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}
