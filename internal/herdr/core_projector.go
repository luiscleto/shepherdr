package herdr

import (
	"context"
	"time"
)

// appWindowInterval is how often Home asks whether the Herdr app window is
// open while herdr-core is live.
const appWindowInterval = 5 * time.Second

// AppWindow says whether the Herdr app window answers.
type AppWindow interface {
	WindowOpen(context.Context) bool
}

// SetAppWindow makes Home on herdr-core offer the app window's actions while
// app says the window is open. Call it before Run.
func (p *Projector) SetAppWindow(app AppWindow) {
	p.app = app
}

func (p *Projector) appWindowOpen(ctx context.Context) bool {
	return p.app != nil && p.app.WindowOpen(ctx)
}

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

	// The window is asked at the start, every appWindowInterval, and on each
	// requested refresh, which follows every action. Core events alone don't
	// ask it. A change publishes one complete Home.
	var windowTick <-chan time.Time
	if p.app != nil {
		ticker := time.NewTicker(appWindowInterval)
		defer ticker.Stop()
		windowTick = ticker.C
	}
	windowOpen := p.appWindowOpen(ctx)
	takeSignals := func() bool {
		dirty := takeRefreshSignals(changed, nil)
		if takeRefreshSignals(nil, p.refresh) {
			windowOpen = p.appWindowOpen(ctx)
			dirty = true
		}
		return dirty
	}
	baseline := true
	for {
		takeSignals()
		read, err := p.core.Read(ctx)
		if err != nil {
			return true, err
		}
		read.AppWindowOpen = windowOpen
		snapshot, home, err := ProjectCore(read, p.homeFolder)
		if err != nil {
			return true, err
		}
		p.publishHome(snapshot, home, baseline)
		baseline = false
		if takeSignals() {
			continue
		}
	wait:
		for {
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case err := <-lost:
				return false, err
			case <-changed:
				break wait
			case <-p.refresh:
				windowOpen = p.appWindowOpen(ctx)
				break wait
			case <-windowTick:
				if open := p.appWindowOpen(ctx); open != windowOpen {
					windowOpen = open
					break wait
				}
			}
		}
	}
}
