package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"

	"mcp-server/app/internal/clients/orderly"
	"mcp-server/app/internal/clients/perptools"
	"mcp-server/app/internal/clients/solfund"
	"mcp-server/app/internal/service/auth"
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
	solfund        *solfund.Client
}

func NewService(cfg Config) *Service {
	orderlyClient := orderly.NewClient(orderly.Config{BaseURL: cfg.OrderlyBaseURL})
	authSvc := auth.NewService(auth.Config{BrokerID: cfg.BrokerID}, orderlyClient)

	s := &Service{
		cfg:     cfg,
		auth:    authSvc,
		orderly: orderlyClient,
		solfund: solfund.New(cfg.SolanaRPCURL),
	}
	s.perptools = perptools.NewClient(cfg.PerptoolsBaseURL)

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

// AuthenticatedWallet returns the wallet address of the fully authenticated
// user (registration + orderly key complete), or "" when unauthenticated.
// Used by telemetry to attribute usage events.
func (s *Service) AuthenticatedWallet() string {
	if !s.auth.IsAuthenticated() {
		return ""
	}
	creds := s.auth.GetCredentials()
	if creds == nil {
		return ""
	}
	return creds.WalletAddress
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

func (s *Service) GetLeaderboard(ctx context.Context, publicKey string) (*perptools.LeaderboardStanding, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.perptools.GetLeaderboard(ctx, publicKey)
}

// --- AI Agents (Envy-backed, via the AI proxy) ---

// DeployAgent deploys a new AI trading agent owned by req.WalletAddress.
func (s *Service) DeployAgent(ctx context.Context, req perptools.DeployAgentRequest) (*perptools.DeployAgentResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.perptools.DeployAgent(ctx, req.WalletAddress, req)
}

// AgreementStatus returns whether the wallet has signed the risk-disclosure
// agreement, along with the message it must sign if not. A signed agreement is
// a precondition for depositing into an agent.
func (s *Service) AgreementStatus(ctx context.Context, wallet string) (*perptools.AgreementStatusResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.perptools.GetAgreementStatus(ctx, wallet)
}

// SignAgreement records the wallet's signature over the risk-disclosure
// agreement message. signature is the base58-encoded ed25519 signature of the
// message returned by AgreementStatus.
func (s *Service) SignAgreement(ctx context.Context, wallet, signature string) (*perptools.AgreementStatusResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.perptools.VerifyAgreement(ctx, wallet, signature)
}

// PrepareMainDepositViaProxy builds the wallet -> Main Account deposit
// transaction through the orderly-signed Envy proxy (server-side HMAC), instead
// of the browser-only session gateway.

// MainDepositPlan is what PrepareMainDeposit hands back: the on-chain USDC
// transfer (wallet -> Main Account) the wallet must sign and submit.
type MainDepositPlan struct {
	// MasterWallet is the custodied Main Account address (the transfer recipient).
	MasterWallet string `json:"master_wallet"`
	// Amount is the USDC amount being moved into the Main Account.
	Amount float64 `json:"amount"`
	// UnsignedTransaction is the base64 Solana transaction to sign with the
	// user's wallet, then submit via CompleteMainDeposit.
	UnsignedTransaction string `json:"unsigned_transaction"`
}

// PrepareMainDeposit builds the wallet -> Main Account top-up as a plain on-chain
// USDC transfer to the user's custodied master wallet (resolved via the orderly
// proxy). This is the MCP-reachable top-up: no browser session gateway. It
// returns an UNSIGNED base64 transaction for the wallet to sign.
func (s *Service) PrepareMainDeposit(ctx context.Context, wallet string, amount float64) (*MainDepositPlan, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	if amount <= 0 {
		return nil, fmt.Errorf("amount must be > 0")
	}
	master, err := s.GetMasterWallet(ctx, wallet)
	if err != nil {
		return nil, err
	}
	walletPub, err := solfund.ParsePublicKey(wallet)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet address: %w", err)
	}
	masterPub, err := solfund.ParsePublicKey(master)
	if err != nil {
		return nil, fmt.Errorf("invalid master address: %w", err)
	}
	tx, err := s.solfund.BuildUSDCTransfer(ctx, walletPub, masterPub, solfund.ToRaw(amount))
	if err != nil {
		return nil, fmt.Errorf("build deposit transaction: %w", err)
	}
	b64, err := tx.ToBase64()
	if err != nil {
		return nil, fmt.Errorf("encode transaction: %w", err)
	}
	return &MainDepositPlan{MasterWallet: master, Amount: amount, UnsignedTransaction: b64}, nil
}

// CompleteMainDeposit submits the signed wallet -> Main Account USDC transfer to
// the Solana network and waits for confirmation. Returns the transaction
// signature. Envy credits the Main Account from the on-chain balance.
func (s *Service) CompleteMainDeposit(ctx context.Context, signedTransaction string) (string, error) {
	if err := s.requireAuth(); err != nil {
		return "", err
	}
	sig, err := s.solfund.SubmitBase64(ctx, signedTransaction)
	if err != nil {
		return "", err
	}
	return sig.String(), nil
}

// --- Orderly trading-vault deposit ---
//
// This is a SEPARATE money path from the AI-agent Main Account above. The
// Orderly vault is the collateral the user trades with directly (create_order,
// set_position_tp_sl, ...). Funding it is an on-chain LayerZero deposit into the
// Orderly Solana vault program — distinct from the plain SPL transfer that tops
// up the Main Account.

// DepositResult is an unsigned Orderly-vault deposit transaction, ready for the
// user's wallet to sign and then broadcast via CompleteOrderlyDeposit.
type DepositResult struct {
	TransactionBase64 string `json:"transaction_base64"`
	WalletAddress     string `json:"wallet_address"`
	Symbol            string `json:"symbol"`
	Amount            uint64 `json:"amount"`
	NativeFee         uint64 `json:"native_fee"`
}

// PrepareOrderlyDeposit builds an unsigned Solana transaction that deposits
// `amount` (in token base units) of `symbol` into the user's ORDERLY TRADING
// vault via LayerZero. The cross-chain messaging fee is quoted on-chain
// (oappQuote simulation) and folded into the transaction. The wallet signs the
// returned base64 and broadcasts it with CompleteOrderlyDeposit.
func (s *Service) PrepareOrderlyDeposit(ctx context.Context, walletAddress, symbol string, amount uint64) (*DepositResult, error) {
	if s.cfg.SolanaRPCURL == "" {
		return nil, fmt.Errorf("solana RPC not configured — set SOLANA_RPC_URL")
	}
	rpcClient := s.solfund.RPC()

	userKey, err := solana.PublicKeyFromBase58(walletAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet address: %w", err)
	}

	nativeFee, err := orderly.GetDepositQuoteFee(ctx, rpcClient, s.cfg.BrokerID, symbol, userKey, amount)
	if err != nil {
		return nil, fmt.Errorf("get deposit quote fee: %w", err)
	}

	recent, err := rpcClient.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("get blockhash: %w", err)
	}

	tx, err := orderly.Deposit(
		ctx,
		rpcClient,
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

// CompleteOrderlyDeposit broadcasts the signed Orderly-vault deposit transaction
// (the base64 the wallet signed from PrepareOrderlyDeposit) to Solana and
// returns the transaction signature. Mirrors CompleteMainDeposit, but funds the
// trading vault rather than the Main Account; no auth is required because the
// wallet's own signature already authorises the transfer.
func (s *Service) CompleteOrderlyDeposit(ctx context.Context, signedTransaction string) (string, error) {
	sig, err := s.solfund.SubmitBase64(ctx, signedTransaction)
	if err != nil {
		return "", err
	}
	return sig.String(), nil
}

// --- Orderly trading-vault withdrawal ---
//
// Withdrawing from the Orderly trading vault is a signed Orderly-API request,
// not an on-chain LayerZero message. PrepareOrderlyWithdraw fetches the withdraw
// nonce, builds the canonical sign message + a Solana memo transaction for the
// wallet to sign; SubmitOrderlyWithdraw posts the signature to Orderly. Requires
// the orderly key (prepare_orderly_key + complete_orderly_key).

// WithdrawResult is the prepared Orderly withdrawal: the API message to echo
// back to submit_orderly_withdraw, plus the Solana memo tx for the wallet to sign.
type WithdrawResult struct {
	Message           orderly.WithdrawRequestMessage `json:"message"`
	TransactionBase64 string                         `json:"transaction_base64"`
	WalletAddress     string                         `json:"wallet_address"`
	Token             string                         `json:"token"`
}

// PrepareOrderlyWithdraw builds the sign message + Solana memo transaction for a
// withdrawal from the user's Orderly trading vault to their wallet. The wallet
// signs the returned transaction_base64; pass the signature plus the echoed
// message to SubmitOrderlyWithdraw.
func (s *Service) PrepareOrderlyWithdraw(ctx context.Context, walletAddress, token string, amount uint64) (*WithdrawResult, error) {
	if s.orderlyPrivate == nil {
		return nil, fmt.Errorf("orderly key not set — call prepare_orderly_key, then complete_orderly_key first")
	}

	userKey, err := solana.PublicKeyFromBase58(walletAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet address: %w", err)
	}

	nonceResp, err := s.orderlyPrivate.GetWithdrawNonce(ctx)
	if err != nil {
		return nil, fmt.Errorf("get withdraw nonce: %w", err)
	}
	withdrawNonce := nonceResp.Data.WithdrawNonce

	// Orderly API expects timestamp in UNIX milliseconds
	timestamp := uint64(time.Now().UTC().UnixMilli())

	withdrawMsg := orderly.WithdrawMessage{
		BrokerID:      s.cfg.BrokerID,
		ChainID:       900900900,
		Receiver:      walletAddress,
		Token:         token,
		Amount:        amount,
		WithdrawNonce: withdrawNonce,
		Timestamp:     timestamp,
		ChainType:     "SOL",
	}

	signMessage, err := orderly.CreateWithdrawMessage(withdrawMsg)
	if err != nil {
		return nil, fmt.Errorf("create withdraw message: %w", err)
	}

	// Empty blockhash — wallet fills in recent blockhash when user signs
	tx, err := orderly.PackMessageForSolana(userKey, signMessage, solana.Hash{})
	if err != nil {
		return nil, fmt.Errorf("pack message for solana: %w", err)
	}

	txBytes, err := tx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("serialize tx: %w", err)
	}

	// API message (all string fields per Orderly docs)
	apiMsg := orderly.WithdrawRequestMessage{
		BrokerID:      s.cfg.BrokerID,
		ChainID:       900900900,
		Receiver:      walletAddress,
		Token:         token,
		Amount:        strconv.FormatUint(amount, 10),
		WithdrawNonce: strconv.FormatUint(withdrawNonce, 10),
		Timestamp:     strconv.FormatUint(timestamp, 10),
		ChainType:     "SOL",
	}

	return &WithdrawResult{
		Message:           apiMsg,
		TransactionBase64: base64.StdEncoding.EncodeToString(txBytes),
		WalletAddress:     walletAddress,
		Token:             token,
	}, nil
}

// SubmitOrderlyWithdraw posts the signed withdrawal request to the Orderly API.
func (s *Service) SubmitOrderlyWithdraw(ctx context.Context, signature string, msg orderly.WithdrawRequestMessage) (*orderly.CreateWithdrawResponse, error) {
	if s.orderlyPrivate == nil {
		return nil, fmt.Errorf("orderly key not set — call prepare_orderly_key, then complete_orderly_key first")
	}

	req := orderly.CreateWithdrawRequest{
		Signature:         signature,
		UserAddress:       msg.Receiver,
		VerifyingContract: orderly.WithdrawVerifyingContract,
		Message:           msg,
	}

	return s.orderlyPrivate.CreateWithdrawRequest(ctx, req)
}

// Balances is a snapshot an agent uses to decide whether (and how much) to top
// up the Main Account before depositing into an agent.
type Balances struct {
	// WalletUSDC is the user's on-chain USDC (the source for a Main Account top-up).
	WalletUSDC float64 `json:"wallet_usdc"`
	// MainAccountUSDC is the spendable Main Account balance (the source deposit_to_agent uses).
	MainAccountUSDC float64 `json:"main_account_usdc"`
	// MasterWallet is the Main Account (custodied master) address.
	MasterWallet string `json:"master_wallet"`
}

// GetBalances reports the user's on-chain wallet USDC and their Main Account
// USDC, so an agent can decide whether a top-up is needed and for how much.
func (s *Service) GetBalances(ctx context.Context, wallet string) (*Balances, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	info, err := s.perptools.GetUserInfo(ctx, wallet)
	if err != nil {
		return nil, err
	}
	walletPub, err := solfund.ParsePublicKey(wallet)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet address: %w", err)
	}
	walRaw, err := s.solfund.USDCBalance(ctx, walletPub)
	if err != nil {
		return nil, fmt.Errorf("read wallet USDC balance: %w", err)
	}
	var mainUSDC float64
	if dash, err := s.perptools.GetDashboardOverview(ctx, wallet); err == nil {
		mainUSDC = dash.UsdcBalance
	}
	return &Balances{
		WalletUSDC:      float64(walRaw) / 1e6,
		MainAccountUSDC: mainUSDC,
		MasterWallet:    info.SolanaMasterWallet,
	}, nil
}

// GetMasterWallet returns the user's custodied Main Account (master) Solana
// address — the on-chain destination for a wallet -> Main Account USDC transfer.
func (s *Service) GetMasterWallet(ctx context.Context, wallet string) (string, error) {
	if err := s.requireAuth(); err != nil {
		return "", err
	}
	info, err := s.perptools.GetUserInfo(ctx, wallet)
	if err != nil {
		return "", err
	}
	if info.SolanaMasterWallet == "" {
		return "", fmt.Errorf("no solana master wallet for %s (belongsToHouse=%v)", wallet, info.BelongsToHouse)
	}
	return info.SolanaMasterWallet, nil
}

// ProbeOrderlyPost is a diagnostics helper: orderly-signed POST to an arbitrary
// path, returning status + raw body.
func (s *Service) ProbeOrderlyPost(ctx context.Context, path string, body any) (int, string, error) {
	if err := s.requireAuth(); err != nil {
		return 0, "", err
	}
	return s.perptools.OrderlyRawPost(ctx, path, body)
}

// ProbeOrderlyGet is a diagnostics helper: orderly-signed GET to an arbitrary path.
func (s *Service) ProbeOrderlyGet(ctx context.Context, path, queryKey, queryVal string) (int, string, error) {
	if err := s.requireAuth(); err != nil {
		return 0, "", err
	}
	return s.perptools.OrderlyRawGet(ctx, path, queryKey, queryVal)
}

// AgentDepositResult bundles the deposit acknowledgement with the final (or
// last observed) transaction status after polling.
type AgentDepositResult struct {
	Deposit *perptools.AgentDepositResponse           `json:"deposit"`
	Status  *perptools.AgentTransactionStatusResponse `json:"status,omitempty"`
	// Settled is true only when the transaction reached a terminal state
	// within the polling window. When false, poll get_agent_deposit_status
	// later with deposit.transactionId.
	Settled bool `json:"settled"`
}

// DepositToAgent moves USDC from the caller's already-funded Main Account into
// the agent vault, then polls until the transaction settles. The funds are
// signed server-side with the custodied master key, so no wallet signing
// happens here. The Main Account must already hold enough USDC.
func (s *Service) DepositToAgent(ctx context.Context, req perptools.AgentDepositRequest) (*AgentDepositResult, error) {
	return s.depositAndPoll(ctx, req, false)
}

// DirectDepositToAgent funds the agent vault in a single step
// (/api/v1/agents/direct-deposit), then polls until settlement. Same
// custodied-funds source and server-side signing as DepositToAgent.
func (s *Service) DirectDepositToAgent(ctx context.Context, req perptools.AgentDepositRequest) (*AgentDepositResult, error) {
	return s.depositAndPoll(ctx, req, true)
}

func (s *Service) depositAndPoll(ctx context.Context, req perptools.AgentDepositRequest, direct bool) (*AgentDepositResult, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}

	var (
		dep *perptools.AgentDepositResponse
		err error
	)
	if direct {
		dep, err = s.perptools.DirectDepositToAgent(ctx, req.WalletAddress, req)
	} else {
		dep, err = s.perptools.DepositToAgent(ctx, req.WalletAddress, req)
	}
	if err != nil {
		return nil, err
	}

	res := &AgentDepositResult{Deposit: dep}
	if !dep.Success || dep.TransactionID == "" {
		return res, nil
	}

	statusReq := perptools.AgentTransactionStatusRequest{
		WalletAddress: req.WalletAddress,
		TransactionID: dep.TransactionID,
	}
	const (
		pollInterval = 3 * time.Second
		maxAttempts  = 20 // ~60s total
	)
	for i := 0; i < maxAttempts; i++ {
		select {
		case <-ctx.Done():
			return res, nil // deposit is recorded; caller can poll later
		case <-time.After(pollInterval):
		}
		st, err := s.perptools.GetAgentTransactionStatus(ctx, req.WalletAddress, statusReq)
		if err != nil {
			continue // transient — keep polling
		}
		res.Status = st
		if isFinalTxStatus(st.Status) {
			res.Settled = true
			return res, nil
		}
	}
	return res, nil
}

// AgentWithdrawResult bundles the withdraw acknowledgement with the final (or
// last observed) transaction status after polling.
type AgentWithdrawResult struct {
	Withdraw *perptools.AgentWithdrawResponse          `json:"withdraw"`
	Status   *perptools.AgentTransactionStatusResponse `json:"status,omitempty"`
	// Settled is true only when the transaction reached a terminal state
	// within the polling window. When false, poll get_agent_withdrawal_status
	// later with withdraw.transactionId.
	Settled bool `json:"settled"`
	// SharesWithdrawn is the share count actually submitted — resolved from
	// the caller's full holding when "all" was requested.
	SharesWithdrawn float64 `json:"shares_withdrawn"`
}

// WithdrawFromAgentAndPoll redeems vault shares back into the Main Account
// and returns the acknowledgement QUICKLY — only a short status confirm, not
// a settlement wait. Withdrawals settle asynchronously and can take up to 24h
// (manual approval, queued liquidity), while MCP clients time out in well
// under a minute; a long in-call poll loses the whole response INCLUDING the
// transaction id (observed live 2026-06-12 with a 60s client timeout). The
// caller polls GetAgentTransactionStatus; a lost id is recoverable via
// GetAgentTransactions.
//
// When all is true the caller's full current share count is resolved first —
// /api/v1/agents/withdraw-all is blocked server-side, so "all" is emulated by
// submitting the full count.
func (s *Service) WithdrawFromAgentAndPoll(ctx context.Context, req perptools.AgentWithdrawRequest, all bool) (*AgentWithdrawResult, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}

	if all {
		sh, err := s.perptools.GetAgentShares(ctx, req.WalletAddress, req.AgentID)
		if err != nil {
			return nil, fmt.Errorf("resolve current shares: %w", err)
		}
		if sh.UserShares.Shares <= 0 {
			return nil, fmt.Errorf("no shares held in agent %s — nothing to withdraw", req.AgentID)
		}
		req.Shares = sh.UserShares.Shares
	}
	if req.Shares <= 0 {
		return nil, fmt.Errorf("shares must be > 0 (or pass all=true)")
	}

	wd, err := s.perptools.WithdrawFromAgent(ctx, req.WalletAddress, req)
	if err != nil {
		return nil, err
	}

	res := &AgentWithdrawResult{Withdraw: wd, SharesWithdrawn: req.Shares}
	if !wd.Success || wd.TransactionID == "" {
		return res, nil
	}
	res.Status, res.Settled = s.pollAgentTransaction(ctx, req.WalletAddress, wd.TransactionID, 2)
	return res, nil
}

// pollAgentTransaction checks the agent transaction status up to maxAttempts
// times (3s apart) and returns the last observed status plus whether it
// reached a terminal state. Keep maxAttempts SMALL on the submit path: the
// total time must stay far below MCP client timeouts, or the caller loses the
// acknowledgement with the transaction id in it.
func (s *Service) pollAgentTransaction(ctx context.Context, wallet, txID string, maxAttempts int) (*perptools.AgentTransactionStatusResponse, bool) {
	statusReq := perptools.AgentTransactionStatusRequest{
		WalletAddress: wallet,
		TransactionID: txID,
	}
	const pollInterval = 3 * time.Second
	var last *perptools.AgentTransactionStatusResponse
	for i := 0; i < maxAttempts; i++ {
		select {
		case <-ctx.Done():
			return last, false // transaction is recorded; caller can poll later
		case <-time.After(pollInterval):
		}
		st, err := s.perptools.GetAgentTransactionStatus(ctx, wallet, statusReq)
		if err != nil {
			continue // transient — keep polling
		}
		last = st
		if isFinalTxStatus(st.Status) {
			return last, true
		}
	}
	return last, false
}

// WithdrawToWallet moves USDC from the Main Account to the user's REGISTERED
// on-chain wallet (no other destination is possible). The transfer is fully
// server-side and synchronous: when this returns successfully the on-chain tx
// is already submitted. Preflights the Main Account balance to fail fast with
// an actionable message instead of an opaque upstream error.
//
// The preflight trusts the LARGER of the dashboard ledger balance and the
// master wallet's on-chain USDC: right after an agent withdrawal completes,
// the proceeds sit on-chain at the master while the dashboard ledger lags
// (verified live 2026-06-10) — and the on-chain balance is what Envy actually
// spends. Envy remains the final arbiter and rejects a true shortfall.
func (s *Service) WithdrawToWallet(ctx context.Context, wallet string, amount float64) (*perptools.MasterWithdrawResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	if amount <= 0 {
		return nil, fmt.Errorf("amount must be > 0")
	}
	bal, err := s.GetBalances(ctx, wallet)
	if err != nil {
		return nil, fmt.Errorf("preflight balance check: %w", err)
	}
	available := bal.MainAccountUSDC
	if available < amount && bal.MasterWallet != "" {
		if masterPub, perr := solfund.ParsePublicKey(bal.MasterWallet); perr == nil {
			if raw, berr := s.solfund.USDCBalance(ctx, masterPub); berr == nil {
				if onchain := float64(raw) / 1e6; onchain > available {
					available = onchain
				}
			}
		}
	}
	if available < amount {
		return nil, fmt.Errorf(
			"Main Account holds %.6f USDC — not enough to withdraw %.6f; withdraw from an agent first (withdraw_from_agent) or lower the amount",
			available, amount)
	}
	return s.perptools.WithdrawFromMaster(ctx, wallet, perptools.MasterWithdrawRequest{
		WalletAddress: wallet,
		Amount:        amount,
	})
}

// --- AI Agent stats (read-only; all routed through the orderly-signed proxy) ---
//
// NOTE: the backend also exposes an unauthenticated proxy
// (/v1/ai/proxy/public) for arena/details/positions, but it is edge-gated and
// returns a blanket 401 to headless callers (verified live 2026-06-10), so
// every stats read goes through the authed proxy and requires auth.

// ListArenaAgents returns one page of the public agent leaderboard, sorted
// descending by sortBy (aum | pnl1h | pnl24h | pnl7d | pnl30d).
func (s *Service) ListArenaAgents(ctx context.Context, wallet, sortBy string, page, pageSize int) (*perptools.ArenaResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.perptools.GetArenaAgents(ctx, wallet, sortBy, page, pageSize)
}

// AgentDetails returns one agent's profile, vault state, analytics and the
// caller's stake in it.
func (s *Service) AgentDetails(ctx context.Context, wallet, agentID string) (*perptools.AgentDetailsResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.perptools.GetAgentDetails(ctx, wallet, agentID)
}

// AgentPositions returns an agent's trades. posType filters by status
// (e.g. "open"); empty returns all.
func (s *Service) AgentPositions(ctx context.Context, wallet, agentID, posType string, page, limit int) (*perptools.AgentPositionsResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.perptools.GetAgentPositions(ctx, wallet, agentID, posType, page, limit)
}

// MyAgentStats returns the caller's stake in one agent vault (shares, share
// price, current value, share of the vault) plus the vault aggregate.
func (s *Service) MyAgentStats(ctx context.Context, wallet, agentID string) (*perptools.AgentSharesResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.perptools.GetAgentShares(ctx, wallet, agentID)
}

// Portfolio merges the dashboard overview with the per-agent share positions
// into one snapshot of everything the user holds across AI agents.
type Portfolio struct {
	// USDCBalance is the spendable Main Account balance (deposit_to_agent's source).
	USDCBalance float64 `json:"usdc_balance"`
	// TotalPortfolioValue is the Main Account balance plus the value of all vault shares.
	TotalPortfolioValue float64 `json:"total_portfolio_value"`
	MyAgentCount        int     `json:"my_agent_count"`
	ActiveAgentCount    int     `json:"active_agent_count"`
	// TopPerformer is the user's best-performing agent, when the backend reports one.
	TopPerformer *perptools.AgentInfo `json:"top_performer,omitempty"`
	// Agents lists every agent the wallet holds vault shares in.
	Agents []perptools.MyAgentSummary `json:"agents"`
}

// MyPortfolio merges /api/v1/dashboard/overview with /api/v1/agents/list. The
// heavy per-agent series (pnlChart, latestReport) are stripped — callers that
// need them should use AgentDetails.
func (s *Service) MyPortfolio(ctx context.Context, wallet string) (*Portfolio, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	dash, err := s.perptools.GetDashboardOverview(ctx, wallet)
	if err != nil {
		return nil, err
	}
	list, err := s.perptools.GetMyAgents(ctx, wallet)
	if err != nil {
		return nil, fmt.Errorf("list my agents: %w", err)
	}
	agents := list.Agents
	for i := range agents {
		agents[i].PnlChart = nil
		agents[i].LatestReport = nil
	}
	return &Portfolio{
		USDCBalance:         dash.UsdcBalance,
		TotalPortfolioValue: dash.TotalPortfolioValue,
		MyAgentCount:        dash.MyAgentCount,
		ActiveAgentCount:    dash.ActiveAgentCount,
		TopPerformer:        dash.TopPerformer,
		Agents:              agents,
	}, nil
}

// --- Agent chat (websocket relay to the agent's Envy terminal) ---

// SendAgentMessage sends one chat message to an AI agent over a short-lived
// websocket session and waits for the agent's reply. The session is held open
// until the reply quiesces (replies can span multiple frames and take tens of
// seconds) or until timeout — closing early would lose the late reply, since
// the relay persists agent output only inside the live session.
func (s *Service) SendAgentMessage(ctx context.Context, wallet, agentID, message string, timeout time.Duration) (*perptools.ChatReply, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(message) == "" {
		return nil, fmt.Errorf("message must not be empty")
	}
	return s.perptools.SendAgentMessage(ctx, wallet, agentID, message, timeout)
}

// AgentChatHistory returns persisted chat history with an agent. An empty
// `before` returns the newest batch; pass the returned cursor as `before` to
// page further back.
func (s *Service) AgentChatHistory(ctx context.Context, wallet, agentID, before string, limit int) (*perptools.ChatHistory, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.perptools.GetAgentChatHistory(ctx, wallet, agentID, before, limit)
}

// GetAgentTransactionStatus reports the settlement state of a vault deposit or
// withdrawal by its transaction id.
// GetAgentTransactions lists the caller's deposits/withdrawals in one agent
// vault, newest first. This is the recovery path when a withdraw/deposit
// acknowledgement (and its transaction id) was lost to a client-side timeout,
// and the way to find an in-flight withdrawal blocking a new one.
func (s *Service) GetAgentTransactions(ctx context.Context, req perptools.AgentTransactionsRequest) (*perptools.AgentTransactionsResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.perptools.GetAgentTransactions(ctx, req.WalletAddress, req)
}

func (s *Service) GetAgentTransactionStatus(ctx context.Context, req perptools.AgentTransactionStatusRequest) (*perptools.AgentTransactionStatusResponse, error) {
	if err := s.requireAuth(); err != nil {
		return nil, err
	}
	return s.perptools.GetAgentTransactionStatus(ctx, req.WalletAddress, req)
}

// isFinalTxStatus mirrors the Envy proxy's terminal-status set.
func isFinalTxStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "COMPLETED", "FAILED", "CANCELLED", "CANCELED", "REJECTED", "CONFIRMED", "SUCCESS", "ERROR", "FAILURE":
		return true
	}
	return false
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
