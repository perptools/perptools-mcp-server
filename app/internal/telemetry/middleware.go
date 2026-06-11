package telemetry

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"mcp-server/app/internal/clients/solfund"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// AuthState is the slice of the service the middleware needs: the wallet of
// the currently authenticated user, or "" when unauthenticated. Satisfied by
// *service.Service.
type AuthState interface {
	AuthenticatedWallet() string
}

// ToolMiddleware wraps every tool handler to emit one mcp_tool_called event
// per invocation. The wrapped handler's result and error are returned
// unchanged; event construction is recover-guarded so telemetry can never
// break a tool call.
func ToolMiddleware(em *Emitter, auth AuthState) server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			start := time.Now()
			result, err := next(ctx, req)

			func() {
				defer func() {
					if r := recover(); r != nil {
						em.logFailure(panicError{})
					}
				}()
				em.Emit(em.buildEvent(ctx, auth, req, result, err, time.Since(start)))
			}()

			return result, err
		}
	}
}

// panicError stands in for a recovered panic in event construction without
// leaking the panic value (which could contain anything) into logs.
type panicError struct{}

func (panicError) Error() string { return "panic while building telemetry event" }

func (e *Emitter) buildEvent(
	ctx context.Context,
	auth AuthState,
	req mcp.CallToolRequest,
	result *mcp.CallToolResult,
	err error,
	duration time.Duration,
) Event {
	success := err == nil && !(result != nil && result.IsError)
	publicKey := ResolvePublicKey(auth, req)

	data := EventData{
		Tool:       req.Params.Name,
		Success:    success,
		DurationMS: duration.Milliseconds(),
	}
	if !success {
		data.ErrorCategory = ClassifyError(ctx, result, err)
	}
	return Event{PublicKey: publicKey, Data: data}
}

// ResolvePublicKey picks the event's public_key:
//  1. the authenticated wallet, when set;
//  2. else the tool's "wallet_address" argument, if it is a valid base58
//     Solana public key;
//  3. else the system-program sentinel (pre-auth calls).
func ResolvePublicKey(auth AuthState, req mcp.CallToolRequest) string {
	if auth != nil {
		if w := auth.AuthenticatedWallet(); w != "" {
			return w
		}
	}
	if w, ok := req.GetArguments()["wallet_address"].(string); ok && w != "" {
		if _, err := solfund.ParsePublicKey(w); err == nil {
			return w
		}
	}
	return SentinelPublicKey
}

// The classifier parses the deterministic error formats each source actually
// produces — no keyword guessing:
//
//	perptools checkErr   "op: 400 Bad Request {\"message\":...,\"error_code\":1000}"
//	                     — the backend's resp.Error body verbatim. error_code
//	                     families (jupbot-perps-api resp/errors.go): 1000-1999
//	                     validation, 2000+ system, 3000+ envy/payments,
//	                     4000+ withdrawals.
//	orderly REST         same "op: <status> <body>" shape with {"code":-N,...},
//	                     or "op: [-N] message" from orderly.APIError.Error().
//	own validation       exact literals from this repo's handlers and mcp-go's
//	                     Require* helpers.
//	transport            Go net/context error strings.

// httpStatusRe anchors on the resty r.Status() shape both clients embed:
// `: 400 Bad Request`, `: 500 Internal Server Error`.
var httpStatusRe = regexp.MustCompile(`: ([1-5]\d\d) [A-Z]`)

// orderlyCodeRe matches orderly.APIError.Error()'s "[%d] %s" prefix.
var orderlyCodeRe = regexp.MustCompile(`: \[-?\d+\] `)

// wireError extracts the structured code from an embedded upstream JSON body:
// error_code is jupbot-perps-api's resp.Error, code is Orderly's.
type wireError struct {
	ErrorCode int `json:"error_code"`
	Code      int `json:"code"`
}

// ClassifyError maps a failed tool invocation onto the closed error-category
// set: auth_required | invalid_args | rejected | timeout | upstream_error |
// internal. Tool handlers in this repo wrap almost all failures into
// mcp.NewToolResultError(...) text and return a nil Go error, so
// classification works on the result text (or err.Error() when err != nil).
// The raw text is matched here and then discarded — it never enters the event.
func ClassifyError(ctx context.Context, result *mcp.CallToolResult, err error) string {
	var msg string
	switch {
	case err != nil:
		msg = err.Error()
	case result != nil:
		msg = resultText(result)
	}
	lower := strings.ToLower(msg)

	switch {
	// Our own auth gate: requireAuth (service.go) and the formatAuthError
	// banner. Upstream 401/403 are handled by status below.
	case strings.Contains(lower, "authentication required"),
		strings.Contains(lower, "not authenticated"),
		strings.Contains(lower, "orderly key not set"):
		return "auth_required"

	// Go stdlib/net exact strings + the chat tool's own "timed out".
	case ctx.Err() != nil,
		strings.Contains(lower, "context deadline exceeded"),
		strings.Contains(lower, "context canceled"),
		strings.Contains(lower, "client.timeout"),
		strings.Contains(lower, "i/o timeout"),
		strings.Contains(lower, "timed out"):
		return "timeout"
	}

	// An embedded HTTP status means the request reached an upstream and was
	// answered — classify by status + the structured code in the body.
	if m := httpStatusRe.FindStringSubmatchIndex(msg); m != nil {
		status := msg[m[2]:m[3]]
		var wire wireError
		if i := strings.Index(msg[m[1]:], "{"); i >= 0 {
			_ = json.Unmarshal([]byte(msg[m[1]+i:]), &wire)
		}
		switch {
		case status == "401" || status == "403":
			return "auth_required"
		case status[0] == '5' || status == "429" || status == "404":
			// 404 = dead/changed endpoint (an integration problem, not the
			// caller's), 429 = rate limit, 5xx = upstream failure.
			return "upstream_error"
		case wire.ErrorCode >= 2000:
			// Backend business-rule rejection (system/envy/payment/withdrawal
			// families): agreement not signed, withdrawal already pending, ...
			return "rejected"
		case wire.Code != 0:
			// Orderly rejection: insufficient margin, min notional, ...
			return "rejected"
		default:
			// Remaining 4xx with the validation family (error_code 1000-1999)
			// or no structured code: the request itself was malformed.
			return "invalid_args"
		}
	}

	switch {
	// orderly.APIError without an embedded status ("op: [-1101] message").
	case orderlyCodeRe.MatchString(msg):
		return "rejected"

	// Exact validation literals: mcp-go Require* ("required argument %q not
	// found", "argument %q is not a string") and this repo's handlers/service.
	case strings.Contains(lower, "required argument"),
		strings.Contains(lower, "is not a"),
		strings.Contains(lower, "is required and must be"),
		strings.Contains(lower, "pass either shares"),
		strings.Contains(lower, "invalid wallet address"),
		strings.Contains(lower, "invalid master address"),
		strings.Contains(lower, "invalid sort_by"):
		return "invalid_args"

	// Transport-level failures (Go net exact strings) — never reached the
	// upstream handler.
	case strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "connection reset"),
		strings.Contains(lower, "no such host"),
		strings.Contains(lower, "broken pipe"),
		strings.Contains(lower, "unexpected eof"):
		return "upstream_error"

	default:
		return "internal"
	}
}

func resultText(r *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}
