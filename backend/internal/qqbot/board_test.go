package qqbot

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

func previewBoard() Board {
	names := []string{"Claude Kiro高缓存-012", "Claude MAX 满血分组-15", "Claude Max 蒸馏专用", "GPT Luna-045", "GPT PRO-022", "GPT Plus-0085", "GPT Plus/Pro-0125", "GPT Pro-016"}
	cache := []string{"81.0%", "0.00%", "-", "64.8%", "75.5%", "84.9%", "83.9%", "83.2%"}
	availability := []string{"99.5%", "67.9%", "100.0%", "93.8%", "97.8%", "97.1%", "97.5%", "100.0%"}
	ttft := []string{"3.0s", "-", "-", "15.0s", "8.0s", "8.0s", "10.0s", "5.0s"}
	board := Board{Title: "VIAPI", Subtitle: "布局预览 · 示例数据（非实时） · 近 90 分钟 / 每格 5 分钟", Through: time.Date(2026, 9, 22, 8, 26, 28, 0, time.UTC)}
	for i, name := range names {
		p, state := "openai", "healthy"
		if i < 3 {
			p = "anthropic"
		}
		if i == 1 {
			state = "critical"
		}
		if i == 3 {
			state = "warning"
		}
		card := Card{Platform: p, Name: name, State: state, Cache: cache[i], Availability: availability[i], TTFT: ttft[i], AvailabilityState: state, TTFTState: "healthy"}
		if i >= 3 && i <= 6 {
			card.TTFTState = "warning"
		}
		for k := 0; k < 18; k++ {
			v := "healthy"
			if i == 1 && k < 17 {
				v = "critical"
			}
			if i == 3 || i == 4 {
				if k%5 == 0 {
					v = "warning"
				}
				if k == 7 {
					v = "critical"
				}
			}
			card.History = append(card.History, v)
		}
		board.Cards = append(board.Cards, card)
	}
	return board
}

func TestQQBoardPNGAndPagination(t *testing.T) {
	f, err := opentype.Parse(goregular.TTF)
	require.NoError(t, err)
	board := previewBoard()
	images, err := renderBoard(board, f)
	require.NoError(t, err)
	require.Len(t, images, 1)
	decoded, err := png.DecodeConfig(bytes.NewReader(images[0]))
	require.NoError(t, err)
	require.Equal(t, 1200, decoded.Width)
	require.Greater(t, decoded.Height, 1500)
	board.Cards = nil
	for i := 0; i < 51; i++ {
		board.Cards = append(board.Cards, Card{Platform: "openai", Name: fmt.Sprint(i)})
	}
	images, err = renderBoard(board, f)
	require.NoError(t, err)
	require.Len(t, images, 4)
	for _, image := range images {
		require.Less(t, len(image), 8*1024*1024)
	}
}

func TestQQBoardVisualPreview(t *testing.T) {
	output := os.Getenv("QQ_BOT_PREVIEW_FILE")
	if output == "" {
		t.Skip("set QQ_BOT_PREVIEW_FILE to render a CJK visual QA artifact")
	}
	images, err := RenderBoard(previewBoard())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(output, images[0], 0600))
}
