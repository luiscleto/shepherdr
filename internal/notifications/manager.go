package notifications

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/luisc/shepherdr/internal/herdr"
)

const (
	deliveryWorkers = 2
	deliveryQueue   = 32
	endpointTimeout = 4 * time.Second
)

type Manager struct {
	store     *Store
	validator *endpointValidator
	sender    deliverySender
	logger    *slog.Logger
	events    chan Event
	authority TrustAuthority

	evaluatorMu sync.Mutex
	evaluator   snapshotEvaluator
}

type TrustAuthority interface {
	WithActiveTrustRead(string, func() error) error
}

func NewManager(store *Store, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	validator := newEndpointValidator(nil)
	return &Manager{
		store:     store,
		validator: validator,
		sender:    newWebPushSender(validator),
		logger:    logger,
		events:    make(chan Event, deliveryQueue),
	}
}

func (m *Manager) Start(ctx context.Context) {
	for index := 0; index < deliveryWorkers; index++ {
		go m.deliver(ctx)
	}
}

func (m *Manager) ObserveSnapshot(snapshot herdr.Snapshot, baseline bool) {
	m.evaluatorMu.Lock()
	events := m.evaluator.Observe(snapshot, baseline)
	m.evaluatorMu.Unlock()
	for _, event := range events {
		m.logger.Info("Notification candidate observed")
		select {
		case m.events <- event:
			m.logger.Info("Notification candidate enqueued")
		default:
			m.logger.Info("Notification candidate dropped")
		}
	}
}

func (m *Manager) Config() (contact, publicKey string, configured bool, err error) {
	return m.store.Config()
}

func (m *Manager) EnableProtected(authority TrustAuthority, activeTrustIDs map[string]struct{}) error {
	if authority == nil {
		return errNotConfigured
	}
	m.authority = authority
	if err := m.store.EnableProtected(activeTrustIDs); err != nil {
		return err
	}
	return nil
}

func (m *Manager) Lookup(endpoint string) (EventSettings, bool, error) {
	if _, err := validateEndpointURL(endpoint); err != nil {
		return EventSettings{}, false, err
	}
	return m.store.Lookup(endpoint)
}

func (m *Manager) LookupOwned(endpoint, trustID string) (EventSettings, bool, error) {
	if _, err := validateEndpointURL(endpoint); err != nil {
		return EventSettings{}, false, err
	}
	return m.store.LookupOwned(endpoint, trustID)
}

func (m *Manager) Save(ctx context.Context, browser BrowserSubscription, events EventSettings) error {
	_, _, configured, err := m.store.Config()
	if err != nil {
		return err
	}
	if !configured {
		return errNotConfigured
	}
	subscription := Subscription{
		Endpoint:       browser.Endpoint,
		ExpirationTime: browser.ExpirationTime,
		Keys:           browser.Keys,
		Events:         events,
	}
	if err := validateSubscriptionShape(subscription); err != nil {
		return err
	}
	validationCtx, cancel := context.WithTimeout(ctx, endpointTimeout)
	defer cancel()
	if err := m.validator.validate(validationCtx, subscription.Endpoint); err != nil {
		return err
	}
	return m.store.Upsert(subscription)
}

func (m *Manager) Prepare(ctx context.Context, browser BrowserSubscription, events EventSettings) (Subscription, error) {
	_, _, configured, err := m.store.Config()
	if err != nil {
		return Subscription{}, err
	}
	if !configured {
		return Subscription{}, errNotConfigured
	}
	subscription := Subscription{
		Endpoint: browser.Endpoint, ExpirationTime: browser.ExpirationTime, Keys: browser.Keys, Events: events,
	}
	if err := validateSubscriptionShape(subscription); err != nil {
		return Subscription{}, err
	}
	validationCtx, cancel := context.WithTimeout(ctx, endpointTimeout)
	defer cancel()
	if err := m.validator.validate(validationCtx, subscription.Endpoint); err != nil {
		return Subscription{}, err
	}
	return subscription, nil
}

func (m *Manager) CommitOwned(subscription Subscription, trustID string) error {
	return m.store.UpsertOwned(subscription, trustID)
}

func (m *Manager) RemoveOwned(endpoint, trustID string) (bool, error) {
	if _, err := validateEndpointURL(endpoint); err != nil {
		return false, err
	}
	return m.store.RemoveOwned(endpoint, trustID)
}

func (m *Manager) RemoveTrustSubscriptions(trustID string) error {
	return m.store.RemoveTrustSubscriptions(trustID)
}

func (m *Manager) ResetProtectedSubscriptions() error {
	return m.store.ResetProtectedSubscriptions()
}

func (m *Manager) Remove(endpoint string) (bool, error) {
	if _, err := validateEndpointURL(endpoint); err != nil {
		return false, err
	}
	return m.store.Remove(endpoint)
}

func (m *Manager) deliver(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-m.events:
			m.deliverEvent(ctx, event)
		}
	}
}

func (m *Manager) deliverEvent(ctx context.Context, event Event) {
	contact, publicKey, privateKey, subscriptions, configured := m.store.deliveryState()
	if !configured {
		return
	}
	for _, subscription := range subscriptions {
		if !event.selected(subscription.Events) {
			continue
		}
		outcome := sendFailed
		if m.authority != nil {
			if subscription.TrustID == "" {
				continue
			}
			if err := m.authority.WithActiveTrustRead(subscription.TrustID, func() error {
				outcome = m.sender.Send(ctx, event, subscription, contact, publicKey, privateKey)
				return nil
			}); err != nil {
				continue
			}
		} else {
			outcome = m.sender.Send(ctx, event, subscription, contact, publicKey, privateKey)
		}
		switch outcome {
		case sendAccepted:
			m.logger.Info("Push service accepted notification")
		case sendFailed:
			m.logger.Info("Notification push failed")
		case sendGone:
			m.logger.Info("Push service reports subscription gone")
			if _, err := m.store.Remove(subscription.Endpoint); err != nil {
				m.logger.Warn("Could not remove an expired notification subscription")
			}
		}
	}
}

type managerError string

func (e managerError) Error() string { return string(e) }

const errNotConfigured managerError = "notifications are not configured"
