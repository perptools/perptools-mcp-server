package perptools

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"
)

type Client interface {
	// Public
	Health(ctx context.Context) (*HealthResponse, error)
	Version(ctx context.Context) (*VersionResponse, error)
	GetMarket(ctx context.Context, addresses string) ([]MarketResponse, error)
	GetMarkets(ctx context.Context, limit, offset int32) (*MarketsResponse, error)

	// Session-based (no orderly signature required)
	Session(ctx context.Context, publicKey string) error
	CreateEvent(ctx context.Context, event CreateEventRequest) error
	ApplyEarly(ctx context.Context, email string) error
	ApplyEarlyV2(ctx context.Context, req ApplyEarlyV2Request) error
	GetWhitelist(ctx context.Context, publicKey string) (*WhitelistResponse, error)
	ApplyWhitelistCode(ctx context.Context, publicKey, referralCode string) error

	// Protected — auto-signed with Orderly credentials
	GetUserPoints(ctx context.Context, publicKey string) (*UserPoints, error)
	GetUserPointsHistory(ctx context.Context, publicKey string) ([]UserPointsHistoryRow, error)
	GetUserMultipliers(ctx context.Context, publicKey string) (*Multipliers, error)
	GetLeaderboard(ctx context.Context, publicKey string, limit, offset int32) ([]LeaderboardEntry, error)
	GetFeeTier(ctx context.Context, publicKey string) (*FeeTier, error)

	GetAvailableAchievements(ctx context.Context, publicKey string) ([]Achievement, error)
	GetUserAchievements(ctx context.Context, publicKey string) ([]Achievement, error)
	ClaimAchievement(ctx context.Context, req ClaimAchievementRequest) error
	MarkAchievementNotified(ctx context.Context, req MarkAchievementNotifiedRequest) error

	GetAvailableTasks(ctx context.Context, publicKey string) ([]UserTask, error)
	VerifyTask(ctx context.Context, req VerifyTaskRequest) (*VerifyTaskResponse, error)

	GetUserAgent(ctx context.Context, publicKey string) (*AgentResponse, error)
	NewAgentAvatar(ctx context.Context, publicKey string) (*FileResponse, error)
	RegisterAgent(ctx context.Context, req RegisterAgentRequest) (*AgentResponse, error)
	ImproveDescription(ctx context.Context, req ImproveDescriptionRequest) (*AgentDescriptionResponse, error)

	// AI trading agents (Envy-backed) — routed through the /v1/ai/proxy gateway.
	DeployAgent(ctx context.Context, publicKey string, req DeployAgentRequest) (*DeployAgentResponse, error)
	// DepositToAgent moves USDC from the caller's already-funded Main Account
	// into the agent vault (path /api/v1/agents/deposit).
	DepositToAgent(ctx context.Context, publicKey string, req AgentDepositRequest) (*AgentDepositResponse, error)
	// DirectDepositToAgent funds the agent vault in one step
	// (path /api/v1/agents/direct-deposit). Same custodied-funds source.
	DirectDepositToAgent(ctx context.Context, publicKey string, req AgentDepositRequest) (*AgentDepositResponse, error)
	GetAgentTransactionStatus(ctx context.Context, publicKey string, req AgentTransactionStatusRequest) (*AgentTransactionStatusResponse, error)
	// WithdrawFromAgent redeems vault SHARES back into the Main Account
	// (path /api/v1/agents/withdraw). Settlement is tracked via the same
	// transaction-status endpoint deposits use.
	WithdrawFromAgent(ctx context.Context, publicKey string, req AgentWithdrawRequest) (*AgentWithdrawResponse, error)
	// WithdrawFromMaster moves USDC from the Main Account to the registered
	// on-chain wallet (path /api/v1/users/withdraw/solana). Synchronous —
	// the on-chain transfer is done when it returns.
	WithdrawFromMaster(ctx context.Context, publicKey string, req MasterWithdrawRequest) (*MasterWithdrawResponse, error)

	// Risk-disclosure agreement (orderly-signed). A signed agreement gates the
	// agent deposit flow — without it the backend returns ErrNotSignedAgreement.
	GetAgreementStatus(ctx context.Context, publicKey string) (*AgreementStatusResponse, error)
	VerifyAgreement(ctx context.Context, publicKey, signature string) (*AgreementStatusResponse, error)

	// GetUserInfo returns the user's Envy profile, including the custodied Main
	// Account address (solanaMasterWallet) — the on-chain top-up destination.
	GetUserInfo(ctx context.Context, publicKey string) (*UserInfoResponse, error)
	// GetDashboardOverview returns the user's Main Account USDC balance and agent counts.
	GetDashboardOverview(ctx context.Context, publicKey string) (*DashboardOverviewResponse, error)

	// Agent stats (read-only, via the orderly-signed proxy).
	// GetArenaAgents lists the public agent leaderboard (path /api/data/arena,
	// API-key auth signed server-side), sorted descending by sortBy.
	GetArenaAgents(ctx context.Context, publicKey, sortBy string, page, pageSize int) (*ArenaResponse, error)
	// GetAgentDetails returns one agent's profile, vault, analytics and the
	// caller's stake (path /api/v1/agents/details).
	GetAgentDetails(ctx context.Context, publicKey, agentID string) (*AgentDetailsResponse, error)
	// GetAgentPositions returns an agent's trades (path /api/v1/agents/positions).
	// posType filters by status (e.g. "open"); empty means all.
	GetAgentPositions(ctx context.Context, publicKey, agentID, posType string, page, limit int) (*AgentPositionsResponse, error)
	// GetAgentShares returns the caller's stake in one agent vault
	// (path /api/v1/agents/shares).
	GetAgentShares(ctx context.Context, publicKey, agentID string) (*AgentSharesResponse, error)
	// GetMyAgents lists every agent the wallet holds vault shares in
	// (path /api/v1/agents/list).
	GetMyAgents(ctx context.Context, publicKey string) (*MyAgentsResponse, error)

	// Agent chat — short-lived websocket sessions to the agent-chat relay.
	// SendAgentMessage sends one user message to an AI agent and holds the
	// socket open until the agent's reply quiesces (or timeout) — the relay
	// persists the reply only while the session is live.
	SendAgentMessage(ctx context.Context, publicKey, agentID, message string, timeout time.Duration) (*ChatReply, error)
	// GetAgentChatHistory returns persisted chat history: the connect-time
	// initial batch (newest 10) or, with before set, an older page via the
	// relay's load_more (served from the backend's Postgres).
	GetAgentChatHistory(ctx context.Context, publicKey, agentID, before string, limit int) (*ChatHistory, error)

	// OrderlyRawPost sends an orderly-signed POST to an arbitrary path and
	// returns the HTTP status + raw body. Used to probe whether gateway
	// endpoints are reachable with orderly auth (diagnostics only).
	OrderlyRawPost(ctx context.Context, path string, body any) (int, string, error)
	OrderlyRawGet(ctx context.Context, path string, queryKey, queryVal string) (int, string, error)

	RegisterReferralCode(ctx context.Context, publicKey, referralCode string) error

	GetLoyaltyRules(ctx context.Context, publicKey string) ([]LoyaltyRule, error)
	CompleteRule(ctx context.Context, req CompleteRuleRequest) (*RuleStatusResponse, error)
	GetSnagUser(ctx context.Context, publicKey string) (*SnagUser, error)

	GetMysteryTasks(ctx context.Context, publicKey string) ([]MysteryTask, error)

	// V2
	GetAchievementsV2(ctx context.Context, publicKey string) ([]Achievement, error)
	SetAchievementNotifiedV2(ctx context.Context, req SetAchievementNotifiedV2Request) error

	// Admin
	AdminCreateWhitelist(ctx context.Context, req AdminCreateWhitelistRequest) (*WhitelistAccess, error)
	AdminDeleteWhitelist(ctx context.Context, publicKey string) error
	AdminApplyRoundPointsDistribution(ctx context.Context, roundStart string) error
}


type client struct {
	pubHTTP    *resty.Client
	authedHTTP *resty.Client
	adminHTTP  *resty.Client

	// Orderly credentials, retained for the websocket agent-chat sessions
	// (which sign their own auth frames instead of HTTP headers).
	baseURL           string
	accountID         string
	orderlyPublicKey  string
	orderlyPrivateKey ed25519.PrivateKey
}

func NewClient(baseURL string) Client {
	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
	}

	pubClient := resty.New().SetBaseURL(baseURL).SetHeaders(headers)
	authedClient := resty.New().SetBaseURL(baseURL).SetHeaders(headers)

	return &client{
		pubHTTP:    pubClient,
		authedHTTP: authedClient,
		baseURL:    baseURL,
	}
}

func NewClientWithAuth(baseURL, accountID, publicKey string, privateKey ed25519.PrivateKey) Client {
	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
	}

	pubClient := resty.New().SetBaseURL(baseURL).SetHeaders(headers)
	authedClient := resty.New().SetBaseURL(baseURL).SetHeaders(headers)
	authedClient.OnBeforeRequest(orderlySignMiddleware(accountID, publicKey, privateKey))

	return &client{
		pubHTTP:           pubClient,
		authedHTTP:        authedClient,
		baseURL:           baseURL,
		accountID:         accountID,
		orderlyPublicKey:  publicKey,
		orderlyPrivateKey: privateKey,
	}
}

// orderlySignMiddleware auto-signs each request with the stored Orderly ed25519 credentials.
// Signature format matches VerifyOrderlySignature middleware: timestamp + METHOD + /path?query + body
func orderlySignMiddleware(accountID, publicKey string, privateKey ed25519.PrivateKey) resty.RequestMiddleware {
	return func(c *resty.Client, r *resty.Request) error {
		ts := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
		normalized := ts + r.Method + r.URL

		if len(r.QueryParam) != 0 {
			normalized += "?" + r.QueryParam.Encode()
		}

		if r.Method != http.MethodGet && r.Method != http.MethodDelete && r.Body != nil {
			bodyBytes, err := json.Marshal(r.Body)
			if err != nil {
				return fmt.Errorf("marshal body: %w", err)
			}
			normalized += string(bodyBytes)
		}

		sig := ed25519.Sign(privateKey, []byte(normalized))
		sigBase64 := base64.RawURLEncoding.EncodeToString(sig)

		r.SetHeader("orderly-account-id", accountID)
		r.SetHeader("orderly-key", "ed25519:"+publicKey)
		r.SetHeader("orderly-timestamp", ts)
		r.SetHeader("orderly-signature", sigBase64)

		if r.Method == http.MethodGet || r.Method == http.MethodDelete {
			r.SetHeader("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r.SetHeader("Content-Type", "application/json")
		}

		return nil
	}
}

func checkErr(r *resty.Response, err error, op string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if r.IsError() {
		return fmt.Errorf("%s: %s %s", op, r.Status(), r.String())
	}
	return nil
}

// ---------------------------------------------------------------------------
// Public
// ---------------------------------------------------------------------------

func (c *client) Health(ctx context.Context) (*HealthResponse, error) {
	var out HealthResponse
	r, err := c.pubHTTP.R().SetContext(ctx).SetResult(&out).Get("/health")
	if e := checkErr(r, err, "health"); e != nil {
		return nil, e
	}
	return &out, nil
}

func (c *client) Version(ctx context.Context) (*VersionResponse, error) {
	var out VersionResponse
	r, err := c.pubHTTP.R().SetContext(ctx).SetResult(&out).Get("/v")
	if e := checkErr(r, err, "version"); e != nil {
		return nil, e
	}
	return &out, nil
}

func (c *client) GetMarket(ctx context.Context, addresses string) ([]MarketResponse, error) {
	var out []MarketResponse
	r, err := c.pubHTTP.R().SetContext(ctx).
		SetQueryParam("addresses", addresses).
		SetResult(&out).
		Get("/v1/market")
	if e := checkErr(r, err, "get market"); e != nil {
		return nil, e
	}
	return out, nil
}

func (c *client) GetMarkets(ctx context.Context, limit, offset int32) (*MarketsResponse, error) {
	var out MarketsResponse
	req := c.pubHTTP.R().SetContext(ctx)
	if limit > 0 {
		req.SetQueryParam("limit", fmt.Sprintf("%d", limit))
	}
	if offset > 0 {
		req.SetQueryParam("offset", fmt.Sprintf("%d", offset))
	}
	r, err := req.SetResult(&out).Get("/v1/markets")
	if e := checkErr(r, err, "get markets"); e != nil {
		return nil, e
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Session-based (no orderly signature)
// ---------------------------------------------------------------------------

func (c *client) Session(ctx context.Context, publicKey string) error {
	r, err := c.pubHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		Get("/v1/session")
	return checkErr(r, err, "session")
}

func (c *client) CreateEvent(ctx context.Context, event CreateEventRequest) error {
	r, err := c.pubHTTP.R().SetContext(ctx).SetBody(event).Post("/v1/event")
	return checkErr(r, err, "create event")
}

func (c *client) ApplyEarly(ctx context.Context, email string) error {
	r, err := c.pubHTTP.R().SetContext(ctx).
		SetBody(map[string]string{"email": email}).
		Post("/v1/early/apply")
	return checkErr(r, err, "apply early")
}

func (c *client) ApplyEarlyV2(ctx context.Context, req ApplyEarlyV2Request) error {
	r, err := c.pubHTTP.R().SetContext(ctx).SetBody(req).Post("/v2/early/apply")
	return checkErr(r, err, "apply early v2")
}

func (c *client) GetWhitelist(ctx context.Context, publicKey string) (*WhitelistResponse, error) {
	var out WhitelistResponse
	r, err := c.pubHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		SetResult(&out).
		Get("/v1/early/whitelist")
	if e := checkErr(r, err, "get whitelist"); e != nil {
		return nil, e
	}
	return &out, nil
}

func (c *client) ApplyWhitelistCode(ctx context.Context, publicKey, referralCode string) error {
	r, err := c.pubHTTP.R().SetContext(ctx).
		SetBody(map[string]string{"public_key": publicKey, "referral_code": referralCode}).
		Post("/v1/early/whitelist/join")
	return checkErr(r, err, "apply whitelist code")
}

// ---------------------------------------------------------------------------
// Protected (auto-signed with Orderly credentials)
// ---------------------------------------------------------------------------

func (c *client) GetUserPoints(ctx context.Context, publicKey string) (*UserPoints, error) {
	var out UserPoints
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		SetResult(&out).
		Get("/v1/points")
	if e := checkErr(r, err, "get user points"); e != nil {
		return nil, e
	}
	return &out, nil
}

func (c *client) GetUserPointsHistory(ctx context.Context, publicKey string) ([]UserPointsHistoryRow, error) {
	var out []UserPointsHistoryRow
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		SetResult(&out).
		Get("/v1/points/history")
	if e := checkErr(r, err, "get user points history"); e != nil {
		return nil, e
	}
	return out, nil
}

func (c *client) GetUserMultipliers(ctx context.Context, publicKey string) (*Multipliers, error) {
	var out Multipliers
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		SetResult(&out).
		Get("/v1/multipliers")
	if e := checkErr(r, err, "get user multipliers"); e != nil {
		return nil, e
	}
	return &out, nil
}

func (c *client) GetLeaderboard(ctx context.Context, publicKey string, limit, offset int32) ([]LeaderboardEntry, error) {
	var out []LeaderboardEntry
	req := c.authedHTTP.R().SetContext(ctx).SetQueryParam("public_key", publicKey)
	if limit > 0 {
		req.SetQueryParam("limit", fmt.Sprintf("%d", limit))
	}
	if offset > 0 {
		req.SetQueryParam("offset", fmt.Sprintf("%d", offset))
	}
	r, err := req.SetResult(&out).Get("/v1/leaderboard")
	if e := checkErr(r, err, "get leaderboard"); e != nil {
		return nil, e
	}
	return out, nil
}

func (c *client) GetFeeTier(ctx context.Context, publicKey string) (*FeeTier, error) {
	var out FeeTier
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		SetResult(&out).
		Get("/v1/tier")
	if e := checkErr(r, err, "get fee tier"); e != nil {
		return nil, e
	}
	return &out, nil
}

func (c *client) GetAvailableAchievements(ctx context.Context, publicKey string) ([]Achievement, error) {
	var out []Achievement
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		SetResult(&out).
		Get("/v1/achievement/available")
	if e := checkErr(r, err, "get available achievements"); e != nil {
		return nil, e
	}
	return out, nil
}

func (c *client) GetUserAchievements(ctx context.Context, publicKey string) ([]Achievement, error) {
	var out []Achievement
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		SetResult(&out).
		Get("/v1/achievement/claimed")
	if e := checkErr(r, err, "get user achievements"); e != nil {
		return nil, e
	}
	return out, nil
}

func (c *client) ClaimAchievement(ctx context.Context, req ClaimAchievementRequest) error {
	r, err := c.authedHTTP.R().SetContext(ctx).SetBody(req).Post("/v1/achievement/claim")
	return checkErr(r, err, "claim achievement")
}

func (c *client) MarkAchievementNotified(ctx context.Context, req MarkAchievementNotifiedRequest) error {
	r, err := c.authedHTTP.R().SetContext(ctx).SetBody(req).Post("/v1/achievement/notified")
	return checkErr(r, err, "mark achievement notified")
}

func (c *client) GetAvailableTasks(ctx context.Context, publicKey string) ([]UserTask, error) {
	var out []UserTask
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		SetResult(&out).
		Get("/v1/tasks/list")
	if e := checkErr(r, err, "get available tasks"); e != nil {
		return nil, e
	}
	return out, nil
}

func (c *client) VerifyTask(ctx context.Context, req VerifyTaskRequest) (*VerifyTaskResponse, error) {
	var out VerifyTaskResponse
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetBody(req).
		SetResult(&out).
		Post("/v1/tasks/verify")
	if e := checkErr(r, err, "verify task"); e != nil {
		return nil, e
	}
	return &out, nil
}

func (c *client) GetUserAgent(ctx context.Context, publicKey string) (*AgentResponse, error) {
	var out AgentResponse
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		SetResult(&out).
		Get("/v1/agent")
	if e := checkErr(r, err, "get user agent"); e != nil {
		return nil, e
	}
	return &out, nil
}

func (c *client) NewAgentAvatar(ctx context.Context, publicKey string) (*FileResponse, error) {
	var out FileResponse
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		SetResult(&out).
		Get("/v1/agent/avatar")
	if e := checkErr(r, err, "new agent avatar"); e != nil {
		return nil, e
	}
	return &out, nil
}

func (c *client) RegisterAgent(ctx context.Context, req RegisterAgentRequest) (*AgentResponse, error) {
	var out AgentResponse
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetBody(req).
		SetResult(&out).
		Post("/v1/agent/register")
	if e := checkErr(r, err, "register agent"); e != nil {
		return nil, e
	}
	return &out, nil
}

func (c *client) ImproveDescription(ctx context.Context, req ImproveDescriptionRequest) (*AgentDescriptionResponse, error) {
	var out AgentDescriptionResponse
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetBody(req).
		SetResult(&out).
		Post("/v1/agent/description")
	if e := checkErr(r, err, "improve description"); e != nil {
		return nil, e
	}
	return &out, nil
}

func (c *client) RegisterReferralCode(ctx context.Context, publicKey, referralCode string) error {
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetBody(map[string]string{"public_key": publicKey, "referral_code": referralCode}).
		Post("/v1/referral/join")
	return checkErr(r, err, "register referral code")
}

func (c *client) GetLoyaltyRules(ctx context.Context, publicKey string) ([]LoyaltyRule, error) {
	var out []LoyaltyRule
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		SetResult(&out).
		Get("/v1/snag/rules")
	if e := checkErr(r, err, "get loyalty rules"); e != nil {
		return nil, e
	}
	return out, nil
}

func (c *client) CompleteRule(ctx context.Context, req CompleteRuleRequest) (*RuleStatusResponse, error) {
	var out RuleStatusResponse
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetBody(req).
		SetResult(&out).
		Post("/v1/snag/rules/complete")
	if e := checkErr(r, err, "complete rule"); e != nil {
		return nil, e
	}
	return &out, nil
}

func (c *client) GetSnagUser(ctx context.Context, publicKey string) (*SnagUser, error) {
	var out SnagUser
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		SetResult(&out).
		Get("/v1/snag/user")
	if e := checkErr(r, err, "get snag user"); e != nil {
		return nil, e
	}
	return &out, nil
}

func (c *client) GetMysteryTasks(ctx context.Context, publicKey string) ([]MysteryTask, error) {
	var out []MysteryTask
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		SetResult(&out).
		Get("/v1/mystery/history")
	if e := checkErr(r, err, "get mystery tasks"); e != nil {
		return nil, e
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// V2
// ---------------------------------------------------------------------------

func (c *client) GetAchievementsV2(ctx context.Context, publicKey string) ([]Achievement, error) {
	var out []Achievement
	r, err := c.pubHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		SetResult(&out).
		Get("/v2/achievements")
	if e := checkErr(r, err, "get achievements v2"); e != nil {
		return nil, e
	}
	return out, nil
}

func (c *client) SetAchievementNotifiedV2(ctx context.Context, req SetAchievementNotifiedV2Request) error {
	r, err := c.pubHTTP.R().SetContext(ctx).SetBody(req).Post("/v2/achievements/notified")
	return checkErr(r, err, "set achievement notified v2")
}

// ---------------------------------------------------------------------------
// Admin
// ---------------------------------------------------------------------------

func (c *client) AdminCreateWhitelist(ctx context.Context, req AdminCreateWhitelistRequest) (*WhitelistAccess, error) {
	var out WhitelistAccess
	r, err := c.adminHTTP.R().SetContext(ctx).
		SetBody(req).
		SetResult(&out).
		Post("/v1/admin/early/whitelist/grant")
	if e := checkErr(r, err, "admin create whitelist"); e != nil {
		return nil, e
	}
	return &out, nil
}

func (c *client) AdminDeleteWhitelist(ctx context.Context, publicKey string) error {
	r, err := c.adminHTTP.R().SetContext(ctx).
		SetBody(map[string]string{"public_key": publicKey}).
		Delete("/v1/admin/early/whitelist/drop")
	return checkErr(r, err, "admin delete whitelist")
}

func (c *client) AdminApplyRoundPointsDistribution(ctx context.Context, roundStart string) error {
	r, err := c.adminHTTP.R().SetContext(ctx).
		SetBody(map[string]string{"round_start": roundStart}).
		Post("/v1/admin/points/distribution")
	return checkErr(r, err, "admin apply round points distribution")
}

// ---------------------------------------------------------------------------
// AI Agents (Envy-backed, via /v1/ai/proxy)
// ---------------------------------------------------------------------------

// aiProxy forwards an Envy call through the perptools AI proxy. publicKey must
// equal the walletAddress inside body — the proxy rejects a mismatch. authType
// 0 = HMAC-SHA256 (the Envy request is signed server-side with the house key).
// The proxy responds with the raw Envy JSON, decoded into out.
func (c *client) aiProxy(ctx context.Context, publicKey, path, method string, body, out any) error {
	return c.aiProxyAuthType(ctx, publicKey, path, method, 0, body, out)
}

// aiProxyAuthType is aiProxy with an explicit Envy auth type: 0 = HMAC-SHA256
// (per-user house signing), 1 = API key (privileged server credential; the
// proxy allow-lists which paths may use it — e.g. /api/data/arena). Query
// strings ride inside path verbatim; body must be a JSON object (use an empty
// map, not nil, for body-less GETs — the proxy rejects non-object bodies).
func (c *client) aiProxyAuthType(ctx context.Context, publicKey, path, method string, authType int, body, out any) error {
	rawBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal proxy body: %w", err)
	}
	envelope := AIProxyRequest{
		PublicKey: publicKey,
		Path:      path,
		Method:    method,
		AuthType:  authType,
		Body:      rawBody,
	}
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetBody(envelope).
		SetResult(out).
		Post("/v1/ai/proxy")
	return checkErr(r, err, "ai proxy "+path)
}

func (c *client) DeployAgent(ctx context.Context, publicKey string, req DeployAgentRequest) (*DeployAgentResponse, error) {
	var out DeployAgentResponse
	if err := c.aiProxy(ctx, publicKey, "/api/v1/deploy/agent", http.MethodPost, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) DepositToAgent(ctx context.Context, publicKey string, req AgentDepositRequest) (*AgentDepositResponse, error) {
	return c.agentDeposit(ctx, publicKey, "/api/v1/agents/deposit", req)
}

func (c *client) DirectDepositToAgent(ctx context.Context, publicKey string, req AgentDepositRequest) (*AgentDepositResponse, error) {
	return c.agentDeposit(ctx, publicKey, "/api/v1/agents/direct-deposit", req)
}

func (c *client) agentDeposit(ctx context.Context, publicKey, path string, req AgentDepositRequest) (*AgentDepositResponse, error) {
	var out AgentDepositResponse
	if err := c.aiProxy(ctx, publicKey, path, http.MethodPost, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WithdrawFromAgent redeems vault shares back into the Main Account (Envy
// path /api/v1/agents/withdraw). Shares is denominated in SHARES, not USDC.
// NOTE: /api/v1/agents/withdraw-all is blocked server-side ("withdraw-all is
// not supported") — "all" is emulated by passing the full current share count.
func (c *client) WithdrawFromAgent(ctx context.Context, publicKey string, req AgentWithdrawRequest) (*AgentWithdrawResponse, error) {
	var out AgentWithdrawResponse
	if err := c.aiProxy(ctx, publicKey, "/api/v1/agents/withdraw", http.MethodPost, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WithdrawFromMaster moves USDC from the Main Account to the registered
// on-chain wallet (Envy path /api/v1/users/withdraw/solana — the sdk-era
// /api/sdk/withdraw/solana 404s upstream; live path verified by probing
// 2026-06-10). Fully server-side and synchronous.
func (c *client) WithdrawFromMaster(ctx context.Context, publicKey string, req MasterWithdrawRequest) (*MasterWithdrawResponse, error) {
	var out MasterWithdrawResponse
	if err := c.aiProxy(ctx, publicKey, "/api/v1/users/withdraw/solana", http.MethodPost, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) GetAgentTransactionStatus(ctx context.Context, publicKey string, req AgentTransactionStatusRequest) (*AgentTransactionStatusResponse, error) {
	var out AgentTransactionStatusResponse
	if err := c.aiProxy(ctx, publicKey, "/api/v1/agents/transaction/status", http.MethodPost, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAgreementStatus reports whether the wallet has signed the risk-disclosure
// agreement and returns the message that must be signed if it has not.
func (c *client) GetAgreementStatus(ctx context.Context, publicKey string) (*AgreementStatusResponse, error) {
	var out AgreementStatusResponse
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetQueryParam("public_key", publicKey).
		SetResult(&out).
		Get("/v1/ai/agreement/status")
	if e := checkErr(r, err, "get agreement status"); e != nil {
		return nil, e
	}
	return &out, nil
}

// VerifyAgreement submits the wallet's ed25519 signature over the agreement
// message (base58-encoded) and records the signature server-side.
func (c *client) VerifyAgreement(ctx context.Context, publicKey, signature string) (*AgreementStatusResponse, error) {
	var out AgreementStatusResponse
	r, err := c.authedHTTP.R().SetContext(ctx).
		SetBody(VerifyAgreementRequest{PublicKey: publicKey, Signature: signature}).
		SetResult(&out).
		Post("/v1/ai/agreement/verify")
	if e := checkErr(r, err, "verify agreement"); e != nil {
		return nil, e
	}
	return &out, nil
}

// GetUserInfo fetches the user's Envy profile (incl. solanaMasterWallet) via
// the orderly-signed proxy (Envy path /api/v1/users/info).
func (c *client) GetUserInfo(ctx context.Context, publicKey string) (*UserInfoResponse, error) {
	var out UserInfoResponse
	body := map[string]string{"walletAddress": publicKey}
	if err := c.aiProxy(ctx, publicKey, "/api/v1/users/info", http.MethodPost, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetDashboardOverview fetches the user's Main Account balance + agent counts
// via the orderly-signed proxy (Envy path /api/v1/dashboard/overview).
func (c *client) GetDashboardOverview(ctx context.Context, publicKey string) (*DashboardOverviewResponse, error) {
	var out DashboardOverviewResponse
	body := map[string]string{"walletAddress": publicKey}
	if err := c.aiProxy(ctx, publicKey, "/api/v1/dashboard/overview", http.MethodPost, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetArenaAgents fetches the public agent leaderboard (Envy path
// /api/data/arena, API-key auth — allow-listed on the proxy). sortByDesc is
// pinned to sortBy: without it the backend returns ASCENDING order (worst
// first), which is never what a leaderboard caller wants.
func (c *client) GetArenaAgents(ctx context.Context, publicKey, sortBy string, page, pageSize int) (*ArenaResponse, error) {
	var out ArenaResponse
	path := fmt.Sprintf("/api/data/arena?page=%d&pageSize=%d&sortBy=%s&sortByDesc=%s",
		page, pageSize, url.QueryEscape(sortBy), url.QueryEscape(sortBy))
	if err := c.aiProxyAuthType(ctx, publicKey, path, http.MethodGet, 1, map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAgentDetails fetches one agent's profile, vault state, analytics and the
// caller's stake (Envy path /api/v1/agents/details). walletAddress must equal
// publicKey — the proxy rejects a mismatch.
func (c *client) GetAgentDetails(ctx context.Context, publicKey, agentID string) (*AgentDetailsResponse, error) {
	var out AgentDetailsResponse
	body := map[string]string{"walletAddress": publicKey, "agentId": agentID}
	if err := c.aiProxy(ctx, publicKey, "/api/v1/agents/details", http.MethodPost, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAgentPositions fetches an agent's trades (Envy path
// /api/v1/agents/positions). posType filters by status (e.g. "open"); empty
// returns all. page/limit <= 0 are omitted (backend defaults apply).
func (c *client) GetAgentPositions(ctx context.Context, publicKey, agentID, posType string, page, limit int) (*AgentPositionsResponse, error) {
	var out AgentPositionsResponse
	body := map[string]any{"walletAddress": publicKey, "agentId": agentID}
	if posType != "" {
		body["type"] = posType
	}
	if page > 0 {
		body["page"] = page
	}
	if limit > 0 {
		body["limit"] = limit
	}
	if err := c.aiProxy(ctx, publicKey, "/api/v1/agents/positions", http.MethodPost, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAgentShares fetches the caller's stake in one agent vault (Envy path
// /api/v1/agents/shares).
func (c *client) GetAgentShares(ctx context.Context, publicKey, agentID string) (*AgentSharesResponse, error) {
	var out AgentSharesResponse
	body := map[string]string{"walletAddress": publicKey, "agentId": agentID}
	if err := c.aiProxy(ctx, publicKey, "/api/v1/agents/shares", http.MethodPost, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMyAgents lists every agent the wallet currently holds vault shares in
// (Envy path /api/v1/agents/list).
func (c *client) GetMyAgents(ctx context.Context, publicKey string) (*MyAgentsResponse, error) {
	var out MyAgentsResponse
	body := map[string]string{"walletAddress": publicKey}
	if err := c.aiProxy(ctx, publicKey, "/api/v1/agents/list", http.MethodPost, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) OrderlyRawPost(ctx context.Context, path string, body any) (int, string, error) {
	r, err := c.authedHTTP.R().SetContext(ctx).SetBody(body).Post(path)
	if err != nil {
		return 0, "", err
	}
	return r.StatusCode(), string(r.Body()), nil
}

func (c *client) OrderlyRawGet(ctx context.Context, path string, queryKey, queryVal string) (int, string, error) {
	req := c.authedHTTP.R().SetContext(ctx)
	if queryKey != "" {
		req.SetQueryParam(queryKey, queryVal)
	}
	r, err := req.Get(path)
	if err != nil {
		return 0, "", err
	}
	return r.StatusCode(), string(r.Body()), nil
}
