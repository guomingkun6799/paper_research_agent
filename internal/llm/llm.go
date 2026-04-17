package llm

import "context"

type Client interface {
	Chat(ctx context.Context, system string, user string) (string, error)
}

type FallbackClient struct{}

func (FallbackClient) Chat(ctx context.Context, system string, user string) (string, error) {
	_ = ctx
	_ = system
	return user, nil
}
