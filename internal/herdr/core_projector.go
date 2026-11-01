package herdr

import (
	"context"
	"time"
)

// appWindowInterval is how often Home asks whether the Herdr app window is
// open.
const appWindowInterval = 5 * time.Second

// AppWindow says whether the Herdr app window answers.
type AppWindow interface {
	WindowOpen(context.Context) bool
}

// SetAppWindow makes Home on herdr-core offer the app's actions: those the
// window does while app says it is open, and New space and New terminal
// through core while it is closed. Call it before Run.
func (p *Projector) SetAppWindow(app AppWindow) {
	p.app = app
}

// appWindowWatch asks the app window outside the core loop, so a slow or
// hung window never holds up core reads, events or reconnects. The loop asks
// through ask and takes the latest answer from answers; state is the answer
// Home shows, and only the core loop uses it. It stays AppWindowUnpaired until
// the first answer.
type appWindowWatch struct {
	ask     chan struct{}
	answers chan bool
	state   AppWindowState
}

// watchAppWindow asks the window every appWindowInterval and on each ask.
// Without the app it never answers, and Home offers no app actions.
func (p *Projector) watchAppWindow(ctx context.Context) *appWindowWatch {
	window := &appWindowWatch{ask: make(chan struct{}, 1)}
	if p.app == nil {
		return window
	}
	answers := make(chan bool, 1)
	window.answers = answers
	go func() {
		ticker := time.NewTicker(appWindowInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-window.ask:
			}
			open := p.app.WindowOpen(ctx)
			// Only the latest answer waits.
			select {
			case <-answers:
			default:
			}
			answers <- open
		}
	}()
	return window
}

// take reports whether a waiting answer changed open.
func (w *appWindowWatch) take() bool {
	select {
	case open := <-w.answers:
		return w.changed(open)
	default:
		return false
	}
}

func (w *appWindowWatch) changed(open bool) bool {
	state := AppWindowClosed
	if open {
		state = AppWindowOpen
	}
	if state == w.state {
		return false
	}
	w.state = state
	return true
}

// runCore keeps Home current from herdr-core: subscribe, then one complete
// read, then another complete read after each event that can change Home.
// After a disconnect it subscribes again and reads again; nothing is replayed.
func (p *Projector) runCore(ctx context.Context) {
	retry := time.NewTimer(0)
	defer retry.Stop()
	window := p.watchAppWindow(ctx)
	lastDetail := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-retry.C:
		}
		subscription, err := p.core.Subscribe(ctx)
		if err == nil {
			readFailed, followErr := p.followCore(ctx, subscription, window)
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
func (p *Projector) followCore(ctx context.Context, subscription *CoreSubscription, window *appWindowWatch) (readFailed bool, err error) {
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

	// The window is asked on subscribe and on each requested refresh, which
	// follows every action, as well as on its own cadence. Core events alone
	// don't ask it. Reads use the latest answer; a changed answer publishes
	// one complete Home.
	latchDirty(window.ask)
	takeSignals := func() bool {
		dirty := takeRefreshSignals(changed, p.repositories.resolved)
		if takeRefreshSignals(nil, p.refresh) {
			latchDirty(window.ask)
			dirty = true
		}
		return window.take() || dirty
	}
	baseline := true
	for {
		takeSignals()
		read, err := p.core.Read(ctx)
		if err != nil {
			return true, err
		}
		read.AppWindow = window.state
		snapshot, home, err := projectCore(read, p.homeFolder, p.repositories.lookup)
		if err != nil {
			return true, err
		}
		p.repositories.settle()
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
			case <-p.repositories.resolved:
				break wait
			case <-p.refresh:
				latchDirty(window.ask)
				break wait
			case open := <-window.answers:
				if window.changed(open) {
					break wait
				}
			}
		}
	}
}
