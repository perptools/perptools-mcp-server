package orderly

import (
	"context"
	"encoding/binary"
	"os"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// TestSendConfigDVNProbe reads the LIVE LayerZero ULN send config for the
// Orderly vault OApp and prints the DVN set. The deposit quote (oapp_quote)
// fails with ULN error 6019 (InvalidAccountLength) when the worker accounts we
// pass don't equal (required_dvns + optional_dvns) * 4. Our code hardcodes ONE
// DVN; this probe shows what the chain actually requires now.
//
// Read-only. Run: go test ./app/internal/clients/orderly/ -run TestSendConfigDVNProbe -v
func TestSendConfigDVNProbe(t *testing.T) {
	rpcURL := os.Getenv("SOLANA_RPC_URL")
	if rpcURL == "" {
		rpcURL = "https://api.mainnet-beta.solana.com"
	}
	cl := rpc.New(rpcURL)
	ctx := context.Background()

	vaultProgram := SOLANA_VAULT_PROGRAM_ID[ChainName]
	oappConfig := getOAppConfigPDA(vaultProgram)
	dstEID := getDstEID()

	t.Logf("vault=%s oappConfig=%s dstEID=%d", vaultProgram, oappConfig, dstEID)
	t.Logf("SEND_LIB_PROGRAM_ID=%s", SEND_LIB_PROGRAM_ID)

	targets := []struct {
		label string
		pda   solana.PublicKey
	}{
		{"custom send_config", getSendConfigPDA(oappConfig, dstEID)},
		{"default send_config", getDefaultSendConfigPDA(dstEID)},
	}

	for _, tg := range targets {
		t.Logf("==== %s: %s ====", tg.label, tg.pda)
		info, err := cl.GetAccountInfo(ctx, tg.pda)
		if err != nil {
			t.Logf("  not found / error: %v", err)
			continue
		}
		if info == nil || info.Value == nil {
			t.Logf("  account does not exist (uninitialized)")
			continue
		}
		data := info.Value.Data.GetBinary()
		t.Logf("  owner=%s dataLen=%d", info.Value.Owner, len(data))

		// SendConfig = 8 disc + bump(1) + UlnConfig{ confirmations(8),
		// required_dvn_count(1)@17, optional_dvn_count(1)@18, threshold(1)@19,
		// required_dvns: vec<pubkey> @20 (u32 len + n*32), optional_dvns: ... }
		if len(data) < 24 {
			t.Logf("  too short to decode UlnConfig")
			continue
		}
		conf := binary.LittleEndian.Uint64(data[9:17])
		reqCount := data[17]
		optCount := data[18]
		thr := data[19]
		t.Logf("  confirmations=%d required_dvn_count=%d optional_dvn_count=%d threshold=%d", conf, reqCount, optCount, thr)

		off := 20
		readVec := func(name string) {
			if off+4 > len(data) {
				t.Logf("  %s: truncated", name)
				return
			}
			n := int(binary.LittleEndian.Uint32(data[off : off+4]))
			off += 4
			t.Logf("  %s vec len=%d", name, n)
			for i := 0; i < n; i++ {
				if off+32 > len(data) {
					t.Logf("    [%d] truncated", i)
					return
				}
				pk := solana.PublicKeyFromBytes(data[off : off+32])
				off += 32
				owner := "?"
				if oi, err := cl.GetAccountInfo(ctx, pk); err == nil && oi != nil && oi.Value != nil {
					owner = oi.Value.Owner.String()
				}
				t.Logf("    [%d] dvn_config=%s  owner(program)=%s", i, pk, owner)
			}
		}
		readVec("required_dvns")
		readVec("optional_dvns")

		total := int(reqCount) + int(optCount)
		t.Logf("  => worker DVN accounts expected = %d (=%d DVNs * 4); our code hardcodes 1 DVN (4)", total*4, total)
	}
}
