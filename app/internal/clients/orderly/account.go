package orderly

import (
	"fmt"
	"math/big"

	"github.com/btcsuite/btcutil/base58"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func GetAccountID(userAddress, brokerID string) (common.Hash, error) {
	brokerIDHash := crypto.Keccak256Hash([]byte(brokerID))

	var (
		arguments abi.Arguments
		pubKeyArg any
	)

	if common.IsHexAddress(userAddress) {
		arguments = abi.Arguments{
			{Type: mustABIType("address")},
			{Type: mustABIType("bytes32")},
		}
		pubKeyArg = common.HexToAddress(userAddress)
	} else {
		arguments = abi.Arguments{
			{Type: mustABIType("bytes32")},
			{Type: mustABIType("bytes32")},
		}
		pubKeyArg = [32]byte(base58.Decode(userAddress))
	}

	encoded, err := arguments.Pack(pubKeyArg, brokerIDHash)
	if err != nil {
		return common.Hash{}, fmt.Errorf("abi encode: %w", err)
	}

	return crypto.Keccak256Hash(encoded), nil
}

func mustABIType(t string) abi.Type {
	typ, err := abi.NewType(t, "", nil)
	if err != nil {
		panic(err)
	}
	return typ
}

func keccak256String(input string) []byte {
	return crypto.Keccak256([]byte(input))
}

func ParseTokenHash(tokenSymbol string) string {
	hash := crypto.Keccak256([]byte(tokenSymbol))
	return "0x" + common.Bytes2Hex(hash)
}

// CreateWithdrawMessage builds the keccak256 hash (hex-encoded as bytes)
// that the user's wallet must sign via Solana memo to withdraw from Orderly.
func CreateWithdrawMessage(msg WithdrawMessage) ([]byte, error) {
	brokerIDHash := keccak256String(msg.BrokerID)
	tokenHash := keccak256String(msg.Token)
	salt := keccak256String("Orderly Network")

	receiverBytes := base58.Decode(msg.Receiver)

	arguments := abi.Arguments{
		{Type: mustABIType("bytes32")},
		{Type: mustABIType("bytes32")},
		{Type: mustABIType("uint256")},
		{Type: mustABIType("bytes32")},
		{Type: mustABIType("uint256")},
		{Type: mustABIType("uint64")},
		{Type: mustABIType("uint64")},
		{Type: mustABIType("bytes32")},
	}

	packed, err := arguments.Pack(
		[32]byte(brokerIDHash),
		[32]byte(tokenHash),
		new(big.Int).SetUint64(msg.ChainID),
		[32]byte(receiverBytes),
		new(big.Int).SetUint64(msg.Amount),
		msg.WithdrawNonce,
		msg.Timestamp,
		[32]byte(salt),
	)
	if err != nil {
		return nil, err
	}

	msgToSign := crypto.Keccak256(packed)
	return []byte(common.Bytes2Hex(msgToSign)), nil
}
