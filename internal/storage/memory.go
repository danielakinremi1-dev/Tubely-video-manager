package storage

import (
	"context"
	"io"
	"slices"
	"sync"
)

// Memory is an in-memory Store for tests. Set PutErr or DeleteErr to make
// those calls fail.
type Memory struct {
	mu        sync.Mutex
	objects   map[string]Object
	PutErr    error
	DeleteErr error
}

// Object is a stored object's content and type.
type Object struct {
	ContentType string
	Data        []byte
}

var _ Store = (*Memory)(nil)

func NewMemory() *Memory {
	return &Memory{objects: map[string]Object{}}
}

// Put reads the whole body, like a real upload, then stores it. On error
// nothing is stored.
func (m *Memory) Put(ctx context.Context, key, contentType string, body io.Reader) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.PutErr != nil {
		return m.PutErr
	}
	m.objects[key] = Object{ContentType: contentType, Data: data}
	return nil
}

func (m *Memory) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.DeleteErr != nil {
		return m.DeleteErr
	}
	delete(m.objects, key)
	return nil
}

// Get returns the object at key, if there is one.
func (m *Memory) Get(key string) (Object, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	return o, ok
}

// Keys returns every stored key, sorted.
func (m *Memory) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.objects))
	for k := range m.objects {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
