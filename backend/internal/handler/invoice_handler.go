package handler

import (
	"io"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type InvoiceHandler struct {
	svc      *service.InvoiceService
	payments *service.PaymentService
}

func NewInvoiceHandler(s *service.InvoiceService, p *service.PaymentService) *InvoiceHandler {
	return &InvoiceHandler{svc: s, payments: p}
}
func invoiceRespond(c *gin.Context, v any, e error) {
	if e != nil {
		response.ErrorFrom(c, e)
		return
	}
	response.Success(c, v)
}
func (h *InvoiceHandler) Config(c *gin.Context) {
	v, e := h.svc.GetConfig(c.Request.Context())
	invoiceRespond(c, v, e)
}
func (h *InvoiceHandler) UpdateConfig(c *gin.Context) {
	var in service.InvoiceConfig
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "开票配置格式错误")
		return
	}
	v, e := h.svc.UpdateConfig(c.Request.Context(), in)
	invoiceRespond(c, v, e)
}
func (h *InvoiceHandler) Eligible(c *gin.Context) {
	u, ok := requireAuth(c)
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(c.Query("after"), 10, 64)
	v, e := h.svc.EligibleOrders(c.Request.Context(), u.UserID, after)
	invoiceRespond(c, v, e)
}
func (h *InvoiceHandler) Titles(c *gin.Context) {
	u, ok := requireAuth(c)
	if !ok {
		return
	}
	v, e := h.svc.Titles(c.Request.Context(), u.UserID)
	invoiceRespond(c, v, e)
}
func (h *InvoiceHandler) DeleteTitle(c *gin.Context) {
	u, ok := requireAuth(c)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(c.Param("titleID"), 10, 64)
	e := h.svc.DeleteTitle(c.Request.Context(), u.UserID, id)
	invoiceRespond(c, gin.H{"deleted": e == nil}, e)
}
func (h *InvoiceHandler) Create(c *gin.Context) {
	u, ok := requireAuth(c)
	if !ok {
		return
	}
	var in service.InvoiceCreate
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "开票申请格式错误")
		return
	}
	v, e := h.svc.Create(c.Request.Context(), u.UserID, in)
	invoiceRespond(c, v, e)
}
func (h *InvoiceHandler) List(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := requireAuth(c)
		if !ok {
			return
		}
		user := u.UserID
		if admin {
			user = 0
		}
		page, _ := strconv.Atoi(c.Query("page"))
		size, _ := strconv.Atoi(c.Query("page_size"))
		v, e := h.svc.List(c.Request.Context(), user, page, size, c.Query("status"), c.Query("q"))
		invoiceRespond(c, v, e)
	}
}
func (h *InvoiceHandler) Detail(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := requireAuth(c)
		if !ok {
			return
		}
		user := u.UserID
		if admin {
			user = 0
		}
		v, e := h.svc.Detail(c.Request.Context(), c.Param("id"), user)
		invoiceRespond(c, v, e)
	}
}
func (h *InvoiceHandler) Action(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := requireAuth(c)
		if !ok {
			return
		}
		var in struct {
			Action           string `json:"action"`
			Note             string `json:"note"`
			ConfirmNotIssued bool   `json:"confirm_not_issued"`
		}
		if c.ShouldBindJSON(&in) != nil {
			response.BadRequest(c, "操作格式错误")
			return
		}
		user := u.UserID
		if admin {
			user = 0
		} else if in.Action != "withdraw" {
			response.Forbidden(c, "用户仅可撤回待审核申请")
			return
		}
		v, e := h.svc.Action(c.Request.Context(), c.Param("id"), user, u.UserID, in.Action, in.Note, in.ConfirmNotIssued)
		invoiceRespond(c, v, e)
	}
}
func (h *InvoiceHandler) Download(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := requireAuth(c)
		if !ok {
			return
		}
		user := u.UserID
		if admin {
			user = 0
		}
		body, kind, name, e := h.svc.Download(c.Request.Context(), c.Param("id"), user)
		if e != nil {
			response.ErrorFrom(c, e)
			return
		}
		c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Data(http.StatusOK, kind, body)
	}
}

func (h *InvoiceHandler) Upload(c *gin.Context) {
	actor, ok := requireAuth(c)
	if !ok {
		return
	}
	file, err := c.FormFile("file")
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	}
	if err != nil {
		response.BadRequest(c, "请选择PDF、PNG或JPG文件，最大20MB")
		return
	}
	if file.Size > service.MaxInvoiceFileSize {
		response.BadRequest(c, "发票文件不能超过20MB")
		return
	}
	reader, err := file.Open()
	if err != nil {
		response.BadRequest(c, "无法读取文件")
		return
	}
	defer func() { _ = reader.Close() }()
	content, err := io.ReadAll(io.LimitReader(reader, service.MaxInvoiceFileSize+1))
	if err != nil {
		response.BadRequest(c, "文件读取失败")
		return
	}
	result, err := h.svc.Upload(c.Request.Context(), c.Param("id"), actor.UserID, content, c.PostForm("invoice_number"), c.PostForm("confirm_external_checked") == "true")
	invoiceRespond(c, result, err)
}

func (h *InvoiceHandler) Quote(c *gin.Context) {
	u, ok := requireAuth(c)
	if !ok {
		return
	}
	var in struct {
		OrderIDs []int64 `json:"order_ids"`
	}
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "Invalid orders")
		return
	}
	value, err := h.svc.Quote(c.Request.Context(), u.UserID, in.OrderIDs)
	invoiceRespond(c, value, err)
}
func (h *InvoiceHandler) FeePayment(c *gin.Context) {
	u, ok := requireAuth(c)
	if !ok {
		return
	}
	value, err := h.svc.FeePayment(c.Request.Context(), c.Param("id"), u.UserID)
	invoiceRespond(c, value, err)
}
func (h *InvoiceHandler) RefundFee(c *gin.Context) {
	if _, ok := requireAuth(c); !ok {
		return
	}
	ctx := c.Request.Context()
	id := c.Param("id")
	if _, err := h.svc.Detail(ctx, id, 0); err != nil {
		invoiceRespond(c, nil, err)
		return
	}
	ids, err := h.svc.RefundableFeeOrders(ctx, id)
	if err != nil {
		invoiceRespond(c, nil, err)
		return
	}
	for _, oid := range ids {
		order, err := h.payments.GetOrderByID(ctx, oid)
		if err != nil {
			invoiceRespond(c, nil, err)
			return
		}
		if order.Status == "REFUND_PENDING" {
			if _, err = h.payments.QueryAndFinalizeRefund(ctx, oid); err != nil {
				invoiceRespond(c, nil, err)
				return
			}
			continue
		}
		plan, result, err := h.payments.PrepareRefund(ctx, oid, 0, "发票申请关闭或重复支付，退还开票手续费", false, false)
		if err != nil {
			invoiceRespond(c, nil, err)
			return
		}
		if result != nil {
			invoiceRespond(c, result, nil)
			return
		}
		if _, err = h.payments.ExecuteRefund(ctx, plan); err != nil {
			invoiceRespond(c, nil, err)
			return
		}
	}
	value, err := h.svc.Detail(ctx, id, 0)
	invoiceRespond(c, value, err)
}
