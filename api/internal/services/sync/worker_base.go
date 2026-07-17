package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/riverqueue/river"
)

// SyncWorker defines the service-specific work that concrete workers implement.
// BaseSyncWorker handles infrastructure concerns and then calls Execute.
type SyncWorker interface {
	ServiceName() string
	Execute(ctx context.Context) error
}

// BaseSyncWorker centralizes shared workflow for sync services:
// rate-limit check -> circuit breaker check -> execute -> update infra state.
type BaseSyncWorker struct {
	Infra *Infra
}

func (b *BaseSyncWorker) Handle(ctx context.Context, service string, execute func(context.Context) error) error {
	if b == nil || b.Infra == nil {
		return fmt.Errorf("sync infra not initialized")
	}
	allowed, retryAfter, err := b.Infra.AllowCircuit(ctx, service)
	if err != nil {
		return err
	}
	if !allowed {
		return river.JobSnooze(retryAfter)
	}

	rateAllowed, err := b.Infra.ConsumeRateLimit(ctx, service)
	if err != nil {
		return err
	}
	if !rateAllowed {
		return river.JobSnooze(1 * time.Second)
	}

	if err := execute(ctx); err != nil {
		_, _ = b.Infra.RecordFailure(ctx, service, err.Error())
		return err
	}
	return b.Infra.RecordSuccess(ctx, service)
}
