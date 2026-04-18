package cache

import (
	"container/list"
	"context"
	"log/slog"
	"sync"
	"time"

	commonerrors "github.com/psyb0t/common-go/errors"
	"github.com/psyb0t/ctxerrors"
)

const defaultCleanupInterval = 1 * time.Minute

type memoryEntry struct {
	key       string
	value     []byte
	expiresAt time.Time
}

type Memory struct {
	mu        sync.RWMutex
	items     map[string]*list.Element
	evictList *list.List
	maxSize   int
	stopCh    chan struct{}
}

func toMemoryEntry(
	elem *list.Element,
) (*memoryEntry, error) {
	entry, ok := elem.Value.(*memoryEntry)
	if !ok {
		return nil, ctxerrors.Wrap(
			commonerrors.ErrInvalidValue,
			"unexpected element type in cache list",
		)
	}

	return entry, nil
}

func NewMemory(maxEntries int) *Memory {
	m := &Memory{
		items:     make(map[string]*list.Element),
		evictList: list.New(),
		maxSize:   maxEntries,
		stopCh:    make(chan struct{}),
	}

	go m.cleanup()

	return m
}

func (m *Memory) Get(
	_ context.Context,
	key string,
) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	elem, ok := m.items[key]
	if !ok {
		return nil, ErrCacheMiss
	}

	entry, err := toMemoryEntry(elem)
	if err != nil {
		return nil, err
	}

	if time.Now().After(entry.expiresAt) {
		return nil, ErrCacheMiss
	}

	m.evictList.MoveToFront(elem)

	return entry.value, nil
}

func (m *Memory) Set(
	_ context.Context,
	key string,
	val []byte,
	ttl time.Duration,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if elem, ok := m.items[key]; ok {
		m.evictList.MoveToFront(elem)

		entry, err := toMemoryEntry(elem)
		if err != nil {
			return err
		}

		entry.value = val
		entry.expiresAt = time.Now().Add(ttl)

		return nil
	}

	for m.evictList.Len() >= m.maxSize {
		if err := m.evictOldest(); err != nil {
			return err
		}
	}

	entry := &memoryEntry{
		key:       key,
		value:     val,
		expiresAt: time.Now().Add(ttl),
	}

	elem := m.evictList.PushFront(entry)
	m.items[key] = elem

	return nil
}

func (m *Memory) Delete(
	_ context.Context,
	key string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	elem, ok := m.items[key]
	if !ok {
		return nil
	}

	return m.removeElement(elem)
}

func (m *Memory) Close() error {
	close(m.stopCh)

	return nil
}

func (m *Memory) evictOldest() error {
	elem := m.evictList.Back()
	if elem == nil {
		return nil
	}

	return m.removeElement(elem)
}

func (m *Memory) removeElement(
	elem *list.Element,
) error {
	m.evictList.Remove(elem)

	entry, err := toMemoryEntry(elem)
	if err != nil {
		return err
	}

	delete(m.items, entry.key)

	return nil
}

func (m *Memory) cleanup() {
	ticker := time.NewTicker(defaultCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.removeExpired()
		}
	}
}

func (m *Memory) removeExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	for _, elem := range m.items {
		entry, err := toMemoryEntry(elem)
		if err != nil {
			slog.Error("cache cleanup: bad element",
				"error", err,
			)

			continue
		}

		if now.After(entry.expiresAt) {
			if err := m.removeElement(elem); err != nil {
				slog.Error("cache cleanup: remove failed",
					"error", err,
					"key", entry.key,
				)
			}
		}
	}
}
