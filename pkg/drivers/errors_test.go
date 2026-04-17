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

package drivers

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelErrors_AreDistinct(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrVolumeNotFound", ErrVolumeNotFound},
		{"ErrVolumeGroupNotFound", ErrVolumeGroupNotFound},
		{"ErrReplicationNotReady", ErrReplicationNotReady},
		{"ErrInvalidTransition", ErrInvalidTransition},
		{"ErrDriverNotFound", ErrDriverNotFound},
	}

	for _, s := range sentinels {
		t.Run(s.name, func(t *testing.T) {
			if s.err == nil {
				t.Fatalf("%s is nil", s.name)
			}
			if s.err.Error() == "" {
				t.Fatalf("%s has empty message", s.name)
			}
		})
	}

	for i := range sentinels {
		for j := i + 1; j < len(sentinels); j++ {
			if errors.Is(sentinels[i].err, sentinels[j].err) {
				t.Fatalf("%s should not match %s", sentinels[i].name, sentinels[j].name)
			}
		}
	}
}

func TestSentinelErrors_WrappedWithErrorsIs(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrVolumeNotFound", ErrVolumeNotFound},
		{"ErrVolumeGroupNotFound", ErrVolumeGroupNotFound},
		{"ErrReplicationNotReady", ErrReplicationNotReady},
		{"ErrInvalidTransition", ErrInvalidTransition},
		{"ErrDriverNotFound", ErrDriverNotFound},
	}

	for _, s := range sentinels {
		t.Run(s.name+"_wrapped", func(t *testing.T) {
			wrapped := fmt.Errorf("operation context: %w", s.err)
			if !errors.Is(wrapped, s.err) {
				t.Fatalf("errors.Is failed on wrapped %s", s.name)
			}
		})
	}
}
