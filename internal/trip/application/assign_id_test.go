package application

import (
	"sync"
	"testing"
)

func TestTripApplication_OverrideAndComplianceIDUniqueness(t *testing.T) {
	const total = 5000
	var mu sync.Mutex
	seenOvr := make(map[string]struct{}, total)
	seenChk := make(map[string]struct{}, total)

	var wg sync.WaitGroup
	workers := 10
	perWorker := total / workers

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				ovr := newOverrideID()
				chk := newComplianceCheckID()

				mu.Lock()
				if _, dup := seenOvr[ovr]; dup {
					t.Errorf("duplicate override id minted: %s", ovr)
				}
				seenOvr[ovr] = struct{}{}

				if _, dup := seenChk[chk]; dup {
					t.Errorf("duplicate compliance check id minted: %s", chk)
				}
				seenChk[chk] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
}
