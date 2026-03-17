package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	"mcp-server/app/internal/clients/orderly"

	"github.com/btcsuite/btcutil/base58"
)

const solanaChainID = 900900900

type Credentials struct {
	AccountID         string
	WalletAddress     string
	OrderlyPublicKey  string
	OrderlyPrivateKey ed25519.PrivateKey
}

type PrepareResult struct {
	MessageBase64     string
	WalletAddress     string
	DebugHash         string
	AlreadyRegistered bool
	AccountID         string
}

type Config struct {
	BrokerID string
}

type Service struct {
	cfg    Config
	client orderly.Client

	credentials *Credentials

	pendingRegMsg *orderly.RegisterAccountMessage
	pendingKeyMsg *orderly.OrderlyKeyMessage
	pendingKeyPub ed25519.PublicKey
	pendingKeyPrv ed25519.PrivateKey
}

func NewService(cfg Config, client orderly.Client) *Service {
	return &Service{cfg: cfg, client: client}
}

func (s *Service) IsAuthenticated() bool {
	return s.credentials != nil && s.credentials.OrderlyPrivateKey != nil
}

func (s *Service) GetCredentials() *Credentials {
	return s.credentials
}

// Step 1: check if account already exists; if not, build registration message
// for wallet signing.
func (s *Service) PrepareRegistration(ctx context.Context, walletAddress string) (*PrepareResult, error) {
	existing, err := s.client.GetAccount(ctx, walletAddress, s.cfg.BrokerID)
	if err == nil && existing.Success && existing.Data.AccountID != "" {
		s.credentials = &Credentials{
			AccountID:     existing.Data.AccountID,
			WalletAddress: walletAddress,
		}
		return &PrepareResult{
			WalletAddress:     walletAddress,
			AlreadyRegistered: true,
			AccountID:         existing.Data.AccountID,
		}, nil
	}

	nonceResp, err := s.client.GetNonce(ctx)
	if err != nil {
		return nil, fmt.Errorf("get nonce: %w", err)
	}

	ts := time.Now().UTC().UnixMilli()
	msg := orderly.RegisterAccountMessage{
		BrokerID:          s.cfg.BrokerID,
		ChainID:           solanaChainID,
		Timestamp:         strconv.FormatInt(ts, 10),
		RegistrationNonce: nonceResp.Data.Nonce,
		ChainType:         "SOL",
	}

	signBytes, err := CreateRegistrationMessage(msg.BrokerID, msg.RegistrationNonce, int64(msg.ChainID), ts)
	if err != nil {
		return nil, fmt.Errorf("create registration message: %w", err)
	}

	s.pendingRegMsg = &msg
	return &PrepareResult{
		MessageBase64: base64.StdEncoding.EncodeToString(signBytes),
		WalletAddress: walletAddress,
		DebugHash:     string(signBytes),
	}, nil
}

// Step 2: submit wallet signature to Orderly.
func (s *Service) CompleteRegistration(ctx context.Context, walletAddress, signature string) error {
	if s.pendingRegMsg == nil {
		return fmt.Errorf("call PrepareRegistration first")
	}

	result, err := s.client.RegisterAccount(ctx, orderly.RegisterAccountRequest{
		Message:     *s.pendingRegMsg,
		Signature:   signature,
		UserAddress: walletAddress,
	})
	if err != nil {
		return fmt.Errorf("register account: %w", err)
	}
	if result.Data.AccountID == "" {
		return fmt.Errorf("registration failed")
	}

	s.credentials = &Credentials{
		AccountID:     result.Data.AccountID,
		WalletAddress: walletAddress,
	}
	s.pendingRegMsg = nil
	return nil
}

// Step 3: generate random ed25519 keypair, build key message, return base64-encoded bytes for wallet to sign.
func (s *Service) PrepareOrderlyKey(ctx context.Context, walletAddress string) (*PrepareResult, error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 keypair: %w", err)
	}

	now := time.Now().UTC()
	msg := orderly.OrderlyKeyMessage{
		BrokerID:   s.cfg.BrokerID,
		ChainID:    solanaChainID,
		OrderlyKey: "ed25519:" + base58.Encode(pub),
		Scope:      "read,trading,asset",
		Timestamp:  now.UnixMilli(),
		Expiration: now.Add(365 * 24 * time.Hour).UnixMilli(),
		ChainType:  "SOL",
	}

	signBytes, err := CreateOrderlyKeyMessage(msg)
	if err != nil {
		return nil, fmt.Errorf("create orderly key message: %w", err)
	}

	s.pendingKeyMsg = &msg
	s.pendingKeyPub = pub
	s.pendingKeyPrv = priv
	return &PrepareResult{
		MessageBase64: base64.StdEncoding.EncodeToString(signBytes),
		WalletAddress: walletAddress,
		DebugHash:     string(signBytes),
	}, nil
}

// Step 4: submit wallet signature to Orderly, store credentials in memory.
func (s *Service) CompleteOrderlyKey(ctx context.Context, walletAddress, signature string) error {
	if s.pendingKeyMsg == nil {
		return fmt.Errorf("call PrepareOrderlyKey first")
	}

	result, err := s.client.AddOrderlyKey(ctx, orderly.AddOrderlyKeyRequest{
		Message:     *s.pendingKeyMsg,
		Signature:   signature,
		UserAddress: walletAddress,
	})
	if err != nil {
		return fmt.Errorf("add orderly key: %w", err)
	}
	if result.Data.OrderlyKey == "" {
		return fmt.Errorf("key registration failed")
	}

	if s.credentials == nil {
		s.credentials = &Credentials{WalletAddress: walletAddress}
	}
	s.credentials.OrderlyPublicKey = base58.Encode(s.pendingKeyPub)
	s.credentials.OrderlyPrivateKey = s.pendingKeyPrv

	s.pendingKeyMsg = nil
	s.pendingKeyPub = nil
	s.pendingKeyPrv = nil
	return nil
}
