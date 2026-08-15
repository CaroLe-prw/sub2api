package admin

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type LotteryHandler struct {
	service *service.LotteryService
}

func NewLotteryHandler(lotteryService *service.LotteryService) *LotteryHandler {
	return &LotteryHandler{service: lotteryService}
}

type UpdateLotteryConfigRequest struct {
	Enabled                bool      `json:"enabled"`
	StartsAt               time.Time `json:"starts_at"`
	EndsAt                 time.Time `json:"ends_at"`
	FirstPrizeReward       float64   `json:"first_prize_reward"`
	FirstPrizeWeight       int       `json:"first_prize_weight"`
	FirstPrizeWinnerCount  int       `json:"first_prize_winner_count"`
	SecondPrizeReward      float64   `json:"second_prize_reward"`
	SecondPrizeWeight      int       `json:"second_prize_weight"`
	SecondPrizeWinnerCount int       `json:"second_prize_winner_count"`
	ThirdPrizeReward       float64   `json:"third_prize_reward"`
	ThirdPrizeWeight       int       `json:"third_prize_weight"`
	ThirdPrizeWinnerCount  int       `json:"third_prize_winner_count"`
}

func (h *LotteryHandler) GetConfig(c *gin.Context) {
	config, err := h.service.GetAdminConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, config)
}

func (h *LotteryHandler) UpdateConfig(c *gin.Context) {
	var req UpdateLotteryConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	config, err := h.service.Configure(c.Request.Context(), service.LotteryConfigureInput{
		Enabled:                req.Enabled,
		StartsAt:               req.StartsAt,
		EndsAt:                 req.EndsAt,
		FirstPrizeReward:       req.FirstPrizeReward,
		FirstPrizeWeight:       req.FirstPrizeWeight,
		FirstPrizeWinnerCount:  req.FirstPrizeWinnerCount,
		SecondPrizeReward:      req.SecondPrizeReward,
		SecondPrizeWeight:      req.SecondPrizeWeight,
		SecondPrizeWinnerCount: req.SecondPrizeWinnerCount,
		ThirdPrizeReward:       req.ThirdPrizeReward,
		ThirdPrizeWeight:       req.ThirdPrizeWeight,
		ThirdPrizeWinnerCount:  req.ThirdPrizeWinnerCount,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, config)
}

func (h *LotteryHandler) ListResults(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	results, total, err := h.service.AdminListResults(c.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, results, total, page, pageSize)
}
