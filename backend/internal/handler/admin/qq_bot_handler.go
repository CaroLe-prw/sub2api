package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func (h *QQBotHandler) Preview(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 || page > 4 {
		response.BadRequest(c, "页码必须为 1 到 4")
		return
	}
	images, err := h.service.PreviewBoard(c.Request.Context())
	if err != nil {
		response.BadRequest(c, "无法生成看板，请先开启 V2 监控、保存展示分组，并确认服务器已安装中文字体")
		return
	}
	if page > len(images) {
		response.BadRequest(c, "没有这一页")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-QQ-Board-Pages", strconv.Itoa(len(images)))
	c.Data(http.StatusOK, "image/png", images[page-1])
}

type QQBotHandler struct{ service *service.QQBotService }

func NewQQBotHandler(s *service.QQBotService) *QQBotHandler { return &QQBotHandler{service: s} }

func (h *QQBotHandler) ModerationRecords(c *gin.Context) {
	records, err := h.service.ModerationRecords(c.Request.Context())
	if err != nil {
		response.InternalError(c, "无法读取广告处理记录")
		return
	}
	response.Success(c, records)
}
func (h *QQBotHandler) Get(c *gin.Context) {
	result, err := h.service.Get(c.Request.Context())
	if err != nil {
		response.InternalError(c, "无法读取 QQ 机器人配置")
		return
	}
	response.Success(c, result)
}
func (h *QQBotHandler) Update(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32*1024)
	var input service.QQBotUpdate
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "QQ 机器人配置格式不正确")
		return
	}
	result, err := h.service.Update(c.Request.Context(), input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
