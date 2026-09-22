package qqbot

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Board contains only explicitly shareable presentation values. Never pass
// account credentials, raw API errors or private usage volumes to a renderer.
type Board struct {
	Title, Subtitle string
	Through         time.Time
	Stale           bool
	Cards           []Card
}
type Card struct {
	Platform, Name, Model, State string
	Cache, Availability, TTFT    string
	AvailabilityState, TTFTState string
	History                      []string
}

const cardsPerPage = 12
const maxBoardPages = 4 // QQ allows up to five passive replies to one group message.

var boardFont struct {
	sync.Once
	font *opentype.Font
	err  error
}

func loadBoardFont() (*opentype.Font, error) {
	boardFont.Do(func() {
		paths := []string{os.Getenv("QQ_BOT_FONT_PATH"), "/usr/share/fonts/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc", "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc", "/System/Library/Fonts/Supplemental/Arial Unicode.ttf"}
		for _, pattern := range []string{"/usr/share/fonts/*/NotoSansCJK*.ttc", "/usr/share/fonts/*/NotoSansCJK*.otf"} {
			matches, _ := filepath.Glob(pattern)
			paths = append(paths, matches...)
		}
		for _, path := range paths {
			if path == "" {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			collection, err := opentype.ParseCollection(data)
			if err != nil {
				continue
			}
			for i := 0; i < collection.NumFonts(); i++ {
				f, err := collection.Font(i)
				if err != nil {
					continue
				}
				glyph, err := f.GlyphIndex(nil, '渠')
				if err == nil && glyph != 0 {
					boardFont.font = f
					return
				}
			}
		}
		boardFont.err = errors.New("QQ board Chinese font unavailable; install font-noto-cjk or set QQ_BOT_FONT_PATH")
	})
	return boardFont.font, boardFont.err
}

func RenderBoard(board Board) ([][]byte, error) {
	f, err := loadBoardFont()
	if err != nil {
		return nil, err
	}
	return renderBoard(board, f)
}

func renderBoard(board Board, f *opentype.Font) ([][]byte, error) {
	cards := append([]Card(nil), board.Cards...)
	sort.SliceStable(cards, func(i, j int) bool { return cards[i].Platform < cards[j].Platform })
	total := len(cards)
	if len(cards) > cardsPerPage*maxBoardPages {
		cards = cards[:cardsPerPage*maxBoardPages]
	}
	pages := (len(cards) + cardsPerPage - 1) / cardsPerPage
	if pages == 0 {
		pages = 1
	}
	var images [][]byte
	for page := 0; page < pages; page++ {
		start := page * cardsPerPage
		end := min(start+cardsPerPage, len(cards))
		data, err := renderBoardPage(board, cards[start:end], f, page+1, pages, total, len(cards))
		if err != nil {
			return nil, err
		}
		images = append(images, data)
	}
	return images, nil
}

var (
	boardBG    = color.RGBA{245, 247, 252, 255}
	boardInk   = color.RGBA{31, 41, 55, 255}
	boardMuted = color.RGBA{129, 145, 169, 255}
	boardLine  = color.RGBA{224, 231, 241, 255}
)

func stateColor(state string) color.RGBA {
	switch state {
	case "healthy", "operational":
		return color.RGBA{13, 175, 127, 255}
	case "warning", "degraded":
		return color.RGBA{233, 157, 8, 255}
	case "critical", "failed", "error":
		return color.RGBA{240, 78, 91, 255}
	default:
		return color.RGBA{166, 180, 199, 255}
	}
}
func stateName(state string) string {
	switch state {
	case "healthy", "operational":
		return "健康"
	case "warning", "degraded":
		return "需关注"
	case "critical", "failed", "error":
		return "异常"
	default:
		return "未知"
	}
}
func platformName(p string) string {
	names := map[string]string{"openai": "OpenAI", "anthropic": "Anthropic", "grok": "Grok", "gemini": "Gemini", "antigravity": "Antigravity", "deepseek": "DeepSeek", "kimi": "Kimi"}
	if s := names[p]; s != "" {
		return s
	}
	return p
}
func providerColor(p string) color.RGBA {
	switch p {
	case "anthropic":
		return color.RGBA{173, 109, 74, 255}
	case "gemini", "antigravity":
		return color.RGBA{94, 114, 208, 255}
	case "grok":
		return boardInk
	default:
		return color.RGBA{27, 156, 124, 255}
	}
}
func tint(c color.RGBA) color.RGBA {
	return color.RGBA{uint8((int(c.R) + 255*9) / 10), uint8((int(c.G) + 255*9) / 10), uint8((int(c.B) + 255*9) / 10), 255}
}

type boardCanvas struct {
	image *image.RGBA
	faces map[int]font.Face
}

func (c *boardCanvas) box(x, y, w, h, r int, col color.RGBA) {
	for py := 0; py < h; py++ {
		inset := 0
		if r > 0 {
			dy := 0.0
			if py < r {
				dy = float64(r-py) - 0.5
			} else if py >= h-r {
				dy = float64(py-(h-r)) + 0.5
			}
			if dy > 0 {
				inset = int(math.Ceil(float64(r) - math.Sqrt(float64(r*r)-dy*dy)))
			}
		}
		draw.Draw(c.image, image.Rect(x+inset, y+py, x+w-inset, y+py+1), image.NewUniform(col), image.Point{}, draw.Src)
	}
}
func (c *boardCanvas) text(x, y, size int, value string, col color.RGBA, maxWidth int) {
	face := c.faces[size]
	value = strings.Join(strings.Fields(value), " ")
	if maxWidth > 0 && font.MeasureString(face, value).Ceil() > maxWidth {
		r := []rune(value)
		for len(r) > 0 && font.MeasureString(face, string(r)+"…").Ceil() > maxWidth {
			r = r[:len(r)-1]
		}
		value = string(r) + "…"
	}
	d := font.Drawer{Dst: c.image, Src: image.NewUniform(col), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(value)
}

func renderBoardPage(board Board, cards []Card, f *opentype.Font, page, pages, total, shown int) ([]byte, error) {
	const width = 1200
	height := 214
	for i := 0; i < len(cards); {
		j := i
		for j < len(cards) && cards[j].Platform == cards[i].Platform {
			j++
		}
		height += 62 + ((j-i+1)/2)*344
		i = j
	}
	if len(cards) == 0 {
		height += 180
	}
	height += 84
	c := &boardCanvas{image: image.NewRGBA(image.Rect(0, 0, width, height)), faces: map[int]font.Face{}}
	for _, size := range []int{14, 16, 18, 20, 22, 24, 28, 30, 36} {
		face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return nil, err
		}
		c.faces[size] = face
		defer face.Close()
	}
	draw.Draw(c.image, c.image.Bounds(), image.NewUniform(boardBG), image.Point{}, draw.Src)
	grid := color.RGBA{237, 241, 248, 255}
	for x := 0; x < width; x += 48 {
		c.box(x, 0, 1, height, 0, grid)
	}
	for y := 0; y < height; y += 48 {
		c.box(0, y, width, 1, 0, grid)
	}
	c.text(36, 58, 30, board.Title+" · 渠道状态", boardInk, 1000)
	c.text(36, 94, 18, board.Subtitle, boardMuted, 1100)
	healthy, warnings := 0, 0
	for _, card := range board.Cards {
		if card.State == "healthy" || card.State == "operational" {
			healthy++
		} else if card.State != "unknown" && card.State != "" {
			warnings++
		}
	}
	chips := []string{fmt.Sprintf("%d 个分组", total), fmt.Sprintf("健康 %d", healthy), fmt.Sprintf("关注 / 异常 %d", warnings), fmt.Sprintf("第 %d / %d 页", page, pages)}
	x := 36
	for _, chip := range chips {
		w := font.MeasureString(c.faces[18], chip).Ceil() + 32
		c.box(x, 114, w, 38, 10, color.RGBA{255, 255, 255, 255})
		c.text(x+16, 140, 18, chip, boardInk, w-24)
		x += w + 12
	}
	stamp := "暂无数据时间"
	if !board.Through.IsZero() {
		stamp = "数据截至 " + board.Through.In(time.FixedZone("UTC+8", 8*3600)).Format("2006/01/02 15:04:05") + "  北京时间"
	}
	if board.Stale {
		stamp += "  · 数据过期或缺失，无法判断当前状态"
	}
	c.text(36, 184, 16, stamp, boardMuted, 1120)
	y := 214
	for i := 0; i < len(cards); {
		j := i
		for j < len(cards) && cards[j].Platform == cards[i].Platform {
			j++
		}
		provider := providerColor(cards[i].Platform)
		name := platformName(cards[i].Platform)
		c.box(36, y, 38, 38, 10, tint(provider))
		initial := "?"
		if name != "" {
			initial = strings.ToUpper(string([]rune(name)[0]))
		}
		c.text(48, y+27, 22, initial, provider, 25)
		c.text(88, y+28, 24, name, boardInk, 800)
		c.text(1000, y+26, 16, fmt.Sprintf("%d 个分组", j-i), boardMuted, 164)
		y += 58
		for k := i; k < j; k++ {
			cx := 36 + ((k-i)%2)*576
			cy := y + ((k-i)/2)*344
			c.card(cx, cy, cards[k], board.Stale)
		}
		y += ((j-i+1)/2)*344 + 4
		i = j
	}
	if len(cards) == 0 {
		c.box(36, y, 1128, 150, 16, color.RGBA{255, 255, 255, 255})
		c.text(72, y+70, 24, "暂无匹配数据", boardInk, 1000)
		c.text(72, y+108, 18, "空白和灰色状态代表未知，不代表渠道正常。", boardMuted, 1000)
	}
	foot := "仅展示管理员允许公开的分组 · 状态条：绿=健康 / 黄=需关注 / 红=异常 / 灰=无数据"
	if total > shown {
		foot = fmt.Sprintf("共 %d 个分组，显示前 %d 个；请加平台或分组名称缩小查询范围。", total, shown)
	}
	c.text(36, height-36, 16, foot, boardMuted, 1128)
	var out bytes.Buffer
	if err := png.Encode(&out, c.image); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (c *boardCanvas) card(x, y int, card Card, stale bool) {
	c.box(x, y+3, 552, 320, 16, color.RGBA{231, 235, 244, 255})
	c.box(x, y, 552, 318, 16, color.RGBA{255, 255, 255, 255})
	pc := providerColor(card.Platform)
	c.box(x+24, y+27, 6, 43, 3, pc)
	c.text(x+43, y+47, 22, card.Name, boardInk, 365)
	sub := platformName(card.Platform)
	if card.Model != "" {
		sub += " / " + card.Model
	}
	c.text(x+43, y+77, 16, sub, pc, 460)
	state := card.State
	label := stateName(state)
	if stale {
		label = "已过期"
		state = "unknown"
	}
	sc := stateColor(state)
	c.box(x+450, y+24, 78, 28, 12, tint(sc))
	c.text(x+461, y+44, 14, label, sc, 62)
	values := []string{card.Cache, card.Availability, card.TTFT}
	labels := []string{"缓存率", "可用率", "首 Token"}
	states := []string{"", card.AvailabilityState, card.TTFTState}
	for i := 0; i < 3; i++ {
		mx := x + 24 + i*172
		c.box(mx, y+111, 160, 86, 10, boardBG)
		c.text(mx+13, y+139, 16, labels[i], boardMuted, 135)
		col := boardInk
		if i > 0 {
			col = stateColor(states[i])
		}
		v := values[i]
		if v == "" {
			v = "-"
		}
		if v == "-" {
			col = boardMuted
		}
		c.text(mx+13, y+177, 28, v, col, 138)
	}
	c.box(x+24, y+217, 504, 1, 0, boardLine)
	c.text(x+24, y+246, 14, fmt.Sprintf("近 %d 个时段", len(card.History)), boardMuted, 200)
	count := len(card.History)
	if count == 0 {
		count = 18
	}
	step := float64(504) / float64(count)
	for i := 0; i < count; i++ {
		state := "unknown"
		if i < len(card.History) {
			state = card.History[i]
		}
		h := 5
		switch state {
		case "healthy", "operational":
			h = 24
		case "warning", "degraded":
			h = 13
		case "critical", "failed", "error":
			h = 5
		}
		w := int(step) - 3
		if w < 1 {
			w = 1
		}
		c.box(x+24+int(float64(i)*step), y+283-h, w, h, 2, stateColor(state))
	}
	c.text(x+24, y+303, 14, "PAST", boardMuted, 150)
	c.text(x+488, y+303, 14, "NOW", boardMuted, 45)
}
