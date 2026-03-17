package orderly

import (
	"encoding/binary"
	"encoding/hex"

	"github.com/gagliardetto/solana-go"
)

const ChainName = "mainnet"

var (
	VAULT_AUTHORITY_SEED     = []byte("VaultAuthority")
	SOL_VAULT_SEED           = []byte("SolVault")
	BROKER_SEED              = []byte("Broker")
	TOKEN_SEED               = []byte("Token")
	OAPP_SEED                = []byte("OApp")
	PEER_SEED                = []byte("Peer")
	ENFORCED_OPTIONS_SEED    = []byte("EnforcedOptions")
	MESSAGE_LIB_SEED         = []byte("MessageLib")
	SEND_LIBRARY_CONFIG_SEED = []byte("SendLibraryConfig")
	ENDPOINT_SEED            = []byte("Endpoint")
	NONCE_SEED               = []byte("Nonce")
	EVENT_SEED               = []byte("__event_authority")
	SEND_CONFIG_SEED         = []byte("SendConfig")
	EXECUTOR_CONFIG_SEED     = []byte("ExecutorConfig")
	PRICE_FEED_SEED          = []byte("PriceFeed")
	DVN_CONFIG_SEED          = []byte("DvnConfig")
	ULN_SEED                 = []byte("MessageLib")

	MAIN_DST_EID    = uint32(30213)
	TESTNET_DST_EID = uint32(40200)

	ENDPOINT_PROGRAM_ID    = solana.MustPublicKeyFromBase58("76y77prsiCMvXMjuoZ5VRrhG5qYBrUMYTE5WgHqgjEn6")
	SEND_LIB_PROGRAM_ID    = solana.MustPublicKeyFromBase58("7a4WjyR8VZ7yZz5XJAKm39BUGn5iT9CKcv2pmG9tdXVH")
	RECEIVE_LIB_PROGRAM_ID = SEND_LIB_PROGRAM_ID
	TREASURY_PROGRAM_ID    = SEND_LIB_PROGRAM_ID
	PRICE_FEED_PROGRAM_ID  = solana.MustPublicKeyFromBase58("8ahPGPjEbpgGaZx2NV1iG5Shj7TDwvsjkEDcGWjt94TP")
	DVN_PROGRAM_ID         = solana.MustPublicKeyFromBase58("HtEYV4xB4wvsj5fgTkcfuChYpvGYzgzwvNhgDZQNh7wW")
	EXECUTOR_PROGRAM_ID    = solana.MustPublicKeyFromBase58("6doghB248px58JSSwG4qejQ46kFMW4AMj7vzJnWZHNZn")

	SOLANA_VAULT_PROGRAM_ID = map[string]solana.PublicKey{
		"mainnet": solana.MustPublicKeyFromBase58("ErBmAD61mGFKvrFNaTJuxoPwqrS8GgtwtqJTJVjFWx9Q"),
		"testnet": solana.MustPublicKeyFromBase58("9shwxWDUNhtwkHocsUAmrNAQfBH2DHh4njdAEdHZZkF2"),
	}

	LOOKUP_TABLE_ADDRESS = map[string]map[solana.PublicKey]solana.PublicKeySlice{
		"mainnet": {
			solana.MustPublicKeyFromBase58("8iq7xCQt3bLdRRn4A46d5GuaXYinBoiAhbe2sUmZVzwg"): {
				solana.MustPublicKeyFromBase58("4ffJMy1qwt9bD9r9Ty1VsdYECaDR88sP6JD1EFM7T1Np"),
				solana.MustPublicKeyFromBase58("6nDxLQwe2TQtNQM5EveWU5pJ1kgqdZyAQyHZY1cFxYao"),
				solana.MustPublicKeyFromBase58("CGvDvUb3CRp7HVtN8UZL89nZUzYqJMwmHTMZgh1Lf4z6"),
				solana.MustPublicKeyFromBase58("F8E8QGhKmHEx2esh5LpVizzcP4cHYhzXdXTwg9w3YYY2"),
				solana.MustPublicKeyFromBase58("J2Qm6r15Q3hJ63uZ8Ht4z7VnfE3Zyo2us7jLf4R2Hc56"),
				solana.MustPublicKeyFromBase58("3caaP2M82TmvmYzG72nHZ1swP7ZexLGRZtT9wwG7nKCg"),
				solana.MustPublicKeyFromBase58("2XgGZG4oP29U3w5h4nTk1V2LFHL23zKDPJjs3psGzLKQ"),
				solana.MustPublicKeyFromBase58("86hdmiYJRj3crVDY1iabRb3bvKam9RKS3ZukinByfqQe"),
				solana.MustPublicKeyFromBase58("526PeNZfw8kSnDU4nmzJFVJzJWNhwmZykEyJr5XWz5Fv"),
				solana.MustPublicKeyFromBase58("5Wv7mK46P19uGkYvVzsAhhrBHqwQzrGJKMh35fhcTZ6p"),
				solana.MustPublicKeyFromBase58("7Epzyft1euxACudED4fQJwDLeMvmuar6r3vPacCHbSTA"),
				solana.MustPublicKeyFromBase58("6pE6QhX1j1SJPvG1bXKQSyPo4gwrKZpP3UVyWyK6gSBG"),
				solana.MustPublicKeyFromBase58("7n1YeBMVEUCJ4DscKAcpVQd6KXU7VpcEcc15ZuMcL4U3"),
				solana.MustPublicKeyFromBase58("2XgGZG4oP29U3w5h4nTk1V2LFHL23zKDPJjs3psGzLKQ"),
				solana.MustPublicKeyFromBase58("2uk9pQh3tB5ErV7LGQJcbWjb4KeJ2UJki5qJZ8QG56G3"),
				solana.MustPublicKeyFromBase58("ENvNRNpE9dRZ8oj88SCCtgFcCpthdHwneb8D7kpZNoPB"),
				solana.MustPublicKeyFromBase58("AwrbHeCyniXaQhiJZkLhgWdUCteeWSGaSN1sTfLiY7xK"),
				solana.MustPublicKeyFromBase58("CSFsUupvJEQQd1F4SsXGACJaxQX4eropQMkGV2696eeQ"),
				solana.MustPublicKeyFromBase58("4VDjp6XQaxoZf5RGwiPU9NR1EXSZn2TP4ATMmiSzLfhb"),
				solana.MustPublicKeyFromBase58("2XgGZG4oP29U3w5h4nTk1V2LFHL23zKDPJjs3psGzLKQ"),
				solana.MustPublicKeyFromBase58("2AoLiH5kVBG2ot1qKoh4ro8F95KQb7HEBbJmkxrwYBec"),
			},
		},
	}

	MAINNET_PEER_ADDRESS, _ = addressToBytes32("0xCecAe061aa078e13b5e70D5F9eCee90a3F2B6AeA")

	SYMBOL_TOKEN = map[string]solana.PublicKey{
		"USDC": solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"),
		"USDT": solana.MustPublicKeyFromBase58("Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB"),
		"SOL":  solana.SolMint,
	}
	TOKEN_SYMBOL = map[solana.PublicKey]string{
		solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"): "USDC",
		solana.MustPublicKeyFromBase58("Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB"): "USDT",
		solana.SolMint: "SOL",
	}
)

func addressToBytes32(addr string) ([32]byte, error) {
	var out [32]byte
	b, err := hex.DecodeString(addr[2:])
	if err != nil {
		return out, err
	}
	copy(out[32-len(b):], b)
	return out, nil
}

func getDstEID() uint32 {
	if ChainName == "testnet" {
		return TESTNET_DST_EID
	}
	return MAIN_DST_EID
}

func getPeerAddress() [32]byte {
	return MAINNET_PEER_ADDRESS
}

func PackMessageForSolana(signer solana.PublicKey, messageBytes []byte) (*solana.Transaction, error) {
	builder := solana.NewTransactionBuilder()
	builder.AddInstruction(&solana.GenericInstruction{
		ProgID:    solana.ComputeBudget,
		DataBytes: []byte{3, 0, 0, 0, 0, 0, 0, 0, 0},
	})
	builder.AddInstruction(&solana.GenericInstruction{
		ProgID:    solana.ComputeBudget,
		DataBytes: []byte{2, 0, 0, 0, 0},
	})
	builder.AddInstruction(&solana.GenericInstruction{
		ProgID:    solana.MemoProgramID,
		DataBytes: messageBytes,
	})
	builder.SetFeePayer(signer)
	builder.SetRecentBlockHash(solana.Hash{})
	return builder.Build()
}

// --- PDA derivation ---

func getVaultAuthorityPDA(vaultProgram solana.PublicKey) solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{VAULT_AUTHORITY_SEED}, vaultProgram)
	return pda
}

func getSolVaultPDA(vaultProgram solana.PublicKey) solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{SOL_VAULT_SEED}, vaultProgram)
	return pda
}

func getBrokerPDA(vaultProgram solana.PublicKey, brokerHash string) solana.PublicKey {
	hash, _ := hex.DecodeString(brokerHash[2:])
	pda, _, _ := solana.FindProgramAddress([][]byte{BROKER_SEED, hash}, vaultProgram)
	return pda
}

func getTokenPDA(vaultProgram solana.PublicKey, tokenHash string) solana.PublicKey {
	hash, _ := hex.DecodeString(tokenHash[2:])
	pda, _, _ := solana.FindProgramAddress([][]byte{TOKEN_SEED, hash}, vaultProgram)
	return pda
}

func getOAppConfigPDA(vaultProgram solana.PublicKey) solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{OAPP_SEED}, vaultProgram)
	return pda
}

func eidBytes(eid uint32) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, eid)
	return buf
}

func getPeerPDA(vaultProgram solana.PublicKey, oappConfig solana.PublicKey, dstEid uint32) solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{PEER_SEED, oappConfig.Bytes(), eidBytes(dstEid)}, vaultProgram)
	return pda
}

func getEnforcedOptionsPDA(vaultProgram solana.PublicKey, oappConfig solana.PublicKey, dstEid uint32) solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{ENFORCED_OPTIONS_SEED, oappConfig.Bytes(), eidBytes(dstEid)}, vaultProgram)
	return pda
}

func getSendLibPDA() solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{MESSAGE_LIB_SEED}, SEND_LIB_PROGRAM_ID)
	return pda
}

func getSendLibConfigPDA(oappConfig solana.PublicKey, dstEid uint32) solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{SEND_LIBRARY_CONFIG_SEED, oappConfig.Bytes(), eidBytes(dstEid)}, ENDPOINT_PROGRAM_ID)
	return pda
}

func getDefaultSendLibConfigPDA(dstEid uint32) solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{SEND_LIBRARY_CONFIG_SEED, eidBytes(dstEid)}, ENDPOINT_PROGRAM_ID)
	return pda
}

func getSendLibInfoPDA(sendLib solana.PublicKey) solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{MESSAGE_LIB_SEED, sendLib.Bytes()}, ENDPOINT_PROGRAM_ID)
	return pda
}

func getEndpointSettingPDA() solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{ENDPOINT_SEED}, ENDPOINT_PROGRAM_ID)
	return pda
}

func getNoncePDA(vaultProgram solana.PublicKey, oappConfig solana.PublicKey, dstEid uint32) solana.PublicKey {
	peer := getPeerAddress()
	pda, _, _ := solana.FindProgramAddress([][]byte{NONCE_SEED, oappConfig.Bytes(), eidBytes(dstEid), peer[:]}, ENDPOINT_PROGRAM_ID)
	return pda
}

func getEventAuthorityPDA() solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{EVENT_SEED}, ENDPOINT_PROGRAM_ID)
	return pda
}

func getUlnSettingPDA() solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{ULN_SEED}, SEND_LIB_PROGRAM_ID)
	return pda
}

func getSendConfigPDA(oappConfig solana.PublicKey, dstEid uint32) solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{SEND_CONFIG_SEED, eidBytes(dstEid), oappConfig.Bytes()}, SEND_LIB_PROGRAM_ID)
	return pda
}

func getDefaultSendConfigPDA(dstEid uint32) solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{SEND_CONFIG_SEED, eidBytes(dstEid)}, RECEIVE_LIB_PROGRAM_ID)
	return pda
}

func getUlnEventAuthorityPDA() solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{EVENT_SEED}, SEND_LIB_PROGRAM_ID)
	return pda
}

func getExecutorConfigPDA() solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{EXECUTOR_CONFIG_SEED}, EXECUTOR_PROGRAM_ID)
	return pda
}

func getPriceFeedPDA() solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{PRICE_FEED_SEED}, PRICE_FEED_PROGRAM_ID)
	return pda
}

func getDvnConfigPDA() solana.PublicKey {
	pda, _, _ := solana.FindProgramAddress([][]byte{DVN_CONFIG_SEED}, DVN_PROGRAM_ID)
	return pda
}
