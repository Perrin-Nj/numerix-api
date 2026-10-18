package auth

import "testing"

// TestHashConcurrent checks that many parallel hashes all finish and verify (the slot cap queues, never fails).
func TestHashConcurrent(t *testing.T) {
	done := make(chan error, 12) // one result per goroutine
	for i := 0; i < 12; i++ {
		go func() {
			h, err := Hash("1234")
			if err == nil {
				var ok bool
				if ok, err = Verify("1234", h); err == nil && !ok {
					err = ErrInvalidHash
				}
			}
			done <- err
		}()
	}
	for i := 0; i < 12; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
