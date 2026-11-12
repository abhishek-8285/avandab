package experiments

import (
	"sync"
	"testing"
)

// Ratchet Law: Test that newID does not produce collisions under rapid minting,
// matching the property proven in internal/customer/application/customer_service_id_test.go.
func TestNewID_UniqueUnderRapidMinting(t *testing.T) {
	const total = 5000
	var mu sync.Mutex
	seen := make(map[string]struct{}, total)

	var wg sync.WaitGroup
	workers := 10
	perWorker := total / workers

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				id := newID("evt")
				mu.Lock()
				if _, dup := seen[id]; dup {
					t.Errorf("duplicate id minted: %s", id)
				}
				seen[id] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
}
