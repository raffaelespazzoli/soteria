/*
Copyright 2026 The Soteria Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

func newCheckpointTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = soteriav1alpha1.AddToScheme(s)
	return s
}

func newCheckpointTestExec(name string) *soteriav1alpha1.DRExecution {
	now := metav1.Now()
	return &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: soteriav1alpha1.DRExecutionSpec{
			PlanName: "test-plan",
			Mode:     soteriav1alpha1.ExecutionModePlannedMigration,
		},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &now,
			Waves: []soteriav1alpha1.WaveStatus{
				{
					WaveIndex: 0,
					Groups: []soteriav1alpha1.DRGroupExecutionStatus{
						{Name: "group-0", Result: soteriav1alpha1.DRGroupResultCompleted, VMNames: []string{"vm-1"}},
					},
				},
			},
		},
	}
}

func newCheckpointFakeClient(exec *soteriav1alpha1.DRExecution) client.Client {
	scheme := newCheckpointTestScheme()
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(exec).
		WithStatusSubresource(&soteriav1alpha1.DRExecution{}).
		Build()
}

func TestKubeCheckpointer_WriteSucceeds(t *testing.T) {
	exec := newCheckpointTestExec("exec-cp-ok")
	cl := newCheckpointFakeClient(exec)
	cp := &KubeCheckpointer{Client: cl}

	exec.Status.Waves[0].Groups[0].Result = soteriav1alpha1.DRGroupResultCompleted
	err := cp.WriteCheckpoint(context.Background(), exec)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	// Verify the status was persisted.
	var fetched soteriav1alpha1.DRExecution
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "exec-cp-ok"}, &fetched); err != nil {
		t.Fatalf("fetching execution: %v", err)
	}
	if fetched.Status.Waves[0].Groups[0].Result != soteriav1alpha1.DRGroupResultCompleted {
		t.Errorf("expected Completed, got %q", fetched.Status.Waves[0].Groups[0].Result)
	}
}

func TestKubeCheckpointer_WriteFailsOnce_RetriesAndSucceeds(t *testing.T) {
	exec := newCheckpointTestExec("exec-cp-retry")
	scheme := newCheckpointTestScheme()

	var attempt int32
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(exec).
		WithStatusSubresource(&soteriav1alpha1.DRExecution{}).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(
				ctx context.Context, client client.Client,
				subResourceName string, obj client.Object,
				opts ...client.SubResourceUpdateOption,
			) error {
				if atomic.AddInt32(&attempt, 1) == 1 {
					return fmt.Errorf("simulated conflict")
				}
				return client.SubResource(subResourceName).Update(ctx, obj, opts...)
			},
		}).
		Build()

	cp := &KubeCheckpointer{Client: cl}
	err := cp.WriteCheckpoint(context.Background(), exec)
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}
	if atomic.LoadInt32(&attempt) < 2 {
		t.Errorf("expected at least 2 attempts, got %d", atomic.LoadInt32(&attempt))
	}
}

func TestKubeCheckpointer_WriteExhaustsRetries_ReturnsError(t *testing.T) {
	exec := newCheckpointTestExec("exec-cp-exhaust")
	scheme := newCheckpointTestScheme()

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(exec).
		WithStatusSubresource(&soteriav1alpha1.DRExecution{}).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(
				ctx context.Context, client client.Client,
				subResourceName string, obj client.Object,
				opts ...client.SubResourceUpdateOption,
			) error {
				return fmt.Errorf("persistent API server failure")
			},
		}).
		Build()

	cp := &KubeCheckpointer{Client: cl}
	err := cp.WriteCheckpoint(context.Background(), exec)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if !errors.Is(err, ErrCheckpointFailed) {
		t.Errorf("expected ErrCheckpointFailed, got: %v", err)
	}
}

func TestKubeCheckpointer_RefetchesResourceVersion(t *testing.T) {
	exec := newCheckpointTestExec("exec-cp-rv")
	scheme := newCheckpointTestScheme()

	var getCount int32
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(exec).
		WithStatusSubresource(&soteriav1alpha1.DRExecution{}).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(
				ctx context.Context, client client.WithWatch,
				key client.ObjectKey, obj client.Object,
				opts ...client.GetOption,
			) error {
				atomic.AddInt32(&getCount, 1)
				return client.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	cp := &KubeCheckpointer{Client: cl}
	err := cp.WriteCheckpoint(context.Background(), exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// At least one GET (re-fetch before status update).
	if atomic.LoadInt32(&getCount) < 1 {
		t.Errorf("expected at least 1 GET call for re-fetch, got %d", atomic.LoadInt32(&getCount))
	}
}

func TestKubeCheckpointer_ConcurrentCheckpoints_Independent(t *testing.T) {
	exec1 := newCheckpointTestExec("exec-cp-c1")
	exec2 := newCheckpointTestExec("exec-cp-c2")
	scheme := newCheckpointTestScheme()

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(exec1, exec2).
		WithStatusSubresource(&soteriav1alpha1.DRExecution{}).
		Build()

	cp := &KubeCheckpointer{Client: cl}

	var wg sync.WaitGroup
	var err1, err2 error
	wg.Add(2)
	go func() {
		defer wg.Done()
		err1 = cp.WriteCheckpoint(context.Background(), exec1)
	}()
	go func() {
		defer wg.Done()
		err2 = cp.WriteCheckpoint(context.Background(), exec2)
	}()
	wg.Wait()

	if err1 != nil {
		t.Errorf("exec1 checkpoint failed: %v", err1)
	}
	if err2 != nil {
		t.Errorf("exec2 checkpoint failed: %v", err2)
	}
}

func TestNoOpCheckpointer_RecordsCalls(t *testing.T) {
	cp := &NoOpCheckpointer{}

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-noop"},
		Status: soteriav1alpha1.DRExecutionStatus{
			StartTime: &metav1.Time{Time: time.Now()},
		},
	}

	if err := cp.WriteCheckpoint(context.Background(), exec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := cp.WriteCheckpoint(context.Background(), exec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := cp.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0] != "exec-noop" || calls[1] != "exec-noop" {
		t.Errorf("unexpected call names: %v", calls)
	}
}

func TestNoOpCheckpointer_ConfigurableFailure(t *testing.T) {
	cp := &NoOpCheckpointer{FailNext: true}

	exec := &soteriav1alpha1.DRExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "exec-fail"},
	}

	err := cp.WriteCheckpoint(context.Background(), exec)
	if !errors.Is(err, ErrCheckpointFailed) {
		t.Errorf("expected ErrCheckpointFailed, got: %v", err)
	}

	// Second call should succeed (FailNext is reset).
	if err := cp.WriteCheckpoint(context.Background(), exec); err != nil {
		t.Errorf("expected success on second call, got: %v", err)
	}
}
