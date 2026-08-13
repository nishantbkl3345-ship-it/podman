//go:build !remote && (linux || freebsd)

package libpod

import (
	"context"
	"testing"
	"time"

	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	manifest "go.podman.io/image/v5/manifest"
	"go.podman.io/podman/v6/libpod/define"
	"go.podman.io/podman/v6/libpod/lock"
)

// batchedHealthCheckCtr returns a container in the state Batch() hands to its
// callback: batched, with its lock already held and its state already synced.
//
// The lock is deliberately never released. If runHealthCheck() takes it again
// the goroutine calling it has to stay blocked for the test to observe the
// deadlock, and releasing the lock afterwards would let that goroutine run the
// rest of the function against a nil runtime.
func batchedHealthCheckCtr(t *testing.T, startedTime time.Time, startPeriod time.Duration, test []string) *Container {
	t.Helper()

	manager, err := lock.NewInMemoryManager(1)
	require.NoError(t, err)
	ctrLock, err := manager.AllocateLock()
	require.NoError(t, err)

	ctr := &Container{
		config: &ContainerConfig{
			ID:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Spec: &spec.Spec{},
			ContainerMiscConfig: ContainerMiscConfig{
				HealthCheckConfig: &manifest.Schema2HealthConfig{
					Test:        test,
					StartPeriod: startPeriod,
				},
			},
		},
		state:   &ContainerState{StartedTime: startedTime},
		lock:    ctrLock,
		batched: true,
		valid:   true,
	}
	ctr.lock.Lock()

	return ctr
}

// runHealthCheckAsync calls runHealthCheck() on ctr and waits for it to return.
// It fails the test rather than hanging forever if the call blocks.
func runHealthCheckAsync(t *testing.T, ctr *Container) (define.HealthCheckStatus, error) {
	t.Helper()

	type result struct {
		status define.HealthCheckStatus
		err    error
	}
	done := make(chan result, 1)
	go func() {
		status, _, err := ctr.runHealthCheck(context.Background(), false)
		done <- result{status: status, err: err}
	}()

	select {
	case res := <-done:
		return res.status, res.err
	case <-time.After(30 * time.Second):
		t.Fatal("runHealthCheck() blocked on the container lock that Batch() already holds")
		return define.HealthCheckInternalError, nil
	}
}

// TestRunHealthCheckBatchedStartPeriodDoesNotDeadlock verifies that
// runHealthCheck() does not lock a container that is already locked by
// Batch(). The rest of the function guards its locking with !c.batched, but
// the start-period branch locked unconditionally, so a batched container with
// a start period deadlocked against the lock its caller holds.
func TestRunHealthCheckBatchedStartPeriodDoesNotDeadlock(t *testing.T) {
	// No healthcheck command, so the start-period branch is all that runs
	// before runHealthCheck() bails out.
	ctr := batchedHealthCheckCtr(t, time.Now(), time.Hour, nil)

	status, err := runHealthCheckAsync(t, ctr)
	assert.Equal(t, define.HealthCheckNotDefined, status)
	assert.ErrorContains(t, err, "has no defined healthcheck")
}

// TestRunHealthCheckBatchedStartPeriod verifies that a batched container still
// honors its start period, i.e. that skipping the lock does not skip reading
// StartedTime. A kube container inside its start period reports
// HealthCheckDefined without running the check; outside of it the check runs
// as usual.
func TestRunHealthCheckBatchedStartPeriod(t *testing.T) {
	for _, tt := range []struct {
		name           string
		startedTime    time.Time
		expectedStatus define.HealthCheckStatus
		expectedErr    string
	}{
		{
			name:           "inside start period",
			startedTime:    time.Now().Add(-30 * time.Minute),
			expectedStatus: define.HealthCheckDefined,
		},
		{
			name:           "outside start period",
			startedTime:    time.Now().Add(-2 * time.Hour),
			expectedStatus: define.HealthCheckNotDefined,
			expectedErr:    "has no defined healthcheck",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctr := batchedHealthCheckCtr(t, tt.startedTime, time.Hour, nil)
			ctr.config.Spec.Annotations = map[string]string{
				define.KubeHealthCheckAnnotation: "true",
			}

			status, err := runHealthCheckAsync(t, ctr)
			assert.Equal(t, tt.expectedStatus, status)
			if tt.expectedErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.expectedErr)
			}
		})
	}
}
