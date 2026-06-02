package worker

import (
	"context"
	"sync"

	"github.com/tradenexus/backend/internal/logger"
)

// WorkerFunc is a function executed by a worker pool worker.
type WorkerFunc func(ctx context.Context, job interface{})

// WorkerPool manages a pool of goroutine workers for concurrent job processing.
type WorkerPool struct {
	name    string
	workers int
	jobChan chan interface{}
	fn      WorkerFunc
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewWorkerPool creates a new worker pool.
func NewWorkerPool(name string, workers int, bufferSize int, fn WorkerFunc) *WorkerPool {
	return &WorkerPool{
		name:    name,
		workers: workers,
		jobChan: make(chan interface{}, bufferSize),
		fn:      fn,
	}
}

// Start launches all workers in the pool.
func (wp *WorkerPool) Start(ctx context.Context) {
	log := logger.WithComponent("worker." + wp.name)
	wp.ctx, wp.cancel = context.WithCancel(ctx)

	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go func(id int) {
			defer wp.wg.Done()
			log.Debug().Int("workerID", id).Msg("Worker started")

			for {
				select {
				case <-wp.ctx.Done():
					return
				case job, ok := <-wp.jobChan:
					if !ok {
						return
					}
					wp.fn(wp.ctx, job)
				}
			}
		}(i)
	}

	log.Info().Int("workers", wp.workers).Msg("Worker pool started")
}

// Submit sends a job to the pool. Non-blocking if buffer has space.
func (wp *WorkerPool) Submit(job interface{}) {
	select {
	case wp.jobChan <- job:
	default:
		log := logger.WithComponent("worker." + wp.name)
		log.Warn().Msg("Worker pool job channel full, dropping job")
	}
}

// Stop shuts down the worker pool gracefully.
func (wp *WorkerPool) Stop() {
	wp.cancel()
	close(wp.jobChan)
	wp.wg.Wait()
}
