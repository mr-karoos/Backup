package secretcrypto

// ZeroBytes overwrites all bytes in the provided slice with zeroes to minimize the duration
// sensitive material remains in memory buffers.
//
// Note: This provides best-effort memory zeroization. In garbage-collected runtimes like Go,
// copies of memory made during stack growth, heap allocation, or GC compaction cannot be
// deterministically wiped from physical RAM.
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
