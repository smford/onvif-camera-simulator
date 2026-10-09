package events

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/smford/onvif-camera-simulator/internal/metrics"
	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

// Event represents an ONVIF WS-BaseNotification message.
type Event struct {
	Topic       string
	UtcTime     time.Time
	SimpleItems map[string]string
	BBox        [4]float64 // x, y, width, height (for Human / Vehicle)
	HasBBox     bool
}

// Subscription represents an active pull-point subscription.
type Subscription struct {
	ID        string
	Camera    string
	Created   time.Time
	Expires   time.Time
	mu        sync.Mutex
	queue     []Event
	notify    chan struct{}
	cancelled bool
}

func (s *Subscription) Push(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelled {
		return
	}
	s.queue = append(s.queue, ev)
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *Subscription) Pull(maxMessages int, timeout time.Duration) []Event {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		s.mu.Lock()
		if len(s.queue) > 0 {
			count := maxMessages
			if count <= 0 || count > len(s.queue) {
				count = len(s.queue)
			}
			res := make([]Event, count)
			copy(res, s.queue[:count])
			s.queue = s.queue[count:]
			s.mu.Unlock()
			return res
		}
		if s.cancelled {
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()

		select {
		case <-timer.C:
			return nil
		case <-s.notify:
			// Loop back and pull from queue
		}
	}
}

// Manager orchestrates pull-point subscriptions and event emissions across all cameras.
type Manager struct {
	mu            sync.RWMutex
	subscriptions map[string]*Subscription
	hub           *telemetry.Hub
	stopCh        chan struct{}
}

// NewManager creates a pull-point event orchestrator.
func NewManager(hub *telemetry.Hub) *Manager {
	m := &Manager{
		subscriptions: make(map[string]*Subscription),
		hub:           hub,
		stopCh:        make(chan struct{}),
	}
	go m.reaperLoop()
	return m
}

func (m *Manager) reaperLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case now := <-ticker.C:
			m.mu.Lock()
			for id, sub := range m.subscriptions {
				if now.After(sub.Expires) {
					sub.cancelled = true
					delete(m.subscriptions, id)
				}
			}
			m.mu.Unlock()
		}
	}
}

// CreateSubscription creates a subscription with an initial TTL.
func (m *Manager) CreateSubscription(camera string, ttl time.Duration) *Subscription {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := "sub_" + uuid.New().String()[:12]
	if ttl <= 0 {
		ttl = 60 * time.Second
	}

	sub := &Subscription{
		ID:        id,
		Camera:    camera,
		Created:   time.Now().UTC(),
		Expires:   time.Now().UTC().Add(ttl),
		queue:     make([]Event, 0),
		notify:    make(chan struct{}, 100),
		cancelled: false,
	}

	m.subscriptions[id] = sub
	return sub
}

// GetSubscription retrieves a subscription by ID.
func (m *Manager) GetSubscription(id string) (*Subscription, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sub, ok := m.subscriptions[id]
	return sub, ok
}

// Renew refreshes the expiration time of an existing subscription.
func (m *Manager) Renew(id string, ttl time.Duration) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sub, ok := m.subscriptions[id]
	if !ok {
		return time.Time{}, fmt.Errorf("subscription %q not found", id)
	}

	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	sub.Expires = time.Now().UTC().Add(ttl)
	return sub.Expires, nil
}

// Unsubscribe terminates and cleans up a subscription.
func (m *Manager) Unsubscribe(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if sub, ok := m.subscriptions[id]; ok {
		sub.mu.Lock()
		sub.cancelled = true
		sub.mu.Unlock()
		delete(m.subscriptions, id)
	}
}

// Publish broadcasts an event to all subscribers matching the camera.
func (m *Manager) Publish(camera string, ev Event) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, sub := range m.subscriptions {
		if sub.Camera == camera || sub.Camera == "" {
			sub.Push(ev)
		}
	}
}

// TriggerMotion emits a cell motion event.
func (m *Manager) TriggerMotion(camera string, state bool, duration time.Duration) {
	ev := Event{
		Topic:   "tns1:RuleEngine/CellMotionDetector/Motion",
		UtcTime: time.Now().UTC(),
		SimpleItems: map[string]string{
			"IsInside": fmt.Sprintf("%t", state),
		},
	}
	m.Publish(camera, ev)
	metrics.EventsEmitted.WithLabelValues("motion").Inc()

	var expiry time.Time
	if state && duration > 0 {
		expiry = time.Now().Add(duration)
		go func() {
			time.Sleep(duration)
			m.TriggerMotion(camera, false, 0)
		}()
	}
	m.hub.SetEventActive(camera, "motion", state, expiry, [4]float64{})
}

// TriggerHuman emits an AI human detection event with a bounding box.
func (m *Manager) TriggerHuman(camera string, state bool, duration time.Duration, bbox [4]float64) {
	if bbox == [4]float64{} {
		bbox = [4]float64{0.35, 0.25, 0.20, 0.50} // standard default
	}

	ev := Event{
		Topic:   "tns1:RuleEngine/PeopleDetector/People",
		UtcTime: time.Now().UTC(),
		SimpleItems: map[string]string{
			"Class":      "Human",
			"Likelihood": "0.94",
			"State":      fmt.Sprintf("%t", state),
		},
		BBox:    bbox,
		HasBBox: true,
	}
	m.Publish(camera, ev)
	metrics.EventsEmitted.WithLabelValues("human").Inc()

	var expiry time.Time
	if state && duration > 0 {
		expiry = time.Now().Add(duration)
		go func() {
			time.Sleep(duration)
			m.TriggerHuman(camera, false, 0, bbox)
		}()
	}
	m.hub.SetEventActive(camera, "human", state, expiry, bbox)
}

// TriggerVehicle emits a vehicle detection analytics event.
func (m *Manager) TriggerVehicle(camera string, state bool, duration time.Duration, bbox [4]float64) {
	if bbox == [4]float64{} {
		bbox = [4]float64{0.10, 0.40, 0.45, 0.35}
	}

	ev := Event{
		Topic:   "tns1:RuleEngine/VehicleDetector/Vehicle",
		UtcTime: time.Now().UTC(),
		SimpleItems: map[string]string{
			"Class":      "Vehicle",
			"Likelihood": "0.88",
			"State":      fmt.Sprintf("%t", state),
		},
		BBox:    bbox,
		HasBBox: true,
	}
	m.Publish(camera, ev)
	metrics.EventsEmitted.WithLabelValues("vehicle").Inc()

	var expiry time.Time
	if state && duration > 0 {
		expiry = time.Now().Add(duration)
		go func() {
			time.Sleep(duration)
			m.TriggerVehicle(camera, false, 0, bbox)
		}()
	}
	m.hub.SetEventActive(camera, "vehicle", state, expiry, bbox)
}

func (m *Manager) Close() {
	close(m.stopCh)
}
