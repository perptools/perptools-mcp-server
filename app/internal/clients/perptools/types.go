package perptools

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type HealthResponse struct {
	Status string `json:"status"`
}

type VersionResponse struct {
	Version string `json:"version"`
}

type MarketResponse struct {
	Url    *string `json:"url,omitempty"`
	Exists bool    `json:"exists"`
}

type MarketsResponse struct {
	Markets []MarketItem `json:"markets"`
}

type MarketItem struct {
	Symbol string  `json:"symbol"`
	Url    *string `json:"url,omitempty"`
}

type WhitelistResponse struct {
	IsWhitelisted bool       `json:"is_whitelisted"`
	ActiveAfter   *time.Time `json:"active_after,omitempty"`
}

type WhitelistAccess struct {
	AccessID    uuid.UUID  `json:"access_id"`
	PublicKey   string     `json:"public_key"`
	ActiveAfter *time.Time `json:"active_after,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Request types

type CreateEventRequest struct {
	PublicKey string          `json:"public_key"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data,omitempty"`
}

type ApplyEarlyV2Request struct {
	PublicKey string               `json:"public_key"`
	Contacts  []ApplyEarlyV2Contact `json:"contacts"`
}

type ApplyEarlyV2Contact struct {
	Contact     string `json:"contact"`
	ContactType string `json:"type"`
}

type AdminCreateWhitelistRequest struct {
	PublicKey   string     `json:"public_key"`
	ActiveAfter *time.Time `json:"active_after,omitempty"`
}

type SetAchievementNotifiedV2Request struct {
	PublicKey     string    `json:"public_key"`
	AchievementID uuid.UUID `json:"achievement_id"`
}

type ClaimAchievementRequest struct {
	PublicKey string    `json:"public_key"`
	AchiveID  uuid.UUID `json:"achive_id"`
}

type MarkAchievementNotifiedRequest struct {
	PublicKey string    `json:"public_key"`
	AchiveID  uuid.UUID `json:"achive_id"`
}

type VerifyTaskRequest struct {
	PublicKey string    `json:"public_key"`
	TaskID    uuid.UUID `json:"task_id"`
}

type RegisterAgentRequest struct {
	PublicKey   string `json:"public_key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	RiskLevel   string `json:"risk_level"`
	AvatarUrl   string `json:"avatar_url"`
}

type ImproveDescriptionRequest struct {
	PublicKey   string `json:"public_key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	RiskLevel   string `json:"risk_level"`
}

type CompleteRuleRequest struct {
	RuleId           string  `json:"rule_id"`
	PublicKey        string  `json:"public_key"`
	VerificationCode *string `json:"verification_code,omitempty"`
	ContentUrl       string  `json:"content_url,omitempty"`
}

// Response types

type UserPoints struct {
	AsociatedEvmWallet *string          `json:"asociated_evm_wallet,omitempty"`
	WeekRound          int32            `json:"week_round"`
	Points             decimal.Decimal  `json:"points"`
	Ditribution        AggDistribution  `json:"ditribution"`
}

type AggDistribution struct {
	UpdatedAt      time.Time       `json:"updated_at"`
	Change         decimal.Decimal `json:"change"`
	TradingPoints  decimal.Decimal `json:"trading_points"`
	ReferralPoints decimal.Decimal `json:"referral_points"`
	MysteryPoints  decimal.Decimal `json:"mystery_points"`
}

type UserPointsHistoryRow struct {
	Seazon      uint                       `json:"seazon"`
	Week        uint                       `json:"week"`
	StartTime   time.Time                  `json:"start_time"`
	EndTime     time.Time                  `json:"end_time"`
	Ditribution map[string]decimal.Decimal `json:"ditribution"`
}

type Multipliers struct {
	Total       decimal.Decimal `json:"total"`
	Multipliers []Multiplier    `json:"multipliers"`
}

type Multiplier struct {
	Type  string          `json:"type"`
	Value decimal.Decimal `json:"value"`
}

type LeaderboardEntry struct {
	Rank      int64           `json:"rank"`
	PublicKey string          `json:"public_key"`
	Points    decimal.Decimal `json:"points"`
}

// LeaderboardStanding is the user-centric standing the /v1/leaderboard endpoint
// now returns (the backend changed it from a paginated list of entries to the
// caller's own rank/tier/points summary).
type LeaderboardStanding struct {
	UserPoints       string  `json:"user_points"`
	UserRank         int64   `json:"user_rank"`
	UserPreviousRank int64   `json:"user_previous_rank"`
	CurrentTier      string  `json:"current_tier"`
	NextTier         string  `json:"next_tier"`
	ProgressBar      float64 `json:"progress_bar"`
}

type FeeTier struct {
	Tier        int32           `json:"tier"`
	MakerFee    decimal.Decimal `json:"maker_fee"`
	TakerFee    decimal.Decimal `json:"taker_fee"`
}

type Achievement struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Points      decimal.Decimal `json:"points"`
	Claimed     bool            `json:"claimed,omitempty"`
	Notified    bool            `json:"notified,omitempty"`
}

type UserTask struct {
	TaskID      uuid.UUID       `json:"task_id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Points      decimal.Decimal `json:"points"`
	Status      string          `json:"status"`
}

type VerifyTaskResponse struct {
	Points decimal.Decimal `json:"points"`
}

type AgentResponse struct {
	AgentID     uuid.UUID `json:"agent_id"`
	PublicKey   string    `json:"public_key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	RiskLevel   int64     `json:"risk_level"`
	LogoUrl     string    `json:"logo_url"`
}

type AgentDescriptionResponse struct {
	Description string `json:"description"`
}

type FileResponse struct {
	URL string `json:"url"`
}

type RuleStatusResponse struct {
	Status string `json:"status"`
}

type LoyaltyRule struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Points      int32     `json:"points"`
	Completed   bool      `json:"completed"`
}

type SnagUser struct {
	UserID    uuid.UUID `json:"user_id"`
	PublicKey string    `json:"public_key"`
	Twitter   *string   `json:"twitter,omitempty"`
	Discord   *string   `json:"discord,omitempty"`
	Email     *string   `json:"email,omitempty"`
}

type MysteryTask struct {
	ID     uuid.UUID       `json:"id"`
	Type   string          `json:"type"`
	Points decimal.Decimal `json:"points"`
	Status string          `json:"status"`
}

// ---------------------------------------------------------------------------
// AI Agents (Envy-backed, called through the /v1/ai/proxy gateway)
// ---------------------------------------------------------------------------

// AIProxyRequest is the envelope POSTed to /v1/ai/proxy. PublicKey must equal
// the walletAddress inside Body — the proxy rejects a mismatch. AuthType 0
// means the Envy call is HMAC-signed server-side with the house key.
type AIProxyRequest struct {
	PublicKey string          `json:"public_key"`
	Path      string          `json:"path"`
	Method    string          `json:"method"`
	AuthType  int             `json:"auth_type"`
	Body      json.RawMessage `json:"body"`
}

// DeployAgentRequest is the Envy /api/v1/deploy/agent body. WalletAddress is
// the deployer/owner wallet.
type DeployAgentRequest struct {
	WalletAddress            string  `json:"walletAddress"`
	AgentName                string  `json:"agentName"`
	Symbol                   string  `json:"symbol"`
	AgentDescription         string  `json:"agentDescription"`
	AvatarUrl                string  `json:"avatarUrl"`
	RiskLevel                string  `json:"riskLevel"`
	InvestmentHorizon        string  `json:"investmentHorizon,omitempty"`
	CoinsToTrade             string  `json:"coinsToTrade,omitempty"`
	MaxSimultaneousPositions *int    `json:"maxSimultaneousPositions,omitempty"`
	DegenEnergy              float64 `json:"degenEnergy"`
	TradingLevel             float64 `json:"tradingLevel"`
}

// DeployAgentResponse is the Envy deploy result. Deployment is asynchronous;
// Status starts as "pending".
type DeployAgentResponse struct {
	Success      bool   `json:"success"`
	AgentID      string `json:"agentId"`
	DeploymentID string `json:"deploymentId"`
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
	Error        string `json:"error,omitempty"`
}

// AgentDepositRequest is the Envy /api/v1/agents/deposit body. Either AgentID
// or BotID identifies the target vault; Amount is in USDC.
type AgentDepositRequest struct {
	WalletAddress string  `json:"walletAddress"`
	AgentID       string  `json:"agentId,omitempty"`
	BotID         string  `json:"botId,omitempty"`
	Amount        float64 `json:"amount"`
}

// AgentDepositResponse is the proxy deposit acknowledgement. The on-chain
// settlement is tracked separately via the transaction status endpoint.
type AgentDepositResponse struct {
	Success       bool   `json:"success"`
	TransactionID string `json:"transactionId"`
	Error         string `json:"error,omitempty"`
}

// AgentTransactionStatusRequest is the Envy /api/v1/agents/transaction/status body.
type AgentTransactionStatusRequest struct {
	WalletAddress string `json:"walletAddress"`
	TransactionID string `json:"transactionId"`
}

// AgentTransactionStatusResponse reports the settlement state of a vault
// deposit or withdrawal. Shares/SharePrice/TransactionHash are populated once
// the transaction reaches a terminal state.
type AgentTransactionStatusResponse struct {
	Success         bool     `json:"success"`
	Status          string   `json:"status"`
	Error           string   `json:"error,omitempty"`
	Message         string   `json:"message,omitempty"`
	Amount          *float64 `json:"amount,omitempty"`
	SharePrice      *float64 `json:"sharePrice,omitempty"`
	Shares          *float64 `json:"shares,omitempty"`
	Type            string   `json:"type,omitempty"`
	TransactionID   string   `json:"transactionId"`
	TransactionHash *string  `json:"transactionHash,omitempty"`
}

// AgentTransactionsRequest is the Envy /api/v1/agents/transactions body. The
// agent scope is REQUIRED — without agentId the endpoint answers 404 "Agent
// not found in your house" (probed live 2026-06-12). The legacy
// /api/sdk/agent/transactions path 404s upstream, like the rest of /api/sdk/*.
type AgentTransactionsRequest struct {
	WalletAddress string `json:"walletAddress"`
	AgentID       string `json:"agentId"`
	Page          int    `json:"page,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

// AgentTransaction is one vault money movement (deposit or withdrawal) for the
// caller in one agent. ID is the same transaction id that
// /api/v1/agents/transaction/status accepts — listing transactions is the
// recovery path when a withdraw/deposit acknowledgement was lost client-side
// (e.g. the MCP client timed out before the ack arrived).
type AgentTransaction struct {
	ID              string  `json:"id"`
	Type            string  `json:"type"` // DEPOSIT | WITHDRAWAL
	Amount          float64 `json:"amount"`
	Shares          float64 `json:"shares"`
	SharePrice      float64 `json:"sharePrice"`
	Status          string  `json:"status"` // PENDING / PENDING_APPROVAL / QUEUED / COMPLETED / FAILED / ...
	TransactionHash *string `json:"transactionHash,omitempty"`
	Notes           *string `json:"notes,omitempty"`
	CreatedAt       string  `json:"createdAt"`
}

// AgentTransactionsPagination is the list paging block ({total, page, limit,
// totalPages}).
type AgentTransactionsPagination struct {
	Total      int `json:"total"`
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	TotalPages int `json:"totalPages"`
}

// AgentTransactionsResponse is the /api/v1/agents/transactions result, newest
// first.
type AgentTransactionsResponse struct {
	Success      bool                        `json:"success"`
	Transactions []AgentTransaction          `json:"transactions"`
	Pagination   AgentTransactionsPagination `json:"pagination"`
	Error        string                      `json:"error,omitempty"`
}

// AgreementStatusResponse is returned by GET /v1/ai/agreement/status and
// POST /v1/ai/agreement/verify. Message is the exact text the wallet must sign;
// Signed reports whether the risk-disclosure agreement is already on file.
type AgreementStatusResponse struct {
	Message       string `json:"message"`
	PackedMessage string `json:"packed_message"`
	Signed        bool   `json:"signed"`
	SignedAt      *int64 `json:"signed_at,omitempty"`
}

// VerifyAgreementRequest is the POST /v1/ai/agreement/verify body. Signature is
// the wallet's ed25519 signature over the agreement Message, base58-encoded
// (must decode to exactly 64 bytes).
type VerifyAgreementRequest struct {
	PublicKey string `json:"public_key"`
	Signature string `json:"signature"`
}

// UserInfoResponse is the Envy /api/v1/users/info result (via the proxy). It
// carries the user's custodied Main Account address (solanaMasterWallet), which
// is the on-chain destination for a wallet -> Main Account USDC transfer.
type UserInfoResponse struct {
	Success             bool   `json:"success"`
	UserExists          bool   `json:"userExists"`
	BelongsToHouse      bool   `json:"belongsToHouse"`
	IsNewUser           bool   `json:"isNewUser"`
	UserID              string `json:"userId"`
	HouseID             string `json:"houseId"`
	SolanaMasterWallet  string `json:"solanaMasterWallet"`
	MasterWalletAddress string `json:"masterWalletAddress"`
}

// DashboardOverviewResponse is the Envy /api/v1/dashboard/overview result (via
// the proxy). UsdcBalance is the spendable Main Account balance.
type DashboardOverviewResponse struct {
	Success             bool       `json:"success"`
	WalletAddress       string     `json:"walletAddress"`
	UsdcBalance         float64    `json:"usdcBalance"`
	MyAgentCount        int        `json:"myAgentCount"`
	ActiveAgentCount    int        `json:"activeAgentCount"`
	TotalPortfolioValue float64    `json:"totalPortfolioValue"`
	TopPerformer        *AgentInfo `json:"topPerformer,omitempty"`
}

// ---------------------------------------------------------------------------
// AI Agent stats (Envy arena/details/positions/shares, via /v1/ai/proxy)
// ---------------------------------------------------------------------------

// ArenaDataPoint is a single point in an arena chart series.
type ArenaDataPoint struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
}

// ArenaChartData is one time-frame's worth of chart points for an arena bot.
type ArenaChartData struct {
	TimeFrame           string           `json:"timeFrame"`
	DataPointCount      int              `json:"dataPointCount"`
	DataPoints          []ArenaDataPoint `json:"dataPoints"`
	LastUpdated         string           `json:"lastUpdated"`
	LatestPnlPercentage float64          `json:"latestPnlPercentage"`
}

// ArenaBot is one agent row in the GET /api/data/arena leaderboard. ChartData,
// LatestReport and StrategyMetrics are heavy payloads — the list_agents tool
// strips them unless explicitly requested.
type ArenaBot struct {
	ID                 string           `json:"id"`
	Name               string           `json:"name"`
	Avatar             string           `json:"avatar,omitempty"`
	Type               string           `json:"type,omitempty"`
	RiskLevel          string           `json:"riskLevel"`
	AUM                float64          `json:"aum"`
	WalletCount        int              `json:"walletCount"`
	ActivePositions    int              `json:"activePositions"`
	PnlPercentage1h    float64          `json:"pnlPercentage1h"`
	PnlPercentage24h   float64          `json:"pnlPercentage24h"`
	PnlPercentage7d    float64          `json:"pnlPercentage7d"`
	PnlPercentage30d   float64          `json:"pnlPercentage30d"`
	TotalPnlPercentage float64          `json:"totalPnlPercentage"`
	TotalPnl           float64          `json:"totalPnl"`
	HasFreshMetrics    bool             `json:"hasFreshMetrics"`
	ChartData          []ArenaChartData `json:"chartData,omitempty"`
	// LatestReport is arbitrary JSON (the API may send a string or an object).
	LatestReport    json.RawMessage `json:"latestReport,omitempty"`
	StrategyMetrics json.RawMessage `json:"strategyMetrics,omitempty"`
}

// ArenaPagination is the arena pagination object (camelCase on the wire).
type ArenaPagination struct {
	CurrentPage int `json:"currentPage"`
	PageSize    int `json:"pageSize"`
	TotalPages  int `json:"totalPages"`
	TotalItems  int `json:"totalItems"`
}

// ArenaMetadata reports arena data freshness.
type ArenaMetadata struct {
	HasStaleData bool   `json:"hasStaleData"`
	LastUpdated  string `json:"lastUpdated"`
}

// ArenaResponse is the full GET /api/data/arena leaderboard response.
type ArenaResponse struct {
	Success    bool             `json:"success"`
	Bots       []ArenaBot       `json:"bots"`
	Pagination *ArenaPagination `json:"pagination,omitempty"`
	Metadata   *ArenaMetadata   `json:"metadata,omitempty"`
	Error      string           `json:"error,omitempty"`
}

// AgentInfo is the Envy agent profile embedded in details/positions/overview
// responses. Some contexts return only a subset of fields.
type AgentInfo struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Avatar              string   `json:"avatar,omitempty"`
	Description         string   `json:"description,omitempty"`
	Type                string   `json:"type,omitempty"`
	RiskLevel           string   `json:"riskLevel,omitempty"`
	CoinsToTrade        []string `json:"coinsToTrade,omitempty"`
	DegenEnergy         float64  `json:"degenEnergy,omitempty"`
	TradingLevel        float64  `json:"tradingLevel,omitempty"`
	IsDeployer          bool     `json:"isDeployer,omitempty"`
	PnlPercentage1h     float64  `json:"pnlPercentage1h"`
	PnlPercentage24h    float64  `json:"pnlPercentage24h"`
	PnlPercentage7d     float64  `json:"pnlPercentage7d"`
	PnlPercentage30d    float64  `json:"pnlPercentage30d"`
	TotalPnlPercentage  float64  `json:"totalPnlPercentage"`
	StrategyDescription string   `json:"strategyDescription,omitempty"`
}

// UserSharesInfo is the caller's stake in one agent vault. Balance is the
// current USD value of the shares (shares * sharePrice).
type UserSharesInfo struct {
	Shares            float64 `json:"shares"`
	SharePrice        float64 `json:"sharePrice"`
	Balance           float64 `json:"balance,omitempty"`
	CurrentValue      float64 `json:"currentValue,omitempty"`
	PercentageOfVault float64 `json:"percentageOfVault"`
	LastUpdated       string  `json:"lastUpdated,omitempty"`
}

// VaultInfo is an agent vault's aggregate state. TotalAssets is the AUM in USDC.
type VaultInfo struct {
	VaultAddress  string  `json:"vaultAddress,omitempty"`
	TotalShares   float64 `json:"totalShares"`
	TotalAssets   float64 `json:"totalAssets"`
	SharePrice    float64 `json:"sharePrice"`
	InvestorCount int     `json:"investorCount"`
	IsActive      bool    `json:"isActive,omitempty"`
	Status        string  `json:"status,omitempty"`
}

// ClosedPositionsStats summarises an agent's closed trades.
type ClosedPositionsStats struct {
	Wins             int     `json:"wins"`
	Losses           int     `json:"losses"`
	PositionsClosed  int     `json:"positionsClosed"`
	AvgPnlPercent    float64 `json:"avgPnlPercent"`
	AvgWinPnlPercent float64 `json:"avgWinPnlPercent"`
	ProfitFactor     float64 `json:"profitFactor"`
}

// OpenPositionsStats summarises an agent's currently open trades.
type OpenPositionsStats struct {
	PositionsOpen int     `json:"positionsOpen"`
	AvgPnlPercent float64 `json:"avgPnlPercent"`
	TotalMargin   float64 `json:"totalMargin"`
	TotalPnlUsd   float64 `json:"totalPnlUsd"`
}

// AumChartPoint is one point of an agent's AUM time series.
type AumChartPoint struct {
	Timestamp   string  `json:"timestamp"`
	TotalAssets float64 `json:"totalAssets"`
}

// PnlChartPoint is one point of an agent's PnL time series.
type PnlChartPoint struct {
	Timestamp string  `json:"timestamp"`
	PnlUsd    float64 `json:"pnlUsd"`
}

// AgentAnalytics is the analytics block of the agent details response. The
// chart series are heavy — get_agent_details strips them unless asked.
type AgentAnalytics struct {
	AumChart             []AumChartPoint      `json:"aumChart,omitempty"`
	PnlChart             []PnlChartPoint      `json:"pnlChart,omitempty"`
	ClosedPositionsStats ClosedPositionsStats `json:"closedPositionsStats"`
	OpenPositionsStats   OpenPositionsStats   `json:"openPositionsStats"`
}

// LatestReport is metadata for an agent's most recent published report file.
type LatestReport struct {
	FileMimeType string `json:"fileMimeType"`
	FileName     string `json:"fileName"`
	FileSize     int64  `json:"fileSize"`
	FileUrl      string `json:"fileUrl"`
}

// AgentDetailsResponse is the POST /api/v1/agents/details result. UserShares
// reflects the walletAddress in the request body.
type AgentDetailsResponse struct {
	Success      bool           `json:"success"`
	Agent        AgentInfo      `json:"agent"`
	UserShares   UserSharesInfo `json:"userShares"`
	Vault        VaultInfo      `json:"vault"`
	Analytics    AgentAnalytics `json:"analytics"`
	LatestReport *LatestReport  `json:"latestReport,omitempty"`
	Error        string         `json:"error,omitempty"`
}

// AgentPosition is one trade in the POST /api/v1/agents/positions result.
type AgentPosition struct {
	ID              string  `json:"id"`
	Type            string  `json:"type"` // long | short
	Token           string  `json:"token"`
	TotalUsd        float64 `json:"totalUsd"`
	Amount          float64 `json:"amount"`
	Price           float64 `json:"price"`
	EntryPrice      float64 `json:"entryPrice"`
	PnlPercent      float64 `json:"pnlPercent"`
	Leverage        int     `json:"leverage"`
	StopLoss        float64 `json:"stopLoss"`
	TakeProfit      float64 `json:"takeProfit"`
	Status          string  `json:"status"`
	Active          bool    `json:"active"`
	Age             string  `json:"age"`
	Live            bool    `json:"live"`
	ExternalOrderId string  `json:"externalOrderId,omitempty"`
}

// PositionsPagination is the positions pagination object.
type PositionsPagination struct {
	Total      int `json:"total"`
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	TotalPages int `json:"totalPages"`
}

// AgentPositionsResponse is the POST /api/v1/agents/positions result.
type AgentPositionsResponse struct {
	Success    bool                `json:"success"`
	Agent      AgentInfo           `json:"agent"`
	Positions  []AgentPosition     `json:"positions"`
	Pagination PositionsPagination `json:"pagination"`
	Error      string              `json:"error,omitempty"`
}

// AgentSharesResponse is the POST /api/v1/agents/shares result — the caller's
// stake in one agent plus the vault aggregate.
type AgentSharesResponse struct {
	Success    bool           `json:"success"`
	UserShares UserSharesInfo `json:"userShares"`
	Vault      VaultInfo      `json:"vault"`
	Error      string         `json:"error,omitempty"`
}

// MyAgentSummary is one entry of the POST /api/v1/agents/list result: an agent
// the wallet holds shares in, with the position's value and the agent's PnL.
// PnlChart and LatestReport are heavy — get_my_portfolio strips them.
type MyAgentSummary struct {
	Agent              AgentInfo       `json:"agent"`
	Shares             float64         `json:"shares"`
	SharePrice         float64         `json:"sharePrice"`
	Balance            float64         `json:"balance"` // current USD value of the shares
	PercentageOfVault  float64         `json:"percentageOfVault"`
	RiskLevel          string          `json:"riskLevel,omitempty"`
	PnlPercentage1h    float64         `json:"pnlPercentage1h"`
	PnlPercentage24h   float64         `json:"pnlPercentage24h"`
	PnlPercentage7d    float64         `json:"pnlPercentage7d"`
	PnlPercentage30d   float64         `json:"pnlPercentage30d"`
	TotalPnlPercentage float64         `json:"totalPnlPercentage"`
	CreatedAt          string          `json:"createdAt,omitempty"`
	LastUpdated        string          `json:"lastUpdated,omitempty"`
	PnlChart           []PnlChartPoint `json:"pnlChart,omitempty"`
	LatestReport       *LatestReport   `json:"latestReport,omitempty"`
}

// MyAgentsResponse is the POST /api/v1/agents/list result — every agent the
// wallet currently holds vault shares in.
type MyAgentsResponse struct {
	Success bool             `json:"success"`
	Agents  []MyAgentSummary `json:"agents"`
	Error   string           `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// AI Agent withdrawals (Envy-backed, via /v1/ai/proxy)
// ---------------------------------------------------------------------------

// flexFloat is a float64 that tolerates string-encoded numbers on the wire.
// The Envy master-withdraw response models amount/newMasterBalance as strings
// on the backend but numbers on the frontend, so decode both.
type flexFloat float64

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("flexFloat: parse %q: %w", string(b), err)
	}
	*f = flexFloat(v)
	return nil
}

// Float64 returns the plain float value.
func (f flexFloat) Float64() float64 { return float64(f) }

// AgentWithdrawRequest is the Envy /api/v1/agents/withdraw body. Shares is
// denominated in VAULT SHARES (not USDC); proceeds land in the Main Account.
type AgentWithdrawRequest struct {
	WalletAddress string  `json:"walletAddress"`
	AgentID       string  `json:"agentId,omitempty"`
	BotID         string  `json:"botId,omitempty"`
	Shares        float64 `json:"shares"`
}

// AgentWithdrawResponse is the agent-withdraw acknowledgement. Queued=true
// means Envy deferred execution (vault liquidity is tied up in positions) —
// keep polling the transaction status. Settlement is tracked via the same
// /api/v1/agents/transaction/status endpoint deposits use.
type AgentWithdrawResponse struct {
	Success         bool   `json:"success"`
	TransactionID   string `json:"transactionId"`
	Queued          bool   `json:"queued"`
	QueuedRequestID string `json:"queuedRequestId,omitempty"`
	Error           string `json:"error,omitempty"`
	Message         string `json:"message,omitempty"`
}

// MasterWithdrawRequest is the Envy /api/v1/users/withdraw/solana body.
// Amount is in USDC. The destination is ALWAYS the registered wallet —
// there is no destination parameter.
//
// NOTE: Envy migrated its house API from /api/sdk/* to /api/v1/*; the legacy
// /api/sdk/withdraw/solana path 404s upstream. The live path was verified by
// probing on 2026-06-10.
type MasterWithdrawRequest struct {
	WalletAddress string  `json:"walletAddress"`
	Amount        float64 `json:"amount"`
}

// MasterWithdrawResponse is the master-withdraw result. The operation is
// fully server-side and synchronous — the on-chain transfer is already
// submitted when this returns. Amount/NewMasterBalance arrive as strings from
// the backend but numbers from other surfaces, hence flexFloat.
type MasterWithdrawResponse struct {
	Success            bool      `json:"success"`
	TransactionHash    string    `json:"transactionHash"`
	Amount             flexFloat `json:"amount"`
	NewMasterBalance   flexFloat `json:"newMasterBalance"`
	DestinationChain   string    `json:"destinationChain"`
	DestinationAddress string    `json:"destinationAddress"`
	Error              string    `json:"error,omitempty"`
}
