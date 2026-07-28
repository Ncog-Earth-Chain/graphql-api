package pg

import (
	"context"
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
)

// TestEpochReadsOneById covers the single-epoch reader that was missing.
//
// The table, the writer and the list query all existed; only this was absent, so every
// single-epoch read went to the SFC contract over RPC for a row on the primary key -- and
// Epoch.Duration doubled it, because an epoch's length is only knowable by comparing it with
// the one before.
func TestEpochReadsOneById(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.pool.Exec(ctx, `DELETE FROM epoch`); err != nil {
		t.Fatalf("clean: %v", err)
	}

	mk := func(id uint64, endTime uint64) *types.Epoch {
		return &types.Epoch{
			Id:                    hexutil.Uint64(id),
			EndTime:               hexutil.Uint64(endTime),
			EpochFee:              hexutil.Big(*big.NewInt(1000)),
			BaseRewardPerSecond:   hexutil.Big(*big.NewInt(2000)),
			TotalBaseRewardWeight: hexutil.Big(*big.NewInt(3000)),
			TotalTxRewardWeight:   hexutil.Big(*big.NewInt(4000)),
			StakeTotalAmount:      hexutil.Big(*big.NewInt(5000)),
			TotalSupply:           hexutil.Big(*big.NewInt(6000)),
		}
	}

	for _, e := range []*types.Epoch{mk(10, 1700000000), mk(11, 1700000600)} {
		if err := s.AddEpoch(ctx, e); err != nil {
			t.Fatalf("add epoch %d: %v", uint64(e.Id), err)
		}
	}

	t.Run("known epoch", func(t *testing.T) {
		got, err := s.Epoch(ctx, 11)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if got == nil {
			t.Fatal("epoch 11 was stored but did not come back")
		}
		if uint64(got.Id) != 11 || uint64(got.EndTime) != 1700000600 {
			t.Errorf("epoch = {id:%d end:%d}, want {11 1700000600}", uint64(got.Id), uint64(got.EndTime))
		}
	})

	t.Run("absent epoch is not an error", func(t *testing.T) {
		// An epoch the scanner has not reached is an ordinary outcome; the repository
		// falls back to the node on nil, so returning an error here would turn a normal
		// case into a failed request.
		got, err := s.Epoch(ctx, 9999)
		if err != nil {
			t.Fatalf("absent epoch returned an error: %v", err)
		}
		if got != nil {
			t.Errorf("absent epoch returned %v, want nil", got)
		}
	})

	t.Run("duration is derivable from two stored epochs", func(t *testing.T) {
		cur, err := s.Epoch(ctx, 11)
		if err != nil {
			t.Fatalf("read 11: %v", err)
		}
		prev, err := s.Epoch(ctx, 10)
		if err != nil {
			t.Fatalf("read 10: %v", err)
		}
		if cur == nil || prev == nil {
			t.Fatal("both epochs must be readable for Duration to resolve without the node")
		}
		if d := uint64(cur.EndTime) - uint64(prev.EndTime); d != 600 {
			t.Errorf("duration = %d, want 600", d)
		}
	})
}
