package lifecycle

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSealExcludesConcurrentAdmission(t *testing.T) {
	for i := 0; i < 50; i++ {
		var g Group
		var wg sync.WaitGroup
		start := make(chan struct{})
		for n := 0; n < 20; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if done, ok := g.Enter("job"); ok {
					done()
				}
			}()
		}
		close(start)
		g.Seal()
		if _, ok := g.Enter("after_seal"); ok {
			t.Fatal("work admitted after seal")
		}
		wg.Wait()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := g.Wait(ctx); err != nil {
			t.Fatal(err)
		}
		cancel()
	}
}
func TestWaitObservesCompletionNotCancellation(t *testing.T) {
	var g Group
	done, _ := g.Enter("job")
	g.Seal()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if g.Wait(ctx) == nil {
		t.Fatal("unfinished work reported complete")
	}
	done()
	if err := g.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
