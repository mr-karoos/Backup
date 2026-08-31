package secretcrypto

import "testing"

func TestZeroBytes(t *testing.T) {
	t.Run("zeros non-empty slice", func(t *testing.T) {
		b := []byte{1, 2, 3, 4, 5, 255, 128}
		ZeroBytes(b)

		for i, v := range b {
			if v != 0 {
				t.Errorf("expected byte at index %d to be 0, got %d", i, v)
			}
		}
	})

	t.Run("handles nil slice safely", func(t *testing.T) {
		var b []byte
		ZeroBytes(b)
	})

	t.Run("handles empty slice safely", func(t *testing.T) {
		b := []byte{}
		ZeroBytes(b)
	})
}
