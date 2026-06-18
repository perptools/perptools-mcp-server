// Package solfund builds and submits the on-chain USDC transfer that funds a
// user's Main Account (master wallet). Depositing into the Main Account is just
// a plain SPL transfer from the user's wallet to the master wallet address
// (obtained from the Envy dashboard/users-info endpoint); Envy credits the
// Main Account from the on-chain balance. This keeps the whole top-up reachable
// from the MCP server without the browser-only session gateway.
package solfund

import (
	"context"
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
	atok "github.com/gagliardetto/solana-go/programs/associated-token-account"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/gagliardetto/solana-go/rpc"
)

// USDCMint is the SPL mint of USDC on Solana mainnet-beta.
var USDCMint = solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")

// USDCDecimals is the USDC token's decimal precision (1 USDC = 1_000_000).
const USDCDecimals = 6

// Client wraps a Solana RPC client for the USDC funding flow.
type Client struct {
	rpc *rpc.Client
}

func New(rpcURL string) *Client {
	return &Client{rpc: rpc.New(rpcURL)}
}

// RPC exposes the underlying Solana RPC client for callers that build their own
// transactions (e.g. the Orderly vault deposit, which needs an oappQuote
// simulation and a recent blockhash). Reuses the same connection as the USDC
// funding flow so there is a single RPC endpoint to configure.
func (c *Client) RPC() *rpc.Client { return c.rpc }

// ToRaw converts a human USDC amount (e.g. 1.5) to base units (1_500_000).
func ToRaw(amount float64) uint64 {
	return uint64(amount*1e6 + 0.5)
}

// ParsePublicKey parses a base58 Solana address.
func ParsePublicKey(s string) (solana.PublicKey, error) {
	return solana.PublicKeyFromBase58(s)
}

// USDCBalance returns the owner's USDC balance in base units. Returns 0 (no
// error) when the owner has no USDC token account yet.
func (c *Client) USDCBalance(ctx context.Context, owner solana.PublicKey) (uint64, error) {
	ata, _, err := solana.FindAssociatedTokenAddress(owner, USDCMint)
	if err != nil {
		return 0, fmt.Errorf("derive ATA: %w", err)
	}
	bal, err := c.rpc.GetTokenAccountBalance(ctx, ata, rpc.CommitmentFinalized)
	if err != nil {
		// account not found => zero balance
		return 0, nil
	}
	var raw uint64
	_, scanErr := fmt.Sscan(bal.Value.Amount, &raw)
	if scanErr != nil {
		return 0, fmt.Errorf("parse balance %q: %w", bal.Value.Amount, scanErr)
	}
	return raw, nil
}

// BuildUSDCTransfer assembles an UNSIGNED transaction that transfers rawAmount
// USDC base units from -> to, creating the recipient's USDC token account if it
// does not exist yet. The fee payer is `from`, so `from` must sign it. Return
// the tx as base64 (tx.ToBase64()) for an external wallet to sign, or sign it
// in-process for tests.
func (c *Client) BuildUSDCTransfer(ctx context.Context, from, to solana.PublicKey, rawAmount uint64) (*solana.Transaction, error) {
	fromATA, _, err := solana.FindAssociatedTokenAddress(from, USDCMint)
	if err != nil {
		return nil, fmt.Errorf("derive source ATA: %w", err)
	}
	toATA, _, err := solana.FindAssociatedTokenAddress(to, USDCMint)
	if err != nil {
		return nil, fmt.Errorf("derive dest ATA: %w", err)
	}

	var instrs []solana.Instruction

	// Create the recipient's USDC ATA if it is missing (payer = from).
	if _, err := c.rpc.GetAccountInfo(ctx, toATA); err != nil {
		createIx := atok.NewCreateInstruction(from, to, USDCMint).Build()
		instrs = append(instrs, createIx)
	}

	transferIx := token.NewTransferCheckedInstruction(
		rawAmount,
		USDCDecimals,
		fromATA,
		USDCMint,
		toATA,
		from,
		nil,
	).Build()
	instrs = append(instrs, transferIx)

	recent, err := c.rpc.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("get blockhash: %w", err)
	}

	tx, err := solana.NewTransaction(instrs, recent.Value.Blockhash, solana.TransactionPayer(from))
	if err != nil {
		return nil, fmt.Errorf("build tx: %w", err)
	}
	return tx, nil
}

// Submit sends a signed transaction and waits until it is confirmed (or ctx
// expires). Returns the transaction signature.
func (c *Client) Submit(ctx context.Context, tx *solana.Transaction) (solana.Signature, error) {
	sig, err := c.rpc.SendTransaction(ctx, tx)
	if err != nil {
		return solana.Signature{}, fmt.Errorf("send tx: %w", err)
	}
	// poll for confirmation
	for i := 0; i < 30; i++ {
		select {
		case <-ctx.Done():
			return sig, ctx.Err()
		case <-time.After(2 * time.Second):
		}
		st, err := c.rpc.GetSignatureStatuses(ctx, true, sig)
		if err != nil || st == nil || len(st.Value) == 0 || st.Value[0] == nil {
			continue
		}
		s := st.Value[0]
		if s.Err != nil {
			return sig, fmt.Errorf("tx failed on-chain: %v", s.Err)
		}
		if s.ConfirmationStatus == rpc.ConfirmationStatusConfirmed || s.ConfirmationStatus == rpc.ConfirmationStatusFinalized {
			return sig, nil
		}
	}
	return sig, fmt.Errorf("tx %s not confirmed in time", sig)
}

// SubmitBase64 decodes a base64-encoded signed transaction and submits it.
func (c *Client) SubmitBase64(ctx context.Context, b64 string) (solana.Signature, error) {
	tx, err := solana.TransactionFromBase64(b64)
	if err != nil {
		return solana.Signature{}, fmt.Errorf("decode tx: %w", err)
	}
	return c.Submit(ctx, tx)
}
