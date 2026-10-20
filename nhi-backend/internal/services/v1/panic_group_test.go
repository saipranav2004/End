package v1

import "testing"

func TestPanicGroupReraisesInWaiter(t *testing.T) {
	var wg panicGroup
	wg.Add(2)
	go func() { defer wg.Done(); panic("boom") }()
	go func() { defer wg.Done() }()
	defer func() {
		if recover() == nil {
			t.Fatal("Wait did not re-raise the goroutine's panic")
		}
	}()
	wg.Wait()
}
