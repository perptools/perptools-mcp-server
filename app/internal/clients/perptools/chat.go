package perptools

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// ---------------------------------------------------------------------------
// Agent chat — short-lived websocket sessions to the backend's agent-chat
// relay (which proxies Envy's /ws/terminal). Two operations:
//   - SendAgentMessage: open a session, send one user message, hold the
//     socket until the agent's reply quiesces (the relay only persists the
//     reply while the session is open), return the concatenated reply.
//   - GetAgentChatHistory: open a session, capture the initial_history
//     batch (newest 10) or page further back with load_more.
//
// Endpoint: wss://app.perptools.ai/api/v1/envy/ws (legacy alias, upgrade is
// signature-exempt) with canonical /v1/ai/ws (orderly-signed upgrade) as
// fallback. Both require a first-frame auth message within 10s of upgrade,
// signed over: timestamp + "GET" + basePath + "?" + sortedQuery.
// ---------------------------------------------------------------------------

const (
	chatDefaultTimeout = 60 * time.Second
	chatMaxTimeout     = 120 * time.Second
	// chatQuiescence is how long the socket stays open after the last output
	// frame before the reply is considered complete (replies can span
	// multiple frames).
	chatQuiescence = 3 * time.Second
	// chatGreetingDrain is how long greeting/replayed frames are drained
	// after auth before the user message is sent. Envy replays a
	// non-conversational greeting "output" on every connect.
	chatGreetingDrain = 2 * time.Second
	// chatAuthWait bounds the wait for auth_success per endpoint candidate.
	chatAuthWait = 12 * time.Second
	// chatReadLimit allows large history batches (default lib limit is 32KB).
	chatReadLimit = 1 << 22
)

// ChatMessage is one persisted chat message (ours or the agent's).
type ChatMessage struct {
	MessageID string `json:"message_id"`
	Text      string `json:"text"`
	IsFromBot bool   `json:"is_from_bot"`
	Timestamp string `json:"timestamp,omitempty"`
}

// ChatHistory is a page of persisted chat messages.
type ChatHistory struct {
	Messages []ChatMessage `json:"messages"`
	// Cursor is the pagination cursor (a message id) to pass as `before` to
	// fetch older messages. Empty when the server reported none.
	Cursor  string `json:"cursor,omitempty"`
	HasMore bool   `json:"has_more"`
}

// ChatReply is the outcome of one send-and-wait chat exchange.
type ChatReply struct {
	// Reply is the agent's reply text — possibly multiple output frames
	// joined in arrival order. Empty when TimedOut.
	Reply string `json:"reply"`
	// EchoedCommand is the server's echo of the message we sent (confirms
	// the agent received it), when observed.
	EchoedCommand string `json:"echoed_command,omitempty"`
	// TimedOut is true when no reply arrived within the timeout.
	TimedOut bool `json:"timed_out"`
	// FramesReceived counts every server frame seen during the session.
	FramesReceived int `json:"frames_received"`
}

// wsFrame is the relay's frame envelope (both directions).
type wsFrame struct {
	Type   string          `json:"type"`
	BotID  string          `json:"botId,omitempty"`
	Source string          `json:"source,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// wireChatMessage is a persisted message as the relay encodes it.
type wireChatMessage struct {
	MessageID string          `json:"messageId"`
	Text      string          `json:"text"`
	IsFromBot bool            `json:"isFromBot"`
	Timestamp json.RawMessage `json:"timestamp"`
}

func (m wireChatMessage) toChatMessage() ChatMessage {
	return ChatMessage{
		MessageID: m.MessageID,
		Text:      m.Text,
		IsFromBot: m.IsFromBot,
		Timestamp: strings.Trim(string(m.Timestamp), `"`),
	}
}

type wireHistoryData struct {
	Messages []wireChatMessage `json:"messages"`
	Cursor   string            `json:"cursor"`
	HasMore  bool              `json:"hasMore"`
}

type wireOutputData struct {
	Text      string          `json:"text"`
	Timestamp json.RawMessage `json:"timestamp"`
}

type wireEchoData struct {
	Command string `json:"command"`
}

// chatFrameLoggerKey carries an optional diagnostics hook through the
// context: every raw frame sent ("->") or received ("<-") is reported to it.
// Used by integration tests to probe the wire protocol.
type chatFrameLoggerKey struct{}

// WithChatFrameLogger returns a context that mirrors all raw agent-chat
// frames to fn (direction is "->" for sent, "<-" for received).
func WithChatFrameLogger(ctx context.Context, fn func(direction, frame string)) context.Context {
	return context.WithValue(ctx, chatFrameLoggerKey{}, fn)
}

func chatFrameLog(ctx context.Context, direction, frame string) {
	if fn, ok := ctx.Value(chatFrameLoggerKey{}).(func(string, string)); ok {
		fn(direction, frame)
	}
}

// chatEndpoint is one dial/sign candidate. The legacy /v1/envy/ws alias is
// signature-exempt on the upgrade itself; the canonical /v1/ai/ws needs
// orderly headers on the upgrade request. Both need the first-frame auth.
type chatEndpoint struct {
	dialPath   string // appended to the ws base URL (which includes /api)
	signedBase string // base path used in the signature payloads
	upgradeHdr bool   // sign the upgrade request with orderly headers
}

var chatEndpoints = []chatEndpoint{
	{dialPath: "/v1/envy/ws", signedBase: "/v1/envy/ws", upgradeHdr: false},
	{dialPath: "/v1/envy/ws", signedBase: "/api/v1/envy/ws", upgradeHdr: false},
	{dialPath: "/v1/ai/ws", signedBase: "/v1/ai/ws", upgradeHdr: true},
	{dialPath: "/v1/ai/ws", signedBase: "/api/v1/ai/ws", upgradeHdr: true},
}

// errChatWait signals that readFrame's wait elapsed with no frame — the
// session itself is still alive (unlike a read error, which is terminal).
var errChatWait = fmt.Errorf("agent chat: wait elapsed with no frame")

// chatSession is a live, authenticated websocket session to the relay.
//
// A single reader goroutine owns conn.Read for the whole session and feeds
// framesCh. This is deliberate: with coder/websocket, cancelling a Read
// context CLOSES the entire connection, so per-read timeouts cannot be
// expressed as read deadlines — they are selects against framesCh instead.
type chatSession struct {
	conn   *websocket.Conn
	cancel context.CancelFunc
	// endpoint records which dial/sign variant succeeded (diagnostics).
	endpoint string
	// initialHistory is the newest-10 batch the relay pushes on connect,
	// when it arrived during the auth handshake.
	initialHistory *wireHistoryData

	framesCh chan *wsFrame
	readErr  error // set by the reader before framesCh is closed
	frames   int   // counted by the consumer (readFrame)
}

func newChatSession(ctx context.Context, conn *websocket.Conn, endpoint string) *chatSession {
	sctx, cancel := context.WithCancel(ctx)
	s := &chatSession{
		conn:     conn,
		cancel:   cancel,
		endpoint: endpoint,
		framesCh: make(chan *wsFrame, 64),
	}
	go s.readLoop(sctx)
	return s
}

// readLoop is the sole reader of the connection; it exits (closing framesCh)
// when the peer closes, the session context is cancelled, or close() runs.
func (s *chatSession) readLoop(ctx context.Context) {
	defer close(s.framesCh)
	for {
		_, data, err := s.conn.Read(ctx)
		if err != nil {
			s.readErr = err
			return
		}
		chatFrameLog(ctx, "<-", string(data))
		var f wsFrame
		if err := json.Unmarshal(data, &f); err != nil {
			// Tolerate non-JSON frames (e.g. plain-text keepalives) —
			// surface them as an untyped frame so callers can decide.
			f = wsFrame{Type: "raw", Data: json.RawMessage(strconv.Quote(string(data)))}
		}
		select {
		case s.framesCh <- &f:
		case <-ctx.Done():
			s.readErr = ctx.Err()
			return
		}
	}
}

func (s *chatSession) close() {
	_ = s.conn.Close(websocket.StatusNormalClosure, "done")
	s.cancel()
}

// readFrame waits up to `wait` for the next frame. It returns errChatWait
// when the wait elapses quietly (session still usable) or the terminal read
// error when the session is over.
func (s *chatSession) readFrame(ctx context.Context, wait time.Duration) (*wsFrame, error) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case f, ok := <-s.framesCh:
		if !ok {
			if s.readErr != nil {
				return nil, s.readErr
			}
			return nil, fmt.Errorf("agent chat: session closed")
		}
		s.frames++
		return f, nil
	case <-timer.C:
		return nil, errChatWait
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *chatSession) writeJSON(ctx context.Context, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal ws frame: %w", err)
	}
	chatFrameLog(ctx, "->", string(b))
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.conn.Write(wctx, websocket.MessageText, b)
}

// chatQuery builds the canonical query string — url.Values.Encode() sorts
// keys alphabetically (agent_id, path, public_key) and percent-encodes
// values; the first-frame signature must cover this exact rendering.
func chatQuery(publicKey, agentID string) string {
	return url.Values{
		"agent_id":   {agentID},
		"path":       {"/ws/terminal"}, // /ws/chat has no history persistence
		"public_key": {publicKey},
	}.Encode()
}

// signChatPayload signs timestamp + "GET" + path?query (empty body) the same
// way orderlySignMiddleware does for HTTP calls.
func signChatPayload(priv ed25519.PrivateKey, ts, pathWithQuery string) string {
	sig := ed25519.Sign(priv, []byte(ts+"GET"+pathWithQuery))
	return base64.RawURLEncoding.EncodeToString(sig)
}

// wsBaseURL converts the HTTP base URL (e.g. https://app.perptools.ai/api)
// to its websocket form.
func (c *client) wsBaseURL() string {
	b := c.baseURL
	b = strings.Replace(b, "https://", "wss://", 1)
	b = strings.Replace(b, "http://", "ws://", 1)
	return strings.TrimSuffix(b, "/")
}

// dialChat opens and authenticates a relay session, trying each endpoint
// variant in order (legacy signature-exempt alias first). It returns once
// auth_success is observed; any initial_history seen during the handshake is
// retained on the session.
func (c *client) dialChat(ctx context.Context, publicKey, agentID string) (*chatSession, error) {
	if c.orderlyPrivateKey == nil {
		return nil, fmt.Errorf("agent chat requires orderly credentials — authenticate first")
	}
	query := chatQuery(publicKey, agentID)

	// Try each variant; aggregate EVERY failure (not just the last). The last
	// candidate (/v1/ai/ws) tends to 401, so surfacing only it masked the real
	// reason the legacy signature-exempt alias failed — making every problem
	// look like a bare "401". With all four reported, an edge-gate (every
	// variant 401s), a base-URL mismatch (404/dial errors), and a genuine auth
	// rejection are immediately distinguishable.
	errs := make([]string, 0, len(chatEndpoints))
	for _, ep := range chatEndpoints {
		sess, err := c.dialChatEndpoint(ctx, ep, query)
		if err == nil {
			return sess, nil
		}
		errs = append(errs, fmt.Sprintf("[dial %s | signed %s | upgradeHdr=%v] %v",
			ep.dialPath, ep.signedBase, ep.upgradeHdr, err))
	}
	return nil, fmt.Errorf("agent chat: all %d endpoint variants failed (ws base=%s):\n  %s",
		len(chatEndpoints), c.wsBaseURL(), strings.Join(errs, "\n  "))
}

func (c *client) dialChatEndpoint(ctx context.Context, ep chatEndpoint, query string) (*chatSession, error) {
	dialURL := c.wsBaseURL() + ep.dialPath + "?" + query
	opts := &websocket.DialOptions{}
	if ep.upgradeHdr {
		ts := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
		hdr := make(map[string][]string, 4)
		hdr["orderly-timestamp"] = []string{ts}
		hdr["orderly-account-id"] = []string{c.accountID}
		hdr["orderly-key"] = []string{"ed25519:" + c.orderlyPublicKey}
		hdr["orderly-signature"] = []string{signChatPayload(c.orderlyPrivateKey, ts, ep.signedBase+"?"+query)}
		opts.HTTPHeader = hdr
	}

	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dctx, dialURL, opts) //nolint:bodyclose // closed by the lib on success
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	conn.SetReadLimit(chatReadLimit)

	// The reader goroutine lives on the caller's ctx (NOT dctx, which only
	// bounds the dial) and is torn down by sess.close().
	sess := newChatSession(ctx, conn, ep.dialPath+" (signed "+ep.signedBase+")")

	// First-frame auth, required within 10s of the upgrade on both paths.
	ts := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	authFrame := map[string]string{
		"orderly_timestamp":  ts,
		"orderly_account_id": c.accountID,
		"orderly_key":        "ed25519:" + c.orderlyPublicKey,
		"orderly_signature":  signChatPayload(c.orderlyPrivateKey, ts, ep.signedBase+"?"+query),
	}
	if err := sess.writeJSON(ctx, authFrame); err != nil {
		sess.close()
		return nil, fmt.Errorf("send auth frame: %w", err)
	}

	// Wait for auth_success; retain initial_history if it arrives meanwhile.
	deadline := time.Now().Add(chatAuthWait)
	for time.Now().Before(deadline) {
		f, err := sess.readFrame(ctx, time.Until(deadline))
		if err != nil {
			sess.close()
			return nil, fmt.Errorf("await auth_success: %w", err)
		}
		switch f.Type {
		case "auth_success":
			return sess, nil
		case "initial_history":
			var h wireHistoryData
			if err := json.Unmarshal(f.Data, &h); err == nil {
				sess.initialHistory = &h
			}
		case "error":
			sess.close()
			return nil, fmt.Errorf("auth rejected (source=%s): %s", f.Source, string(f.Error))
		}
	}
	sess.close()
	return nil, fmt.Errorf("no auth_success within %s", chatAuthWait)
}

// SendAgentMessage opens a chat session, sends one user message and holds
// the socket open until the agent's reply quiesces (or timeout). The socket
// MUST stay open while waiting: the relay persists the agent's output only
// inside the live session.
func (c *client) SendAgentMessage(ctx context.Context, publicKey, agentID, message string, timeout time.Duration) (*ChatReply, error) {
	if timeout <= 0 {
		timeout = chatDefaultTimeout
	}
	if timeout > chatMaxTimeout {
		timeout = chatMaxTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout+30*time.Second) // dial+auth headroom
	defer cancel()

	sess, err := c.dialChat(ctx, publicKey, agentID)
	if err != nil {
		return nil, err
	}
	defer sess.close()

	// Drain the connect-time replay (initial_history + Envy's greeting
	// output) so it is not mistaken for the reply.
	drainUntil := time.Now().Add(chatGreetingDrain)
	for time.Now().Before(drainUntil) {
		f, err := sess.readFrame(ctx, time.Until(drainUntil))
		if err != nil {
			if errors.Is(err, errChatWait) {
				break // quiet — the expected exit
			}
			return nil, fmt.Errorf("session lost before send: %w", err)
		}
		if f.Type == "error" && f.Source != "" {
			return nil, fmt.Errorf("agent chat error before send (source=%s): %s", f.Source, string(f.Error))
		}
	}

	sendFrame := map[string]any{
		"type": "command",
		"data": map[string]string{"command": message},
	}
	if err := sess.writeJSON(ctx, sendFrame); err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}
	sentAt := time.Now()
	replyDeadline := sentAt.Add(timeout)

	reply := &ChatReply{}
	var outputs []string
	echoSeen := false

	for {
		wait := time.Until(replyDeadline)
		if len(outputs) > 0 {
			// Reply started — close after a short quiet period; replies can
			// span multiple frames.
			if wait > chatQuiescence {
				wait = chatQuiescence
			}
		}
		if wait <= 0 {
			break
		}
		f, err := sess.readFrame(ctx, wait)
		if err != nil {
			break // deadline/quiescence reached, or peer closed
		}
		switch f.Type {
		case "command_echo":
			var e wireEchoData
			_ = json.Unmarshal(f.Data, &e)
			reply.EchoedCommand = e.Command
			if !echoSeen {
				// Outputs before the echo were greeting stragglers.
				outputs = outputs[:0]
				echoSeen = true
			}
		case "output":
			var o wireOutputData
			if err := json.Unmarshal(f.Data, &o); err == nil && o.Text != "" {
				outputs = append(outputs, o.Text)
			}
		case "error":
			reply.FramesReceived = sess.frames
			return reply, fmt.Errorf("agent chat error (source=%s): %s", f.Source, string(f.Error))
		}
	}

	reply.FramesReceived = sess.frames
	reply.Reply = strings.Join(outputs, "\n")
	reply.TimedOut = len(outputs) == 0
	return reply, nil
}

// GetAgentChatHistory returns persisted chat history. With an empty cursor
// it returns the connect-time initial batch (newest 10); with `before` set
// it pages further back via load_more (served from the backend's Postgres).
func (c *client) GetAgentChatHistory(ctx context.Context, publicKey, agentID, before string, limit int) (*ChatHistory, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	sess, err := c.dialChat(ctx, publicKey, agentID)
	if err != nil {
		return nil, err
	}
	defer sess.close()

	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	if before != "" {
		req := map[string]any{
			"type": "load_more",
			"data": map[string]any{"botId": agentID, "before": before, "limit": limit},
		}
		if err := sess.writeJSON(ctx, req); err != nil {
			return nil, fmt.Errorf("send load_more: %w", err)
		}
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			f, err := sess.readFrame(ctx, time.Until(deadline))
			if err != nil {
				return nil, fmt.Errorf("await history_batch: %w", err)
			}
			switch f.Type {
			case "history_batch":
				var h wireHistoryData
				if err := json.Unmarshal(f.Data, &h); err != nil {
					return nil, fmt.Errorf("decode history_batch: %w", err)
				}
				return historyFromWire(&h, false), nil
			case "error":
				return nil, fmt.Errorf("history error (source=%s): %s", f.Source, string(f.Error))
			}
		}
		return nil, fmt.Errorf("no history_batch within 20s")
	}

	// Initial batch: usually already captured during the auth handshake;
	// otherwise wait briefly for it.
	if sess.initialHistory == nil {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			f, err := sess.readFrame(ctx, time.Until(deadline))
			if err != nil {
				return nil, fmt.Errorf("await initial_history: %w", err)
			}
			if f.Type == "initial_history" {
				var h wireHistoryData
				if err := json.Unmarshal(f.Data, &h); err != nil {
					return nil, fmt.Errorf("decode initial_history: %w", err)
				}
				sess.initialHistory = &h
				break
			}
			if f.Type == "error" {
				return nil, fmt.Errorf("history error (source=%s): %s", f.Source, string(f.Error))
			}
		}
	}
	if sess.initialHistory == nil {
		return nil, fmt.Errorf("no initial_history within 15s")
	}
	return historyFromWire(sess.initialHistory, true), nil
}

// historyFromWire converts a wire batch. For the initial batch the server
// does not always include cursor/hasMore — fall back to the oldest message
// id as the next cursor and a full-batch heuristic for has_more.
func historyFromWire(h *wireHistoryData, initial bool) *ChatHistory {
	out := &ChatHistory{
		Messages: make([]ChatMessage, 0, len(h.Messages)),
		Cursor:   h.Cursor,
		HasMore:  h.HasMore,
	}
	for _, m := range h.Messages {
		out.Messages = append(out.Messages, m.toChatMessage())
	}
	// The initial batch carries no cursor/hasMore and its messages arrive
	// OLDEST-FIRST (confirmed live 2026-06-11). When the batch is full
	// (newest 10) older pages may exist — surface the oldest message id as
	// the next cursor. A short batch is the whole history; no cursor.
	if initial && out.Cursor == "" && len(out.Messages) >= 10 {
		out.Cursor = out.Messages[0].MessageID
		out.HasMore = true
	}
	return out
}
