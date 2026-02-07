package rtpengine

import "context"

type Client interface {
	ListCalls(ctx context.Context) ([]string, error)
	QueryCall(ctx context.Context, callID string) (map[string]interface{}, error)
	Subscribe(ctx context.Context, callID, tag string) (map[string]interface{}, error)
	SubscribeAnswer(ctx context.Context, callID, sdp, toTag string) (map[string]interface{}, error)
	UnSubscribe(ctx context.Context, callID, toTag string) (map[string]interface{}, error)
	Statistics(ctx context.Context) (map[string]interface{}, error)
	Close() error
}
