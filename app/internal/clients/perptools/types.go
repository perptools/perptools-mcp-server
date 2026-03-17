package perptools

import (
	"encoding/json"
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

