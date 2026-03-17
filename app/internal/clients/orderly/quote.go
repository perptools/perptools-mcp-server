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
	dvnConfigPDA := getDvnConfigPDA()

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
		solana.Meta(DVN_PROGRAM_ID),
		solana.Meta(dvnConfigPDA),
		solana.Meta(PRICE_FEED_PROGRAM_ID),
		solana.Meta(priceFeedPDA),
	}

	ix := solana.NewInstruction(
		vaultProgram,
		append(accounts, remainingAccounts...),
		buf.Bytes(),
	)

	recent, err := rpcClient.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return 0, fmt.Errorf("get blockhash: %w", err)
	}

	tx, err := solana.NewTransaction(
		[]solana.Instruction{ix},
		recent.Value.Blockhash,
		solana.TransactionPayer(userPublicKey),
		solana.TransactionAddressTables(LOOKUP_TABLE_ADDRESS[ChainName]),
	)
	if err != nil {
		return 0, fmt.Errorf("build quote tx: %w", err)
	}

	simResult, err := rpcClient.SimulateTransaction(ctx, tx)
	if err != nil {
		return 0, fmt.Errorf("simulate transaction: %w", err)
	}

	if simResult.Value.Err != nil {
		return 0, fmt.Errorf("simulation error: %v", simResult.Value.Err)
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
