package server

import "fmt"

// NewMemoryStoreWithConfig opens a private in-memory SQLite store using an
// explicit configuration. Callers that must not inherit process environment
// settings — migration sinks and tests — use this instead of NewMemoryStore.
func NewMemoryStoreWithConfig(config Config) *MemoryStore {
	store, err := NewSQLiteStoreWithConfig(fmt.Sprintf("file:%s?mode=memory&cache=shared", NewID("mem")), config)
	if err != nil {
		panic(err)
	}
	return store
}

// NewMemoryStore opens a private in-memory SQLite store configured from the
// process environment.
func NewMemoryStore() *MemoryStore {
	return NewMemoryStoreWithConfig(ConfigFromEnv())
}
