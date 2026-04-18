package cache

import (
	"container/list"
	"context"
	"sync"
	"time"
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

	entry := elem.Value.(*memoryEntry)
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

		entry := elem.Value.(*memoryEntry)
		entry.value = val
		entry.expiresAt = time.Now().Add(ttl)

		return nil
	}

	for m.evictList.Len() >= m.maxSize {
		m.evictOldest()
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

	if elem, ok := m.items[key]; ok {
		m.removeElement(elem)
	}

	return nil
}

func (m *Memory) Close() error {
	close(m.stopCh)

	return nil
}

func (m *Memory) evictOldest() {
	elem := m.evictList.Back()
	if elem == nil {
		return
	}

	m.removeElement(elem)
}

func (m *Memory) removeElement(elem *list.Element) {
	m.evictList.Remove(elem)

	entry := elem.Value.(*memoryEntry)
	delete(m.items, entry.key)
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
		entry := elem.Value.(*memoryEntry)
		if now.After(entry.expiresAt) {
			m.removeElement(elem)
		}
	}
}
