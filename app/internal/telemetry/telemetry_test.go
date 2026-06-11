package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// error classification
// ---------------------------------------------------------------------------

func errResult(text string) *mcp.CallToolResult {
	return mcp.NewToolResultError(text)
}

func TestClassifyError(t *testing.T) {
	ctx := context.Background()

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name   string
		ctx    context.Context
		result *mcp.CallToolResult
		err    error
		want   string
	}{
		// auth_required — the shapes requireAuth/formatAuthError actually
		// produce, plus upstream 401/403 in the checkErr format
		{"requireAuth no creds", ctx, errResult("get positions failed: not authenticated — start by calling prepare_registration with your wallet_address"), nil, "auth_required"},
		{"requireAuth no key", ctx, errResult("account registered (account_id: 0xabc) but orderly key not set — call prepare_orderly_key, then complete_orderly_key"), nil, "auth_required"},
		{"formatAuthError banner", ctx, errResult("AUTHENTICATION REQUIRED\n\nError: x\n\nGUIDANCE: y"), nil, "auth_required"},
		{"upstream 401", ctx, errResult(`get user points failed: get user points: 401 Unauthorized {"message":"Unauthorized","code":401}`), nil, "auth_required"},
		{"upstream 403 frozen", ctx, errResult(`withdraw to wallet failed: master withdraw: 403 Forbidden {"message":"Withdrawals from your Main AI Wallet are currently disabled","error_code":4004}`), nil, "auth_required"},

		// invalid_args — mcp-go Require* literals, our handlers' literals, and
		// backend validation-family rejections (error_code 1000-1999)
		{"missing required arg", ctx, errResult(`required argument "public_key" not found`), nil, "invalid_args"},
		{"wrong arg type", ctx, errResult(`argument "shares" is not a string`), nil, "invalid_args"},
		{"amount validation", ctx, errResult("amount is required and must be > 0"), nil, "invalid_args"},
		{"shares validation", ctx, errResult("pass either shares > 0 (in VAULT SHARES, not USDC) or all=true"), nil, "invalid_args"},
		{"sort_by validation", ctx, errResult(`invalid sort_by "bogus" — must be one of "aum", "pnl1h", "pnl24h", "pnl7d", "pnl30d"`), nil, "invalid_args"},
		{"invalid wallet", ctx, errResult("prepare main deposit failed: invalid wallet address: decode: invalid base58 digit"), nil, "invalid_args"},
		{"backend validation 1000", ctx, errResult(`create event: 400 Bad Request {"message":"invalid request data: unknown event type: x","error_code":1000}`), nil, "invalid_args"},
		{"backend 400 no code", ctx, errResult(`users withdraw: 400 Bad Request {"message":"Invalid amount"}`), nil, "invalid_args"},

		// rejected — backend business-rule families (2000+) and Orderly codes
		{"agreement not signed 3000", ctx, errResult(`deposit failed: agents deposit: 400 Bad Request {"message":"agreement not signed","error_code":3000}`), nil, "rejected"},
		{"withdrawal already pending 4002", ctx, errResult(`agents withdraw: 409 Conflict {"message":"withdrawal already pending","error_code":4002}`), nil, "rejected"},
		{"orderly code in body", ctx, errResult(`create order: 400 Bad Request {"code":-1101,"message":"insufficient margin","success":false}`), nil, "rejected"},
		{"orderly APIError shape", ctx, errResult("create order failed: [-1101] insufficient margin"), nil, "rejected"},

		// timeout — Go stdlib/net exact strings + the chat tool's own literal
		{"ctx canceled", canceledCtx, errResult("anything"), nil, "timeout"},
		{"deadline text", ctx, errResult("health check failed: health: context deadline exceeded"), nil, "timeout"},
		{"client timeout", ctx, errResult("Client.Timeout exceeded while awaiting headers"), nil, "timeout"},
		{"chat timed out", ctx, errResult("agent reply timed out — retry or check history"), nil, "timeout"},

		// upstream_error — 5xx/429/404 statuses and transport-level failures
		{"http 500", ctx, errResult("get markets failed: get markets: 500 Internal Server Error"), nil, "upstream_error"},
		{"http 404 dead endpoint", ctx, errResult(`sdk withdraw: 404 Not Found {"message":"Cannot POST /api/sdk/withdraw/solana"}`), nil, "upstream_error"},
		{"http 429", ctx, errResult(`get markets: 429 Too Many Requests {"message":"rate limited"}`), nil, "upstream_error"},
		{"connection refused", ctx, errResult("dial tcp: connection refused"), nil, "upstream_error"},
		{"dns failure", ctx, errResult("dial tcp: lookup app.perptools.ai: no such host"), nil, "upstream_error"},

		// internal — anything unrecognized
		{"unknown text", ctx, errResult("something odd happened"), nil, "internal"},
		{"empty result", ctx, &mcp.CallToolResult{IsError: true}, nil, "internal"},
		{"nil result nil err", ctx, nil, nil, "internal"},

		// Go error (handler returned err != nil) takes precedence over result text
		{"go error wins", ctx, errResult("ignored"), errors.New("not authenticated — start by calling prepare_registration"), "auth_required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyError(tt.ctx, tt.result, tt.err); got != tt.want {
				t.Fatalf("ClassifyError() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// public_key resolution
// ---------------------------------------------------------------------------

type fakeAuth struct{ wallet string }

func (f fakeAuth) AuthenticatedWallet() string { return f.wallet }

func reqWithArgs(args map[string]any) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Name = "test_tool"
	req.Params.Arguments = args
	return req
}

func TestResolvePublicKey(t *testing.T) {
	const validWallet = "4Nd1mYvM6kqtcDpZAdYjbaB1WsWXuBKn1Tt1cs1oZ6Zo" // valid base58, 32 bytes
	const authedWallet = "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM"

	tests := []struct {
		name    string
		auth    AuthState
		args    map[string]any
		wantKey string
	}{
		{"authenticated wins", fakeAuth{authedWallet}, map[string]any{"wallet_address": validWallet}, authedWallet},
		{"valid arg fallback", fakeAuth{""}, map[string]any{"wallet_address": validWallet}, validWallet},
		{"invalid arg -> sentinel", fakeAuth{""}, map[string]any{"wallet_address": "not-a-pubkey-0OIl"}, SentinelPublicKey},
		{"non-string arg -> sentinel", fakeAuth{""}, map[string]any{"wallet_address": 42}, SentinelPublicKey},
		{"no arg -> sentinel", fakeAuth{""}, nil, SentinelPublicKey},
		{"nil auth -> sentinel", nil, nil, SentinelPublicKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if key := ResolvePublicKey(tt.auth, reqWithArgs(tt.args)); key != tt.wantKey {
				t.Fatalf("ResolvePublicKey() = %q, want %q", key, tt.wantKey)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// emitter: overflow drop, close drain, disabled mode
// ---------------------------------------------------------------------------

func TestEmitterOverflowDrops(t *testing.T) {
	block := make(chan struct{})
	started := make(chan struct{}, 1)
	send := func(ctx context.Context, ev Event) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-block // hold the drainer so the buffer fills
		return nil
	}
	e := newEmitter(Config{}, send, 2)

	e.Emit(Event{}) // taken by the drainer (blocks in send)
	<-started
	e.Emit(Event{}) // buffer slot 1
	e.Emit(Event{}) // buffer slot 2
	e.Emit(Event{}) // overflow -> dropped
	e.Emit(Event{}) // overflow -> dropped

	if got := e.Dropped(); got != 2 {
		t.Fatalf("Dropped() = %d, want 2", got)
	}
	close(block)
	e.Close()
}

func TestEmitterCloseDrainsAndRejects(t *testing.T) {
	var mu sync.Mutex
	var sent int
	send := func(ctx context.Context, ev Event) error {
		mu.Lock()
		sent++
		mu.Unlock()
		return nil
	}
	e := newEmitter(Config{}, send, 8)
	for i := 0; i < 5; i++ {
		e.Emit(Event{})
	}
	e.Close()

	mu.Lock()
	got := sent
	mu.Unlock()
	if got != 5 {
		t.Fatalf("sent = %d, want 5 (Close must drain the queue)", got)
	}

	e.Emit(Event{}) // after Close: dropped, must not panic
	if e.Dropped() == 0 {
		t.Fatal("Emit after Close must count as dropped")
	}
	e.Close() // double Close must be safe
}

func TestEmitterSendFailuresAreSwallowed(t *testing.T) {
	send := func(ctx context.Context, ev Event) error { return errors.New("400 unknown event type") }
	e := newEmitter(Config{}, send, 8)
	for i := 0; i < 3; i++ {
		e.Emit(Event{})
	}
	e.Close() // must not hang or panic despite every send failing
	if e.failures.Load() != 3 {
		t.Fatalf("failures = %d, want 3", e.failures.Load())
	}
}

func TestEmitterDisabled(t *testing.T) {
	e := NewEmitter(Config{Disabled: true})
	e.Emit(Event{}) // no goroutine, no channel — must be a safe no-op
	if e.Dropped() != 1 {
		t.Fatalf("disabled emitter should count emits as dropped, got %d", e.Dropped())
	}
	e.Close() // must not hang (done channel is nil — guarded by closed flag)
}

// ---------------------------------------------------------------------------
// middleware: result passthrough + event contents
// ---------------------------------------------------------------------------

func TestToolMiddlewareEmitsAndPassesThrough(t *testing.T) {
	var mu sync.Mutex
	var events []Event
	send := func(ctx context.Context, ev Event) error {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
		return nil
	}
	e := newEmitter(Config{}, send, 8)

	wantResult := mcp.NewToolResultText("ok")
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		time.Sleep(5 * time.Millisecond)
		return wantResult, nil
	}
	wrapped := ToolMiddleware(e, fakeAuth{"9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM"})(handler)

	got, err := wrapped(context.Background(), reqWithArgs(nil))
	if err != nil || got != wantResult {
		t.Fatalf("middleware altered handler output: (%v, %v)", got, err)
	}
	e.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	d := ev.Data
	if ev.PublicKey != "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM" {
		t.Fatalf("bad attribution: key=%q", ev.PublicKey)
	}
	if d.Tool != "test_tool" || !d.Success || d.ErrorCategory != "" {
		t.Fatalf("bad event data: %+v", d)
	}
	if d.DurationMS < 5 {
		t.Fatalf("duration_ms = %d, want >= 5", d.DurationMS)
	}
}

func TestToolMiddlewareFailureEvent(t *testing.T) {
	var mu sync.Mutex
	var events []Event
	send := func(ctx context.Context, ev Event) error {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
		return nil
	}
	e := newEmitter(Config{}, send, 8)

	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultError("get positions failed: not authenticated — start by calling prepare_registration with your wallet_address"), nil
	}
	wrapped := ToolMiddleware(e, fakeAuth{""})(handler)

	res, err := wrapped(context.Background(), reqWithArgs(nil))
	if err != nil || !res.IsError {
		t.Fatalf("middleware altered handler output: (%v, %v)", res, err)
	}
	e.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	d := events[0].Data
	if d.Success || d.ErrorCategory != "auth_required" {
		t.Fatalf("bad failure event: %+v", d)
	}
	if events[0].PublicKey != SentinelPublicKey {
		t.Fatalf("public_key = %q, want sentinel", events[0].PublicKey)
	}
}
