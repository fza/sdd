package finders

import (
	"runtime"
	"sync"
)

// forEachConcurrently calls fn for every index below n on GOMAXPROCS workers
// and returns when all calls have. fn writes its result into its own slot.
func forEachConcurrently(n int, fn func(i int)) {
	workers := min(runtime.GOMAXPROCS(0), n)
	next := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for i := range next {
				fn(i)
			}
		})
	}
	for i := range n {
		next <- i
	}
	close(next)
	wg.Wait()
}
