package cli

import "sync"

const maxConcurrency = 10

// ClampConcurrency bounds concurrency into a safe worker-pool range.
func ClampConcurrency(value int) int {
	if value < 1 {
		return 1
	}
	if value > maxConcurrency {
		return maxConcurrency
	}
	return value
}

// RunParallel executes work across a bounded worker pool and returns results
// in the same index order as the input domain.
func RunParallel[T any](count int, concurrency int, work func(index int) T) []T {
	results := make([]T, count)
	if count == 0 {
		return results
	}

	concurrency = ClampConcurrency(concurrency)
	if concurrency == 1 {
		for idx := 0; idx < count; idx++ {
			results[idx] = work(idx)
		}
		return results
	}

	jobs := make(chan int)
	var wg sync.WaitGroup

	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				results[idx] = work(idx)
			}
		}()
	}

	for idx := 0; idx < count; idx++ {
		jobs <- idx
	}
	close(jobs)
	wg.Wait()

	return results
}
