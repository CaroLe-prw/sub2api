package repository

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/wenlng/go-captcha-assets/resources/images"
	"github.com/wenlng/go-captcha-assets/resources/tiles"
	"github.com/wenlng/go-captcha/v2/slide"
)

type goCaptchaGenerator struct {
	once    sync.Once
	captcha slide.Captcha
	err     error
	// Bound CPU-heavy generation; never accumulate an unbounded request queue.
	slots chan struct{}
}

func NewGoCaptchaGenerator() service.GoCaptchaGenerator {
	return &goCaptchaGenerator{slots: make(chan struct{}, 2)}
}

func (g *goCaptchaGenerator) initialize() {
	backgrounds, err := images.GetImages()
	if err != nil {
		g.err = err
		return
	}
	assets, err := tiles.GetTiles()
	if err != nil {
		g.err = err
		return
	}
	graphs := make([]*slide.GraphImage, 0, len(assets))
	for _, asset := range assets {
		graphs = append(graphs, &slide.GraphImage{OverlayImage: asset.OverlayImage, ShadowImage: asset.ShadowImage, MaskImage: asset.MaskImage})
	}
	builder := slide.NewBuilder()
	builder.SetResources(slide.WithBackgrounds(backgrounds), slide.WithGraphImages(graphs))
	g.captcha = builder.Make()
}

func (g *goCaptchaGenerator) Generate(ctx context.Context) (*service.GoCaptchaChallenge, service.GoCaptchaAnswer, error) {
	var answer service.GoCaptchaAnswer
	if err := ctx.Err(); err != nil {
		return nil, answer, err
	}
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	default:
		return nil, answer, service.ErrServiceUnavailable
	}
	g.once.Do(g.initialize)
	if g.err != nil {
		return nil, answer, g.err
	}
	data, err := g.captcha.Generate()
	if err != nil {
		return nil, answer, err
	}
	master, err := data.GetMasterImage().ToBase64()
	if err != nil {
		return nil, answer, err
	}
	thumb, err := data.GetTileImage().ToBase64()
	if err != nil {
		return nil, answer, err
	}
	block := data.GetData()
	return &service.GoCaptchaChallenge{
		Image: master, Thumb: thumb, Width: 300, Height: 220,
		ThumbWidth: block.Width, ThumbHeight: block.Height,
		ThumbX: block.DX, ThumbY: block.DY,
	}, service.GoCaptchaAnswer{X: block.X, Y: block.Y}, nil
}

type goCaptchaStore struct{ client *redis.Client }

func NewGoCaptchaStore(client *redis.Client) service.GoCaptchaStore {
	return &goCaptchaStore{client: client}
}

func (s *goCaptchaStore) Put(ctx context.Context, key, value string, ttl time.Duration) error {
	return s.client.Set(ctx, "go_captcha:"+key, value, ttl).Err()
}

var takeGoCaptchaValue = redis.NewScript(`
local value = redis.call('GET', KEYS[1])
if value then redis.call('DEL', KEYS[1]) end
return value
`)

func (s *goCaptchaStore) Take(ctx context.Context, key string) (string, error) {
	value, err := takeGoCaptchaValue.Run(ctx, s.client, []string{"go_captcha:" + key}).Text()
	if err == redis.Nil {
		return "", nil
	}
	return value, err
}
