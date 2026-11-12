package gstn

import (
	"context"
	"sync"
	"testing"
)

func TestMockEInvoiceClient_AckNoUniquenessUnderRapidCalls(t *testing.T) {
	client := NewMockEInvoiceClient(Config{Enabled: true, UseMock: true})
	const total = 1000
	var mu sync.Mutex
	seen := make(map[string]struct{}, total)

	var wg sync.WaitGroup
	workers := 5
	perWorker := total / workers

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				resp, err := client.GenerateIRN(context.Background(), InvoiceView{
					InvoiceID:     "inv-test",
					InvoiceNumber: "INV-001",
				})
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				mu.Lock()
				if _, dup := seen[resp.AckNo]; dup {
					t.Errorf("duplicate ackNo minted: %s", resp.AckNo)
				}
				seen[resp.AckNo] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
}
