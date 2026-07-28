package resolvers

import (
	"math/big"
	"ncogearthchain-api-graphql/internal/types"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
)

// TestStakeIsLocked pins down the meaning of "locked right now".
//
// The comparison used to be LockedUntil < now, which is true exactly when the lock has
// EXPIRED. That reported every finished lock as active and every active lock as finished
// -- an inversion no caller could have worked around, and one nothing caught because the
// resolver had no test at all.
func TestStakeIsLocked(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	lock := func(amount int64, until time.Time) *types.DelegationLock {
		return &types.DelegationLock{
			LockedAmount: hexutil.Big(*big.NewInt(amount)),
			LockedUntil:  hexutil.Uint64(until.Unix()),
		}
	}

	cases := []struct {
		name string
		in   *types.DelegationLock
		want bool
	}{
		{
			name: "lock ends in the future: locked",
			in:   lock(1000, now.Add(72*time.Hour)),
			want: true,
		},
		{
			name: "lock already ended: not locked",
			in:   lock(1000, now.Add(-time.Second)),
			want: false,
		},
		{
			name: "lock ends exactly now: not locked, the period is over",
			in:   lock(1000, now),
			want: false,
		},
		{
			name: "no amount locked: not locked, however far the end is",
			in:   lock(0, now.Add(72*time.Hour)),
			want: false,
		},
		{
			name: "negative amount: not locked",
			in:   lock(-1, now.Add(72*time.Hour)),
			want: false,
		},
		{
			name: "no lock record at all: not locked",
			in:   nil,
			want: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stakeIsLocked(c.in, now); got != c.want {
				t.Errorf("stakeIsLocked() = %v, want %v", got, c.want)
			}
		})
	}
}
