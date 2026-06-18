package orderly

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// oappQuote Anchor discriminator: sha256("global:oapp_quote")[0:8]
var oappQuoteDiscriminator = func() [8]byte {
	h := sha256.Sum256([]byte("global:oapp_quote"))
	var d [8]byte
	copy(d[:], h[:8])
	return d
}()

// lastLogs returns the final n simulation log lines joined for error context.
func lastLogs(logs []string, n int) string {
	if len(logs) > n {
		logs = logs[len(logs)-n:]
	}
	return strings.Join(logs, " | ")
}

// GetDepositQuoteFee simulates an oappQuote call on the Orderly vault program
// to get the LayerZero native fee required for a cross-chain deposit.
func GetDepositQuoteFee(
	ctx context.Context,
	rpcClient *rpc.Client,
	brokerID, symbol string,
	userPublicKey solana.PublicKey,
	amount uint64,
) (uint64, error) {
	token, ok := SYMBOL_TOKEN[symbol]
	if !ok {
		return 0, fmt.Errorf("unsupported symbol: %s", symbol)
	}

	accountID, err := GetAccountID(userPublicKey.String(), brokerID)
	if err != nil {
		return 0, fmt.Errorf("get accountID: %w", err)
	}

	vaultProgram := SOLANA_VAULT_PROGRAM_ID[ChainName]
	dstEID := getDstEID()

	oappConfigPDA := getOAppConfigPDA(vaultProgram)
	peerPDA := getPeerPDA(vaultProgram, oappConfigPDA, dstEID)
	enforcedPDA := getEnforcedOptionsPDA(vaultProgram, oappConfigPDA, dstEID)
	vaultAuthorityPDA := getVaultAuthorityPDA(vaultProgram)

	sendLibConfigPDA := getSendLibConfigPDA(oappConfigPDA, dstEID)
	defaultSendLibPDA := getDefaultSendLibConfigPDA(dstEID)
	sendLibPDA := getSendLibPDA()
	sendLibInfoPDA := getSendLibInfoPDA(sendLibPDA)
	endpointSettingPDA := getEndpointSettingPDA()
	noncePDA := getNoncePDA(vaultProgram, oappConfigPDA, dstEID)
	sendConfigPDA := getSendConfigPDA(oappConfigPDA, dstEID)
	defaultSendConfigPDA := getDefaultSendConfigPDA(dstEID)
	executorConfigPDA := getExecutorConfigPDA()
	priceFeedPDA := getPriceFeedPDA()

	// DVN worker accounts are read live: the OApp's ULN send config decides how
	// many DVNs participate (currently 3 on mainnet), and a hardcoded count
	// triggers ULN error 6019 (InvalidAccountLength).
	dvns, err := fetchSendDVNs(ctx, rpcClient, oappConfigPDA, dstEID)
	if err != nil {
		return 0, fmt.Errorf("resolve send DVNs: %w", err)
	}

	buf := new(bytes.Buffer)
	enc := bin.NewBorshEncoder(buf)

	if err := enc.WriteBytes(oappQuoteDiscriminator[:], false); err != nil {
		return 0, fmt.Errorf("write discriminator: %w", err)
	}

	if err := enc.Encode(VaultDepositParams{
		AccountID:   accountID,
		BrokerHash:  [32]byte(crypto.Keccak256([]byte(brokerID))),
		TokenHash:   [32]byte(crypto.Keccak256([]byte(TOKEN_SYMBOL[token]))),
		UserAddress: [32]byte(userPublicKey.Bytes()),
		TokenAmount: amount,
	}); err != nil {
		return 0, fmt.Errorf("encode deposit params: %w", err)
	}

	accounts := solana.AccountMetaSlice{
		solana.Meta(oappConfigPDA),
		solana.Meta(peerPDA),
		solana.Meta(enforcedPDA),
		solana.Meta(vaultAuthorityPDA),
	}

	remainingAccounts := solana.AccountMetaSlice{
		solana.Meta(ENDPOINT_PROGRAM_ID),
		solana.Meta(SEND_LIB_PROGRAM_ID),
		solana.Meta(sendLibConfigPDA),
		solana.Meta(defaultSendLibPDA),
		solana.Meta(sendLibInfoPDA),
		solana.Meta(endpointSettingPDA),
		solana.Meta(noncePDA),
		solana.Meta(sendLibPDA),
		solana.Meta(sendConfigPDA),
		solana.Meta(defaultSendConfigPDA),
		solana.Meta(EXECUTOR_PROGRAM_ID),
		solana.Meta(executorConfigPDA),
		solana.Meta(PRICE_FEED_PROGRAM_ID),
		solana.Meta(priceFeedPDA),
	}
	// per-DVN worker chunks, scaled to the live ULN config (read-only for quote)
	remainingAccounts = appendDVNWorkerAccounts(remainingAccounts, dvns, false)

	ix := solana.NewInstruction(
		vaultProgram,
		append(accounts, remainingAccounts...),
		buf.Bytes(),
	)

	recent, err := rpcClient.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return 0, fmt.Errorf("get blockhash: %w", err)
	}

	// Raise the compute-unit limit: quoting fans out a CPI into the executor and
	// every DVN (3 on mainnet) plus their price-feed lookups, which blows past the
	// 200k default and otherwise fails as ProgramFailedToComplete.
	// ComputeBudget SetComputeUnitLimit(1_400_000): opcode 0x02 + u32 LE units.
	cuLimitIx := solana.NewInstruction(
		solana.ComputeBudget,
		solana.AccountMetaSlice{},
		[]byte{0x02, 0xC0, 0x5C, 0x15, 0x00},
	)

	// No address-lookup-table here: the canonical Orderly quote (scripts/quote_oapp.ts
	// `oappQuote(...).view()`) builds a plain legacy transaction. The accounts fit
	// without an ALT, and pinning to a hardcoded ALT snapshot risks resolving stale
	// pubkeys on-chain — which surfaces as ULN error 6019 (InvalidAccountLength).
	tx, err := solana.NewTransaction(
		[]solana.Instruction{cuLimitIx, ix},
		recent.Value.Blockhash,
		solana.TransactionPayer(userPublicKey),
	)
	if err != nil {
		return 0, fmt.Errorf("build quote tx: %w", err)
	}

	simResult, err := rpcClient.SimulateTransaction(ctx, tx)
	if err != nil {
		return 0, fmt.Errorf("simulate transaction: %w", err)
	}

	if simResult.Value.Err != nil {
		return 0, fmt.Errorf("simulation error: %v; logs: %s", simResult.Value.Err, lastLogs(simResult.Value.Logs, 6))
	}

	returnPrefix := fmt.Sprintf("Program return: %s ", vaultProgram.String())
	var encodedReturn string
	if simResult.Value.Logs != nil {
		for _, log := range simResult.Value.Logs {
			if strings.HasPrefix(log, returnPrefix) {
				encodedReturn = strings.TrimPrefix(log, returnPrefix)
				break
			}
		}
	}

	if encodedReturn == "" {
		return 0, fmt.Errorf("oappQuote returned no data — check vault program and accounts")
	}

	decoded, err := base64.StdEncoding.DecodeString(encodedReturn)
	if err != nil {
		return 0, fmt.Errorf("decode return data: %w", err)
	}

	if len(decoded) < 8 {
		return 0, fmt.Errorf("unexpected return data length: %d", len(decoded))
	}

	fee := binary.LittleEndian.Uint64(decoded[:8])
	return fee, nil
}
