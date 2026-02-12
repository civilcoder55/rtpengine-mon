package rtpengine

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jackpal/bencode-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type client struct {
	addr   *net.UDPAddr
	conn   *net.UDPConn
	mu     sync.Mutex
	tracer trace.Tracer
	meter  metric.Meter

	requestCounter metric.Int64Counter
	errorCounter   metric.Int64Counter
}

// NewClient creates a new RTPEngine client for the given address.
func NewClient(address string) (Client, error) {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve udp address: %w", err)
	}

	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to listen udp: %w", err)
	}

	tracer := otel.Tracer("rtpengine-client")
	meter := otel.Meter("rtpengine-client")

	reqCounter, _ := meter.Int64Counter("rtpengine.requests_total", metric.WithDescription("Total number of requests to RTPEngine"))
	errCounter, _ := meter.Int64Counter("rtpengine.errors_total", metric.WithDescription("Total number of errors from RTPEngine"))

	return &client{
		addr:           addr,
		conn:           conn,
		tracer:         tracer,
		meter:          meter,
		requestCounter: reqCounter,
		errorCounter:   errCounter,
	}, nil
}

func (c *client) generateCookie() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func (c *client) sendCommand(ctx context.Context, command string, args map[string]interface{}) (map[string]interface{}, error) {
	ctx, span := c.tracer.Start(ctx, "rtpengine.SendCommand", trace.WithSpanKind(trace.SpanKindClient),trace.WithAttributes(
		attribute.String("command", command),
	))
	defer span.End()

	cookie := c.generateCookie()
	args["command"] = command

	c.requestCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("command", command)))

	var buf bytes.Buffer
	buf.WriteString(cookie + " ")
	if err := bencode.Marshal(&buf, args); err != nil {
		return nil, fmt.Errorf("failed to marshal bencode: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.conn.WriteToUDP(buf.Bytes(), c.addr); err != nil {
		c.errorCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("command", command), attribute.String("reason", "write_error")))
		return nil, fmt.Errorf("failed to write to udp: %w", err)
	}

	c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	respBuf := make([]byte, 65535)
	n, _, err := c.conn.ReadFromUDP(respBuf)
	if err != nil {
		c.errorCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("command", command), attribute.String("reason", "read_error")))
		return nil, fmt.Errorf("failed to read from udp: %w", err)
	}

	spaceIdx := bytes.IndexByte(respBuf[:n], ' ')
	if spaceIdx == -1 {
		return nil, fmt.Errorf("invalid response format (no space)")
	}

	decoded, err := bencode.Decode(bytes.NewReader(respBuf[spaceIdx+1 : n]))
	if err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}

	resp, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("decoded response is not a map: %T", decoded)
	}

	if result, ok := resp["result"].(string); ok && result == "error" {
		c.errorCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("command", command), attribute.String("reason", "rtpengine_error")))
		return nil, fmt.Errorf("rtpengine error: %v", resp["error-reason"])
	}

	return resp, nil
}

func (c *client) ListCalls(ctx context.Context) ([]string, error) {
	resp, err := c.sendCommand(ctx, "list", map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	rawCalls, ok := resp["calls"].([]interface{})
	if !ok {
		if calls, ok := resp["calls"].([]string); ok {
			return calls, nil
		}
		return []string{}, nil
	}

	calls := make([]string, len(rawCalls))
	for i, v := range rawCalls {
		calls[i] = fmt.Sprint(v)
	}
	return calls, nil
}

func (c *client) QueryCall(ctx context.Context, callID string) (map[string]interface{}, error) {
	args := map[string]interface{}{
		"call-id": callID,
	}
	return c.sendCommand(ctx, "query", args)
}

func (c *client) Subscribe(ctx context.Context, callID, tag string) (map[string]interface{}, error) {
	args := map[string]interface{}{
		"call-id":  callID,
		"from-tag": tag,
		"flags":    []string{"trust-address", "generate-mid", "SDES-off", "no-rtcp-attribute", "trickle-ICE"},
		"rtcp-mux": []string{"offer", "require"},
		"transport-protocol": "UDP/TLS/RTP/SAVPF",
		"ICE": "force",
		"codec": map[string]interface{}{
			"strip": "all",
			"transcode": "PCMU",
		},
	}
	return c.sendCommand(ctx, "subscribe request", args)
}

func (c *client) SubscribeAnswer(ctx context.Context, callID, sdp, toTag string) (map[string]interface{}, error) {
	args := map[string]interface{}{
		"call-id":  callID,
		"sdp":      sdp,
		"to-tag":   toTag,
	}
	return c.sendCommand(ctx, "subscribe answer", args)
}

func (c *client) UnSubscribe(ctx context.Context, callID, toTag string) (map[string]interface{}, error) {
	args := map[string]interface{}{
		"call-id":  callID,
		"from-tag": toTag,
		"to-tag":   toTag,
	}
	return c.sendCommand(ctx, "unsubscribe", args)
}

func (c *client) Statistics(ctx context.Context) (map[string]interface{}, error) {
	return c.sendCommand(ctx, "statistics", map[string]interface{}{})
}

func (c *client) Close() error {
	return c.conn.Close()
}
