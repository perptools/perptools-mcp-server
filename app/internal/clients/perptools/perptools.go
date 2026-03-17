package perptools

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
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
		pubHTTP:    pubClient,
		authedHTTP: authedClient,
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
