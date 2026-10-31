package herdr

import (
	"context"
	"time"
)

// runCore keeps Home current from herdr-core: subscribe, then one complete
// read, then another complete read after each event that can change Home.
// After a disconnect it subscribes again and reads again; nothing is replayed.
func (p *Projector) runCore(ctx context.Context) {
	retry := time.NewTimer(0)
	defer retry.Stop()
	lastDetail := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-retry.C:
		}
		subscription, err := p.core.Subscribe(ctx)
		if err == nil {
			readFailed, followErr := p.followCore(ctx, subscription)
			_ = subscription.Close()
			if ctx.Err() != nil {
				return
			}
			if !readFailed {
				// The event stream ended: keep the last Home while reconnecting.
				p.markGap()
				lastDetail = ""
				resetTimer(retry, projectorRetryDelay)
				continue
			}
			err = followErr
		}
		if ctx.Err() != nil {
			return
		}
		p.publishError(err)
		detail := p.Current().Detail
		if detail != "" && detail != lastDetail {
			p.core.logger.Error("Cannot use this Herdr", "herdr_core_socket", p.core.socketPath, "detail", detail)
		}
		lastDetail = detail
		resetTimer(retry, projectorRetryDelay)
	}
}

// followCore returns when the subscription or a read fails; readFailed says
// which, so a failed read shows its reason while a dropped stream reconnects.
func (p *Projector) followCore(ctx context.Context, subscription *CoreSubscription) (readFailed bool, err error) {
	changed := make(chan struct{}, 1)
	lost := make(chan error, 1)
	go func() {
		for {
			changes, err := subscription.Next()
			if err != nil {
				lost <- err
				return
			}
			if changes {
				latchDirty(changed)
			}
		}
	}()

	baseline := true
	for {
		takeRefreshSignals(changed, p.refresh)
		read, err := p.core.Read(ctx)
		if err != nil {
			return true, err
		}
		snapshot, home, err := ProjectCore(read, p.homeFolder)
		if err != nil {
			return true, err
		}
		p.publishHome(snapshot, home, baseline)
		baseline = false
		if takeRefreshSignals(changed, p.refresh) {
			continue
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case err := <-lost:
			return false, err
		case <-changed:
		case <-p.refresh:
		}
	}
}
