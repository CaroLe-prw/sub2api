package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *AuthHandler) CreateGoCaptchaChallenge(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	challenge, err := h.authService.CreateGoCaptchaChallenge(c.Request.Context(), ip.GetClientIP(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, challenge)
}

func (h *AuthHandler) CheckGoCaptchaChallenge(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req struct {
		ID string   `json:"id" binding:"required,len=64"`
		X  *float64 `json:"x" binding:"required,gte=0,lte=300"`
		Y  *float64 `json:"y" binding:"required,gte=0,lte=220"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid captcha answer")
		return
	}
	token, err := h.authService.CheckGoCaptchaChallenge(c.Request.Context(), req.ID, *req.X, *req.Y, ip.GetClientIP(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"token": token})
}
