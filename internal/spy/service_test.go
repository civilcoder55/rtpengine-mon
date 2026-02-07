package spy

import (
	"context"
	"testing"
)

type mockRTPEngineClient struct {
	queryResult map[string]interface{}
	queryErr    error
}

func (m *mockRTPEngineClient) ListCalls(ctx context.Context) ([]string, error) { return nil, nil }
func (m *mockRTPEngineClient) QueryCall(ctx context.Context, callID string) (map[string]interface{}, error) {
	return m.queryResult, m.queryErr
}
func (m *mockRTPEngineClient) Subscribe(ctx context.Context, callID, tag string) (map[string]interface{}, error) {
	return nil, nil
}
func (m *mockRTPEngineClient) SubscribeAnswer(ctx context.Context, callID, sdp, toTag string) (map[string]interface{}, error) {
	return nil, nil
}
func (m *mockRTPEngineClient) UnSubscribe(ctx context.Context, callID, toTag string) (map[string]interface{}, error) {
	return nil, nil
}
func (m *mockRTPEngineClient) Statistics(ctx context.Context) (map[string]interface{}, error) {
	return nil, nil
}
func (m *mockRTPEngineClient) Close() error { return nil }

func TestDetectTags(t *testing.T) {
	tests := []struct {
		name         string
		queryResult  map[string]interface{}
		expectedFrom string
		expectedTo   string
		expectErr    bool
	}{
		{
			name: "normal tags",
			queryResult: map[string]interface{}{
				"tags": map[string]interface{}{
					"tag-caller": map[string]interface{}{"created": int64(1000)},
					"tag-callee": map[string]interface{}{"created": int64(2000)},
				},
			},
			expectedFrom: "tag-caller",
			expectedTo:   "tag-callee",
			expectErr:    false,
		},
		{
			name: "swapped order in map",
			queryResult: map[string]interface{}{
				"tags": map[string]interface{}{
					"tag-callee": map[string]interface{}{"created": int64(2000)},
					"tag-caller": map[string]interface{}{"created": int64(1000)},
				},
			},
			expectedFrom: "tag-caller",
			expectedTo:   "tag-callee",
			expectErr:    false,
		},
		{
			name: "insufficient tags",
			queryResult: map[string]interface{}{
				"tags": map[string]interface{}{
					"tag1": map[string]interface{}{"created": int64(1000)},
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Service{
				rtpClient: &mockRTPEngineClient{queryResult: tt.queryResult},
			}
			from, to, err := s.detectTags(context.Background(), "call-id")
			if (err != nil) != tt.expectErr {
				t.Fatalf("detectTags() error = %v, expectErr %v", err, tt.expectErr)
			}
			if !tt.expectErr {
				if from != tt.expectedFrom || to != tt.expectedTo {
					t.Errorf("expected from=%s, to=%s; got from=%s, to=%s", tt.expectedFrom, tt.expectedTo, from, to)
				}
			}
		})
	}
}
