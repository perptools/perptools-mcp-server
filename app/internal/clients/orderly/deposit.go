package orderly

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	computebudget "github.com/gagliardetto/solana-go/programs/compute-budget"
)

var (
	DEPOSIT_TOKEN_DISCRIMINATOR = [8]byte{242, 35, 198, 137, 82, 225, 242, 182}
	DEPOSIT_SOL_DISCRIMINATOR   = [8]byte{108, 81, 78, 117, 125, 155, 56, 200}
)

type VaultDepositParams struct {
	AccountID   [32]byte
	BrokerHash  [32]byte
	TokenHash   [32]byte
	UserAddress [32]byte
	TokenAmount uint64
}

func (p VaultDepositParams) MarshalWithEncoder(enc *bin.Encoder) error {
	for _, v := range []any{p.AccountID, p.BrokerHash, p.TokenHash, p.UserAddress, p.TokenAmount} {
		if err := enc.Encode(v); err != nil {
			return err
		}
	}
	return nil
}

type OAppSendParams struct {
	NativeFee  uint64 `json:"nativeFee"`
	LzTokenFee uint64 `json:"lzTokenFee"`
}

func (p OAppSendParams) MarshalWithEncoder(enc *bin.Encoder) error {
	if err := enc.Encode(p.NativeFee); err != nil {
		return err
	}
	return enc.Encode(p.LzTokenFee)
}

func Deposit(
	brokerID, symbol string,
	userPublicKey solana.PublicKey,
	amount uint64,
	sendParams OAppSendParams,
	blockhash solana.Hash,
) (*solana.Transaction, error) {

	accountID, err := GetAccountID(userPublicKey.String(), brokerID)
	if err != nil {
		return nil, fmt.Errorf("get accountID: %w", err)
	}

	token, ok := SYMBOL_TOKEN[symbol]
	if !ok {
		return nil, fmt.Errorf("unsupported symbol: %s (supported: USDC, USDT, SOL)", symbol)
	}

	vaultProgram := SOLANA_VAULT_PROGRAM_ID[ChainName]
	tokenHash := ParseTokenHash(symbol)
	brokerHash := ParseTokenHash(brokerID)
	solHash := ParseTokenHash("SOL")
	isSolDeposit := strings.EqualFold(tokenHash, solHash)

	userTokenAccount, _, err := solana.FindAssociatedTokenAddress(userPublicKey, token)
	if err != nil {
		return nil, fmt.Errorf("derive user ATA: %w", err)
	}

	vaultAuthorityPDA := getVaultAuthorityPDA(vaultProgram)
	vaultTokenAccount, _, err := solana.FindAssociatedTokenAddress(vaultAuthorityPDA, token)
	if err != nil {
		return nil, fmt.Errorf("derive vault ATA: %w", err)
	}

	allowedBrokerPDA := getBrokerPDA(vaultProgram, brokerHash)
	allowedTokenPDA := getTokenPDA(vaultProgram, tokenHash)

	oappConfigPDA := getOAppConfigPDA(vaultProgram)
	dstEID := getDstEID()
	peerPDA := getPeerPDA(vaultProgram, oappConfigPDA, dstEID)
	enforcedPDA := getEnforcedOptionsPDA(vaultProgram, oappConfigPDA, dstEID)

	sendLibPDA := getSendLibPDA()
	sendLibConfigPDA := getSendLibConfigPDA(oappConfigPDA, dstEID)
	defaultSendLibPDA := getDefaultSendLibConfigPDA(dstEID)
	sendLibInfoPDA := getSendLibInfoPDA(sendLibPDA)

	endpointSettingPDA := getEndpointSettingPDA()
	noncePDA := getNoncePDA(vaultProgram, oappConfigPDA, dstEID)
	eventAuthorityPDA := getEventAuthorityPDA()

	ulnSettingPDA := getUlnSettingPDA()
	sendConfigPDA := getSendConfigPDA(oappConfigPDA, dstEID)
	defaultSendConfigPDA := getDefaultSendConfigPDA(dstEID)

	ulnEventAuthorityPDA := getUlnEventAuthorityPDA()
	executorConfigPDA := getExecutorConfigPDA()
	priceFeedPDA := getPriceFeedPDA()
	dvnConfigPDA := getDvnConfigPDA()

	buf := new(bytes.Buffer)
	enc := bin.NewBorshEncoder(buf)

	var depositAccounts solana.AccountMetaSlice

	if isSolDeposit {
		if err := enc.WriteBytes(DEPOSIT_SOL_DISCRIMINATOR[:], false); err != nil {
			return nil, fmt.Errorf("write discriminator: %w", err)
		}
		depositAccounts = solana.AccountMetaSlice{
			solana.Meta(userPublicKey).SIGNER().WRITE(),
			solana.Meta(vaultAuthorityPDA).WRITE(),
			solana.Meta(getSolVaultPDA(vaultProgram)).WRITE(),
			solana.Meta(peerPDA),
			solana.Meta(enforcedPDA),
			solana.Meta(oappConfigPDA),
			solana.Meta(allowedBrokerPDA),
			solana.Meta(allowedTokenPDA),
			solana.Meta(solana.SystemProgramID),
		}
	} else {
		if err := enc.WriteBytes(DEPOSIT_TOKEN_DISCRIMINATOR[:], false); err != nil {
			return nil, fmt.Errorf("write discriminator: %w", err)
		}
		depositAccounts = solana.AccountMetaSlice{
			solana.Meta(userPublicKey).SIGNER().WRITE(),
			solana.Meta(userTokenAccount).WRITE(),
			solana.Meta(vaultAuthorityPDA).WRITE(),
			solana.Meta(vaultTokenAccount).WRITE(),
			solana.Meta(token),
			solana.Meta(peerPDA),
			solana.Meta(enforcedPDA),
			solana.Meta(oappConfigPDA),
			solana.Meta(allowedBrokerPDA),
			solana.Meta(allowedTokenPDA),
			solana.Meta(solana.TokenProgramID),
			solana.Meta(solana.SPLAssociatedTokenAccountProgramID),
			solana.Meta(solana.SystemProgramID),
		}
	}

	remainingAccounts := solana.AccountMetaSlice{
		solana.Meta(ENDPOINT_PROGRAM_ID),
		solana.Meta(oappConfigPDA),
		solana.Meta(SEND_LIB_PROGRAM_ID),
		solana.Meta(sendLibConfigPDA),
		solana.Meta(defaultSendLibPDA),
		solana.Meta(sendLibInfoPDA),
		solana.Meta(endpointSettingPDA),
		solana.Meta(noncePDA).WRITE(),
		solana.Meta(eventAuthorityPDA),
		solana.Meta(ENDPOINT_PROGRAM_ID),
		solana.Meta(ulnSettingPDA),
		solana.Meta(sendConfigPDA),
		solana.Meta(defaultSendConfigPDA),
		solana.Meta(userPublicKey).SIGNER(),
		solana.Meta(TREASURY_PROGRAM_ID),
		solana.Meta(solana.SystemProgramID),
		solana.Meta(ulnEventAuthorityPDA),
		solana.Meta(SEND_LIB_PROGRAM_ID),
		solana.Meta(EXECUTOR_PROGRAM_ID),
		solana.Meta(executorConfigPDA).WRITE(),
		solana.Meta(PRICE_FEED_PROGRAM_ID),
		solana.Meta(priceFeedPDA),
		solana.Meta(DVN_PROGRAM_ID),
		solana.Meta(dvnConfigPDA).WRITE(),
		solana.Meta(PRICE_FEED_PROGRAM_ID),
		solana.Meta(priceFeedPDA),
	}

	if err := enc.Encode(VaultDepositParams{
		AccountID:   accountID,
		BrokerHash:  [32]byte(crypto.Keccak256([]byte(brokerID))),
		TokenHash:   [32]byte(crypto.Keccak256([]byte(TOKEN_SYMBOL[token]))),
		UserAddress: [32]byte(userPublicKey.Bytes()),
		TokenAmount: amount,
	}); err != nil {
		return nil, fmt.Errorf("encode deposit params: %w", err)
	}

	if err := enc.Encode(sendParams); err != nil {
		return nil, fmt.Errorf("encode send params: %w", err)
	}

	instructions := []solana.Instruction{
		computebudget.NewSetComputeUnitLimitInstruction(400_000).Build(),
		solana.NewInstruction(
			vaultProgram,
			append(depositAccounts, remainingAccounts...),
			buf.Bytes(),
		),
	}

	return solana.NewTransaction(
		instructions,
		blockhash,
		solana.TransactionPayer(userPublicKey),
		solana.TransactionAddressTables(LOOKUP_TABLE_ADDRESS[ChainName]),
	)
}
