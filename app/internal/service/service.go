package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"mcp-server/app/internal/clients/orderly"
	"mcp-server/app/internal/clients/perptools"
	"mcp-server/app/internal/service/auth"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

type Config struct {
	PerptoolsBaseURL string
	OrderlyBaseURL   string
	BrokerID         string
	SolanaRPCURL     string
}

type Service struct {
	cfg            Config
	auth           *auth.Service
	orderly        orderly.Client
	orderlyPrivate orderly.PrivateClient
	perptools      perptools.Client
	solanaRPC      *rpc.Client
}

func NewService(cfg Config) *Service {
	orderlyClient := orderly.NewClient(orderly.Config{BaseURL: cfg.OrderlyBaseURL})
	authSvc := auth.NewService(auth.Config{BrokerID: cfg.BrokerID}, orderlyClient)

	s := &Service{
		cfg:     cfg,
		auth:    authSvc,
		orderly: orderlyClient,
	}
	s.perptools = perptools.NewClient(cfg.PerptoolsBaseURL)

	if cfg.SolanaRPCURL != "" {
		s.solanaRPC = rpc.New(cfg.SolanaRPCURL)
	}

	return s
}

func (s *Service) rebuildAuthenticatedClients() {
	creds := s.auth.GetCredentials()
	if creds == nil || creds.OrderlyPrivateKey == nil {
		return
	}

	s.perptools = perptools.NewClientWithAuth(
		s.cfg.PerptoolsBaseURL,
		creds.AccountID,
		creds.OrderlyPublicKey,
		creds.OrderlyPrivateKey,
	)

	s.orderlyPrivate = orderly.NewPrivateClient(
		s.cfg.OrderlyBaseURL,
		creds.AccountID,
		creds.OrderlyPublicKey,
		creds.OrderlyPrivateKey,
	)
}

// --- Auth ---

func (s *Service) IsAuthenticated() bool {
	return s.auth.IsAuthenticated()
}

func (s *Service) PrepareRegistration(ctx context.Context, walletAddress string) (*auth.PrepareResult, error) {
	return s.auth.PrepareRegistration(ctx, walletAddress)
}

func (s *Service) CompleteRegistration(ctx context.Context, walletAddress, signature string) (string, error) {
	if creds := s.auth.GetCredentials(); creds != nil && creds.AccountID != "" {
		return creds.AccountID, nil
	}
	if err := s.auth.CompleteRegistration(ctx, walletAddress, signature); err != nil {
		return "", err
	}
	return s.auth.GetCredentials().AccountID, nil
}

func (s *Service) PrepareOrderlyKey(ctx context.Context, walletAddress string) (*auth.PrepareResult, error) {
	return s.auth.PrepareOrderlyKey(ctx, walletAddress)
}

func (s *Service) CompleteOrderlyKey(ctx context.Context, walletAddress, signature string) error {
	if err := s.auth.CompleteOrderlyKey(ctx, walletAddress, signature); err != nil {
		return err
	}
	s.rebuildAuthenticatedClients()
	return nil
}

// --- Perptools (public) ---

func (s *Service) Health(ctx context.Context) (*perptools.HealthResponse, error) {
	return s.perptools.Health(ctx)
}

func (s *Service) GetMarkets(ctx context.Context, limit, offset int32) (*perptools.MarketsResponse, error) {
	return s.perptools.GetMarkets(ctx, limit, offset)
}

// --- Perptools (authenticated) ---

func (s *Service) GetUserPoints(ctx context.Context, publicKey string) (*perptools.UserPoints, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.perptools.GetUserPoints(ctx, publicKey)
}

func (s *Service) GetLeaderboard(ctx context.Context, publicKey string, limit, offset int32) ([]perptools.LeaderboardEntry, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.perptools.GetLeaderboard(ctx, publicKey, limit, offset)
}

// --- Orderly Trading (orders, positions) ---

func (s *Service) CreateOrder(ctx context.Context, req orderly.CreateOrderRequest) (*orderly.CreateOrderResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.orderlyPrivate.CreateOrder(ctx, req)
}

func (s *Service) CancelOrder(ctx context.Context, symbol string, orderID int) (*orderly.CancelOrderResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.orderlyPrivate.CancelOrder(ctx, symbol, orderID)
}

func (s *Service) GetPositions(ctx context.Context) (*orderly.PositionsResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.orderlyPrivate.GetPositions(ctx)
}

func (s *Service) GetSettleNonce(ctx context.Context) (*orderly.SettleNonceResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.orderlyPrivate.GetSettleNonce(ctx)
}

func (s *Service) SetPositionTPSL(ctx context.Context, symbol string, takeProfitPrice, stopLossPrice float64) (*orderly.PlaceAlgoOrderResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	positions, err := s.orderlyPrivate.GetPositions(ctx)
	if err != nil {
		return nil, err
	}
	var pos *orderly.Position
	for i := range positions.Data.Rows {
		if positions.Data.Rows[i].Symbol == symbol {
			pos = &positions.Data.Rows[i]
			break
		}
	}
	if pos == nil || pos.PositionQty == 0 {
		return nil, fmt.Errorf("no open position for %s — open a position first before setting TP/SL", symbol)
	}
	side := "SELL"
	if pos.PositionQty < 0 {
		side = "BUY"
	}
	req := orderly.PlaceAlgoOrderRequest{
		Symbol:           symbol,
		AlgoType:         "POSITIONAL_TP_SL",
		TriggerPriceType: "MARK_PRICE",
		ChildOrders: []orderly.AlgoChildOrder{
			{Symbol: symbol, AlgoType: "TAKE_PROFIT", Side: side, OrderType: "CLOSE_POSITION", TriggerPriceType: "MARK_PRICE", TriggerPrice: takeProfitPrice, ReduceOnly: true},
			{Symbol: symbol, AlgoType: "STOP_LOSS", Side: side, OrderType: "CLOSE_POSITION", TriggerPriceType: "MARK_PRICE", TriggerPrice: stopLossPrice, ReduceOnly: true},
		},
	}
	return s.orderlyPrivate.PlaceAlgoOrder(ctx, req)
}

func (s *Service) CancelAlgoOrder(ctx context.Context, symbol string, algoOrderID int) error {
	if err := s.requireAuth(); err != nil {
		return err
	}
	return s.orderlyPrivate.CancelAlgoOrder(ctx, symbol, algoOrderID)
}

func (s *Service) GetAlgoOrders(ctx context.Context, symbol string) (*orderly.GetAlgoOrdersResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.orderlyPrivate.GetAlgoOrders(ctx, symbol)
}

// --- Orderly Vault (deposit / withdraw) ---

type DepositResult struct {
	TransactionBase64 string `json:"transaction_base64"`
	WalletAddress     string `json:"wallet_address"`
	Symbol            string `json:"symbol"`
	Amount            uint64 `json:"amount"`
	NativeFee         uint64 `json:"native_fee"`
}

type WithdrawResult struct {
	TransactionBase64 string `json:"transaction_base64"`
	WalletAddress     string `json:"wallet_address"`
	Token             string `json:"token"`
	DebugHash         string `json:"debug_hash"`
}

func (s *Service) PrepareOrderlyDeposit(ctx context.Context, walletAddress, symbol string, amount uint64) (*DepositResult, error) {
	if s.solanaRPC == nil {
		return nil, fmt.Errorf("solana RPC not configured — set SOLANA_RPC_URL")
	}

	userKey, err := solana.PublicKeyFromBase58(walletAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet address: %w", err)
	}

	nativeFee, err := orderly.GetDepositQuoteFee(ctx, s.solanaRPC, s.cfg.BrokerID, symbol, userKey, amount)
	if err != nil {
		return nil, fmt.Errorf("get deposit quote fee: %w", err)
	}

	recent, err := s.solanaRPC.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("get blockhash: %w", err)
	}

	tx, err := orderly.Deposit(
		s.cfg.BrokerID,
		symbol,
		userKey,
		amount,
		orderly.OAppSendParams{NativeFee: nativeFee, LzTokenFee: 0},
		recent.Value.Blockhash,
	)
	if err != nil {
		return nil, fmt.Errorf("build deposit tx: %w", err)
	}

	txBase64, err := tx.ToBase64()
	if err != nil {
		return nil, fmt.Errorf("serialize tx: %w", err)
	}

	return &DepositResult{
		TransactionBase64: txBase64,
		WalletAddress:     walletAddress,
		Symbol:            symbol,
		Amount:            amount,
		NativeFee:         nativeFee,
	}, nil
}

func (s *Service) PrepareOrderlyWithdraw(ctx context.Context, walletAddress, token string, amount uint64) (*WithdrawResult, error) {
	if s.orderlyPrivate == nil {
		return nil, fmt.Errorf("orderly key not set — call prepare_orderly_key, then complete_orderly_key first")
	}
	userKey, err := solana.PublicKeyFromBase58(walletAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet address: %w", err)
	}

	nonceResp, err := s.orderlyPrivate.GetSettleNonce(ctx)
	if err != nil {
		return nil, fmt.Errorf("get withdraw nonce: %w", err)
	}
	withdrawNonce := nonceResp.Data.SettleNonce

	withdrawMsg := orderly.WithdrawMessage{
		BrokerID:      s.cfg.BrokerID,
		ChainID:       900900900,
		Receiver:      walletAddress,
		Token:         token,
		Amount:        amount,
		WithdrawNonce: withdrawNonce,
		Timestamp:     uint64(time.Now().UTC().UnixMilli()),
		ChainType:     "SOL",
	}

	signMessage, err := orderly.CreateWithdrawMessage(withdrawMsg)
	if err != nil {
		return nil, fmt.Errorf("create withdraw message: %w", err)
	}

	tx, err := orderly.PackMessageForSolana(userKey, signMessage)
	if err != nil {
		return nil, fmt.Errorf("pack message for solana: %w", err)
	}

	txBytes, err := tx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("serialize tx: %w", err)
	}

	return &WithdrawResult{
		TransactionBase64: base64.StdEncoding.EncodeToString(txBytes),
		WalletAddress:     walletAddress,
		Token:             token,
		DebugHash:         string(signMessage),
	}, nil
}

func (s *Service) requireAuth() error {
	if s.auth.IsAuthenticated() {
		return nil
	}
	creds := s.auth.GetCredentials()
	if creds == nil || creds.AccountID == "" {
		return fmt.Errorf("not authenticated — start by calling prepare_registration with your wallet_address")
	}
	return fmt.Errorf("account registered (account_id: %s) but orderly key not set — call prepare_orderly_key, then complete_orderly_key", creds.AccountID)
}
