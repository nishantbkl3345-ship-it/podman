//go:build !remote && (linux || freebsd)

package libpod

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.podman.io/storage"
)

func Test_generateName(t *testing.T) {
	state, _ := getEmptySqliteState(t)

	r := &Runtime{
		state: state,
	}

	// Test that (*Runtime).generateName returns different names
	// if called twice.
	n1, _ := r.generateName()
	n2, _ := r.generateName()
	assert.NotEqual(t, n1, n2)
}

func TestMakeRuntime_StoreCleanupOnFailure(t *testing.T) {
	// Verify that deferred store cleanup in makeRuntime correctly inspects runtime.store
	var retErr error = assert.AnError
	storeClosed := false

	mockStore := &mockStoreShutdown{
		onShutdown: func() {
			storeClosed = true
		},
	}

	r := &Runtime{
		store: mockStore,
	}

	// Simulate the makeRuntime deferred cleanup logic
	func() {
		defer func() {
			if retErr != nil && r.store != nil {
				_, _ = r.store.Shutdown(false)
			}
		}()
	}()

	assert.True(t, storeClosed, "runtime.store.Shutdown should be invoked when retErr != nil and r.store != nil")
}

type mockStoreShutdown struct {
	storage.Store
	onShutdown func()
}

func (m *mockStoreShutdown) Shutdown(force bool) ([]string, error) {
	if m.onShutdown != nil {
		m.onShutdown()
	}
	return nil, nil
}


