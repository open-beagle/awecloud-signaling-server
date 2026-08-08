package service

import (
	"sync"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type workloadSnapshotKey struct {
	sourceID string
	scopeID  string
	kind     model.WorkloadObservationKind
}

type workloadSnapshot struct {
	SourceTechnicalResourceID string
	NamespaceScopeID          string
	NamespaceUID              string
	NamespaceName             string
	Kind                      model.WorkloadObservationKind
	SourceEpoch               string
	Sequence                  int64
	SnapshotID                string
	ObservedAt                time.Time
	ReceivedAt                time.Time
	LeaseExpiresAt            time.Time
	Projections               []workloadProjection
}

type workloadSourceSequence struct {
	epoch       string
	sequence    int64
	payloadHash string
}

// WorkloadSnapshotStore owns runtime Kubernetes discovery. Its contents are
// deliberately process-local and are rebuilt by Agents after a Server restart.
type WorkloadSnapshotStore struct {
	mu        sync.RWMutex
	snapshots map[workloadSnapshotKey]workloadSnapshot
	sequences map[string]workloadSourceSequence
	epochs    map[string]map[string]struct{}
}

func NewWorkloadSnapshotStore() *WorkloadSnapshotStore {
	return &WorkloadSnapshotStore{
		snapshots: make(map[workloadSnapshotKey]workloadSnapshot),
		sequences: make(map[string]workloadSourceSequence),
		epochs:    make(map[string]map[string]struct{}),
	}
}

func (s *WorkloadSnapshotStore) replace(snapshot workloadSnapshot, payloadHash string) (bool, error) {
	if s == nil {
		return false, ErrWorkloadInventoryInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, exists := s.sequences[snapshot.SourceTechnicalResourceID]
	if exists && previous.epoch == snapshot.SourceEpoch {
		if snapshot.Sequence < previous.sequence {
			return false, ErrWorkloadSequenceGap
		}
		if snapshot.Sequence == previous.sequence {
			if previous.payloadHash != payloadHash {
				return false, ErrWorkloadSequenceConflict
			}
			return true, nil
		}
		if snapshot.Sequence != previous.sequence+1 {
			return false, ErrWorkloadSequenceGap
		}
	} else if exists {
		if _, stale := s.epochs[snapshot.SourceTechnicalResourceID][snapshot.SourceEpoch]; stale {
			return false, ErrWorkloadSourceEpochStale
		}
	}
	key := workloadSnapshotKey{sourceID: snapshot.SourceTechnicalResourceID, scopeID: snapshot.NamespaceScopeID, kind: snapshot.Kind}
	snapshot.Projections = append([]workloadProjection(nil), snapshot.Projections...)
	s.snapshots[key] = snapshot
	s.sequences[snapshot.SourceTechnicalResourceID] = workloadSourceSequence{
		epoch: snapshot.SourceEpoch, sequence: snapshot.Sequence, payloadHash: payloadHash,
	}
	if s.epochs[snapshot.SourceTechnicalResourceID] == nil {
		s.epochs[snapshot.SourceTechnicalResourceID] = make(map[string]struct{})
	}
	s.epochs[snapshot.SourceTechnicalResourceID][snapshot.SourceEpoch] = struct{}{}
	return false, nil
}

func (s *WorkloadSnapshotStore) current(at time.Time) []workloadSnapshot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]workloadSnapshot, 0, len(s.snapshots))
	for _, snapshot := range s.snapshots {
		if !snapshot.LeaseExpiresAt.After(at) {
			continue
		}
		snapshot.Projections = append([]workloadProjection(nil), snapshot.Projections...)
		result = append(result, snapshot)
	}
	return result
}
