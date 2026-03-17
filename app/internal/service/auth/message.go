package auth

import (
	"encoding/hex"
	"math/big"

	"mcp-server/app/internal/clients/orderly"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/crypto"
)

func mustType(t string) abi.Type {
	typ, err := abi.NewType(t, "", nil)
	if err != nil {
		panic(err)
	}
	return typ
}

// CreateRegistrationMessage builds the keccak256 hash (hex-encoded as bytes) that
// the user's wallet must sign via Solana memo to register an Orderly account.
func CreateRegistrationMessage(brokerID, nonce string, chainID, timestamp int64) ([]byte, error) {
	brokerIDHash := crypto.Keccak256Hash([]byte(brokerID))

	arguments := abi.Arguments{
		{Type: mustType("bytes32")},
		{Type: mustType("uint256")},
		{Type: mustType("uint256")},
		{Type: mustType("uint256")},
	}

	registrationNonce, _ := new(big.Int).SetString(nonce, 10)

	encoded, err := arguments.Pack(
		brokerIDHash,
		new(big.Int).SetInt64(chainID),
		new(big.Int).SetInt64(timestamp),
		registrationNonce,
	)
	if err != nil {
		return nil, err
	}

	msgToSign := crypto.Keccak256(encoded)
	return []byte(hex.EncodeToString(msgToSign)), nil
}

// CreateOrderlyKeyMessage builds the keccak256 hash (hex-encoded as bytes) that
// the user's wallet must sign to register an Orderly key.
func CreateOrderlyKeyMessage(msg orderly.OrderlyKeyMessage) ([]byte, error) {
	brokerIDHash := crypto.Keccak256Hash([]byte(msg.BrokerID))
	orderlyKeyHash := crypto.Keccak256Hash([]byte(msg.OrderlyKey))
	scopeHash := crypto.Keccak256Hash([]byte(msg.Scope))

	args := abi.Arguments{
		{Type: mustType("bytes32")},
		{Type: mustType("bytes32")},
		{Type: mustType("bytes32")},
		{Type: mustType("uint256")},
		{Type: mustType("uint256")},
		{Type: mustType("uint256")},
	}

	encoded, err := args.Pack(
		brokerIDHash,
		orderlyKeyHash,
		scopeHash,
		new(big.Int).SetUint64(uint64(msg.ChainID)),
		big.NewInt(msg.Timestamp),
		big.NewInt(msg.Expiration),
	)
	if err != nil {
		return nil, err
	}

	msgToSign := crypto.Keccak256(encoded)
	return []byte(hex.EncodeToString(msgToSign)), nil
}
