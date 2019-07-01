package hlc

import (
	"github.com/HalalChain/qitmeer-lib/common/hash"
	"math/big"
)

// HashToBig converts a hash.Hash into a big.Int that can be used to
// perform math comparisons.
func HashToBig(hash *hash.Hash) *big.Int {
	// A Hash is in little-endian, but the big package wants the bytes in
	// big-endian, so reverse them.
	buf := *hash
	blen := len(buf)
	for i := 0; i < blen/2; i++ {
		buf[i], buf[blen-1-i] = buf[blen-1-i], buf[i]
	}

	return new(big.Int).SetBytes(buf[:])
}