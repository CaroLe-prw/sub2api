package admin

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"strconv"
	"strings"
)

func parseExcludeUserID(c *gin.Context) (int64, error) {
	raw := strings.TrimSpace(c.Query("exclude_user_id"))
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid exclude_user_id, use a positive integer")
	}
	return id, nil
}
