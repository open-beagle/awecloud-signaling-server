package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

const (
	WorkloadInventoryResultAccepted     = "WORKLOAD_ACCEPTED"
	WorkloadInventoryResultReplayed     = "WORKLOAD_REPLAYED"
	workloadInventorySchemaVersion      = 1
	workloadInventoryLeaseDuration      = 10 * time.Minute
	maxWorkloadInventoryPayloadBytes    = 1024 * 1024
	maxWorkloadInventoryObjectsPerBatch = 4096
	maxWorkloadLabelsPerObject          = 64
	workloadExposeLabel                 = "signal.beagle.io/expose"
)

var (
	ErrWorkloadInventoryInvalidInput     = errors.New("WORKLOAD_INVALID_INVENTORY")
	ErrWorkloadPayloadHashMismatch       = errors.New("WORKLOAD_PAYLOAD_HASH_MISMATCH")
	ErrWorkloadSequenceGap               = errors.New("WORKLOAD_SEQUENCE_GAP")
	ErrWorkloadSequenceConflict          = errors.New("WORKLOAD_SEQUENCE_CONFLICT")
	ErrWorkloadSourceEpochStale          = errors.New("WORKLOAD_SOURCE_EPOCH_STALE")
	ErrWorkloadScopeNotTrusted           = errors.New("WORKLOAD_SCOPE_NOT_TRUSTED")
	ErrWorkloadProtocolUnsupported       = errors.New("WORKLOAD_PROTOCOL_UNSUPPORTED")
	ErrWorkloadIdentityInsufficient      = errors.New("WORKLOAD_IDENTITY_INSUFFICIENT")
	ErrWorkloadPayloadForbidden          = errors.New("WORKLOAD_PAYLOAD_FORBIDDEN")
	ErrWorkloadTechnicalCapabilityDenied = errors.New("WORKLOAD_CAPABILITY_NOT_ALLOWED")
)

type WorkloadInventoryService struct {
	db        *gorm.DB
	now       func() time.Time
	snapshots *WorkloadSnapshotStore
}

func NewWorkloadInventoryService(database *gorm.DB, snapshots *WorkloadSnapshotStore) *WorkloadInventoryService {
	return &WorkloadInventoryService{db: database, now: time.Now, snapshots: snapshots}
}

type ReceiveWorkloadInventoryBatchInput struct {
	AuthenticatedSource       TechnicalResourceCredential
	SourceTechnicalResourceID string
	SourceCredentialRevision  int64
	SchemaVersion             int
	SourceEpoch               string
	Sequence                  int64
	SnapshotID                string
	BatchIndex                int
	BatchCount                int
	ClusterIdentityDigest     string
	NamespaceUID              string
	NamespaceName             string
	Kind                      model.WorkloadObservationKind
	ObservedAt                time.Time
	PayloadHash               string
	Payload                   []byte
}

type WorkloadInventoryAck struct {
	TechnicalResourceID string
	AcceptedSequence    int64
	SnapshotID          string
	BatchIndex          int
	ResultCode          string
	Replayed            bool
	Committed           bool
	Retryable           bool
	ServerReceivedAt    time.Time
}

type workloadInventoryDocument struct {
	ServicePorts []workloadServicePortEvidence `json:"service_ports,omitempty"`
	Containers   []workloadContainerEvidence   `json:"containers,omitempty"`
}

type workloadServicePortEvidence struct {
	ServiceUID      string            `json:"service_uid"`
	ServiceName     string            `json:"service_name"`
	ClusterIP       string            `json:"cluster_ip"`
	PortName        string            `json:"port_name"`
	PortNumber      int               `json:"port_number"`
	Protocol        string            `json:"protocol"`
	Ready           bool              `json:"ready"`
	LabelsAllowlist map[string]string `json:"labels_allowlist"`
}

type workloadContainerEvidence struct {
	WorkloadUID     string            `json:"workload_uid"`
	WorkloadKind    string            `json:"workload_kind"`
	WorkloadName    string            `json:"workload_name"`
	PodUID          string            `json:"pod_uid"`
	PodName         string            `json:"pod_name"`
	ContainerName   string            `json:"container_name"`
	Ready           bool              `json:"ready"`
	LabelsAllowlist map[string]string `json:"labels_allowlist"`
	SSHUsers        []string          `json:"ssh_users"`
}

type workloadProjection struct {
	StableKey       string
	IdentityQuality model.WorkloadIdentityQuality
	Ready           bool
	Labels          string
	Target          string
	PayloadHash     string
	DisplayName     string
}

func (s *WorkloadInventoryService) ReceiveBatch(ctx context.Context, input ReceiveWorkloadInventoryBatchInput) (*WorkloadInventoryAck, error) {
	if s == nil || s.db == nil || s.snapshots == nil {
		return nil, ErrWorkloadInventoryInvalidInput
	}
	if err := normalizeWorkloadInventoryInput(&input); err != nil {
		return nil, err
	}
	canonicalPayload, err := canonicalizeWorkloadInventoryPayload(input.Kind, input.Payload)
	if err != nil {
		return nil, err
	}
	if sha256Hex(canonicalPayload) != input.PayloadHash {
		return nil, ErrWorkloadPayloadHashMismatch
	}

	now := s.now().UTC()
	if input.BatchCount != 1 || input.BatchIndex != 0 {
		return nil, ErrWorkloadProtocolUnsupported
	}
	var snapshot workloadSnapshot
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		source, err := resolveWorkloadInventorySource(tx, input)
		if err != nil {
			return err
		}
		scope, err := resolveTrustedWorkloadNamespaceScope(tx, source, input.ClusterIdentityDigest, input.NamespaceUID, now)
		if err != nil {
			return err
		}

		if scope.NamespaceObservationID == nil {
			return ErrWorkloadScopeNotTrusted
		}
		var namespace model.NamespaceObservation
		if err := tx.Where("id = ? AND provider_id = ? AND cluster_resource_id = ?", *scope.NamespaceObservationID, scope.ProviderID, scope.PlatformResourceID).First(&namespace).Error; err != nil {
			return ErrWorkloadScopeNotTrusted
		}
		projections, err := decodeWorkloadDocument(input.Kind, string(canonicalPayload))
		if err != nil {
			return err
		}
		for i := range projections {
			projections[i].Target, err = workloadTargetWithTrustedNamespace(projections[i].Target, &namespace)
			if err != nil {
				return err
			}
		}
		snapshot = workloadSnapshot{
			SourceTechnicalResourceID: source.ID, NamespaceScopeID: scope.ID, NamespaceUID: namespace.NamespaceUID,
			NamespaceName: namespace.Name, Kind: input.Kind, SourceEpoch: input.SourceEpoch, Sequence: input.Sequence,
			SnapshotID: input.SnapshotID, ObservedAt: input.ObservedAt, ReceivedAt: now,
			LeaseExpiresAt: now.Add(workloadInventoryLeaseDuration), Projections: projections,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	replayed, err := s.snapshots.replace(snapshot, input.PayloadHash)
	if err != nil {
		return nil, err
	}
	if err := s.refreshPublishedResources(ctx, snapshot); err != nil {
		return nil, err
	}
	resultCode := WorkloadInventoryResultAccepted
	if replayed {
		resultCode = WorkloadInventoryResultReplayed
	}
	return &WorkloadInventoryAck{
		TechnicalResourceID: snapshot.SourceTechnicalResourceID, AcceptedSequence: input.Sequence,
		SnapshotID: input.SnapshotID, BatchIndex: input.BatchIndex, ResultCode: resultCode,
		Replayed: replayed, Committed: true, Retryable: false, ServerReceivedAt: now,
	}, nil
}

// refreshPublishedResources keeps reviewed resources attached to the current
// Agent snapshot. New objects remain memory-only candidates until reviewed.
func (s *WorkloadInventoryService) refreshPublishedResources(ctx context.Context, snapshot workloadSnapshot) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range snapshot.Projections {
			projection := &snapshot.Projections[i]
			var observation model.WorkloadObservation
			if err := tx.Where("namespace_scope_id = ? AND kind = ? AND stable_key = ?",
				snapshot.NamespaceScopeID, snapshot.Kind, projection.StableKey).First(&observation).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return err
			}

			var evidence model.WorkloadObservationSource
			if err := tx.Where("workload_observation_id = ? AND source_technical_resource_id = ?",
				observation.ID, snapshot.SourceTechnicalResourceID).First(&evidence).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return err
			}
			var sources []model.TenantResourceSource
			if err := tx.Where("workload_observation_id = ?", observation.ID).Find(&sources).Error; err != nil {
				return err
			}
			sourceChanged := false
			for j := range sources {
				if !sources[j].Enabled {
					sourceChanged = true
					break
				}
			}
			if evidence.SourceEpoch == snapshot.SourceEpoch && evidence.Sequence == snapshot.Sequence &&
				evidence.State == model.WorkloadObservationSourceObserved && observation.State == model.WorkloadObservationEligible && !sourceChanged {
				continue
			}

			changed := observation.IdentityQuality != projection.IdentityQuality || observation.Ready != projection.Ready ||
				observation.LabelSnapshot != projection.Labels || observation.State != model.WorkloadObservationEligible ||
				evidence.PayloadHash != projection.PayloadHash || evidence.Ready != projection.Ready ||
				evidence.TargetSnapshot != projection.Target || evidence.State != model.WorkloadObservationSourceObserved || sourceChanged
			observationRevision := observation.ObservedRevision
			evidenceRevision := evidence.SourceRevision
			if changed {
				observationRevision++
				evidenceRevision++
			}

			if err := tx.Model(&model.WorkloadObservation{}).Where("id = ? AND row_version = ?", observation.ID, observation.RowVersion).
				Updates(map[string]any{
					"identity_quality": projection.IdentityQuality, "state": model.WorkloadObservationEligible,
					"ready": projection.Ready, "observed_revision": observationRevision, "label_snapshot": projection.Labels,
					"last_observed_at": snapshot.ObservedAt, "lease_expires_at": snapshot.LeaseExpiresAt,
					"row_version": gorm.Expr("row_version + 1"),
				}).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.WorkloadObservationSource{}).Where("id = ? AND row_version = ?", evidence.ID, evidence.RowVersion).
				Updates(map[string]any{
					"source_epoch": snapshot.SourceEpoch, "sequence": snapshot.Sequence, "payload_hash": projection.PayloadHash,
					"state": model.WorkloadObservationSourceObserved, "ready": projection.Ready, "target_snapshot": projection.Target,
					"observed_at": snapshot.ObservedAt, "received_at": snapshot.ReceivedAt, "lease_expires_at": snapshot.LeaseExpiresAt,
					"source_revision": evidenceRevision, "row_version": gorm.Expr("row_version + 1"),
				}).Error; err != nil {
				return err
			}
			if !changed {
				continue
			}

			availability := model.TenantResourceUnavailable
			if projection.Ready {
				availability = model.TenantResourceAvailable
			}
			for j := range sources {
				sourceRevision := sources[j].SourceRevision + 1
				if err := tx.Model(&model.TenantResourceSource{}).Where("id = ? AND row_version = ?", sources[j].ID, sources[j].RowVersion).
					Updates(map[string]any{
						"enabled": true, "enabled_at": snapshot.ReceivedAt, "disabled_at": nil, "disabled_reason": "",
						"source_revision": sourceRevision, "row_version": gorm.Expr("row_version + 1"),
					}).Error; err != nil {
					return err
				}
				var current model.TenantResourceTargetRevision
				if err := tx.Where("tenant_resource_source_id = ? AND superseded_at IS NULL", sources[j].ID).
					Order("revision DESC").First(&current).Error; err != nil {
					return err
				}
				if err := tx.Model(&model.TenantResourceTargetRevision{}).Where("id = ? AND superseded_at IS NULL", current.ID).
					Update("superseded_at", snapshot.ReceivedAt).Error; err != nil {
					return err
				}
				next := model.TenantResourceTargetRevision{
					ID: uuid.NewString(), TenantResourceSourceID: sources[j].ID, Revision: current.Revision + 1,
					TargetType: snapshot.Kind, TargetSnapshot: projection.Target,
					SourceTechnicalResourceID: snapshot.SourceTechnicalResourceID,
					AccessTechnicalResourceID: snapshot.SourceTechnicalResourceID,
					Ready:                     projection.Ready, ObservedAt: snapshot.ObservedAt,
					ObservationRevision: observationRevision, SourceRevision: sourceRevision, CreatedAt: snapshot.ReceivedAt,
				}
				if err := tx.Create(&next).Error; err != nil {
					return err
				}
				if err := tx.Model(&model.TenantResource{}).Where("id = ?", sources[j].TenantResourceID).
					Updates(map[string]any{
						"availability_state": availability, "revision": gorm.Expr("revision + 1"),
						"row_version": gorm.Expr("row_version + 1"),
					}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func normalizeWorkloadInventoryInput(input *ReceiveWorkloadInventoryBatchInput) error {
	if input == nil {
		return ErrWorkloadInventoryInvalidInput
	}
	input.SourceTechnicalResourceID = strings.TrimSpace(input.SourceTechnicalResourceID)
	input.SourceEpoch = strings.TrimSpace(input.SourceEpoch)
	input.SnapshotID = strings.TrimSpace(input.SnapshotID)
	input.ClusterIdentityDigest = strings.ToLower(strings.TrimSpace(input.ClusterIdentityDigest))
	input.NamespaceUID = strings.TrimSpace(input.NamespaceUID)
	input.NamespaceName = strings.TrimSpace(input.NamespaceName)
	input.PayloadHash = strings.ToLower(strings.TrimSpace(input.PayloadHash))
	input.ObservedAt = input.ObservedAt.UTC()
	if input.SchemaVersion != workloadInventorySchemaVersion || input.Sequence <= 0 || input.BatchCount != 1 || input.BatchIndex != 0 ||
		!input.Kind.Valid() || input.ObservedAt.IsZero() ||
		validateRequired("source_epoch", input.SourceEpoch, 36) != nil || validateRequired("snapshot_id", input.SnapshotID, 36) != nil ||
		validateRequired("namespace_uid", input.NamespaceUID, 128) != nil || len(input.NamespaceName) > 253 ||
		validateOptionalSHA256("cluster_identity_digest", input.ClusterIdentityDigest) != nil || input.ClusterIdentityDigest == "" ||
		validateOptionalSHA256("payload_hash", input.PayloadHash) != nil || input.PayloadHash == "" {
		return ErrWorkloadInventoryInvalidInput
	}
	return nil
}

func resolveWorkloadInventorySource(tx *gorm.DB, input ReceiveWorkloadInventoryBatchInput) (*model.TechnicalResource, error) {
	direct, err := resolveAuthenticatedTechnicalResource(tx, input.AuthenticatedSource)
	if err != nil {
		return nil, err
	}
	if err := requireReportingLifecycle(direct); err != nil {
		return nil, err
	}
	if input.SourceTechnicalResourceID == "" {
		if direct.Type != model.TechnicalResourceAgent || input.SourceCredentialRevision != direct.CredentialRevision {
			return nil, ErrCredentialRevisionStale
		}
		return direct, nil
	}
	if direct.Type != model.TechnicalResourceAgent || input.SourceCredentialRevision <= 0 {
		return nil, ErrTechnicalResourceUnbound
	}
	var source model.TechnicalResource
	if err := tx.Where("id = ? AND provider_id = ? AND parent_id = ? AND type = ?", input.SourceTechnicalResourceID, direct.ProviderID, direct.ID, model.TechnicalResourceEndpoint).
		First(&source).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTechnicalResourceUnbound
		}
		return nil, err
	}
	binding, err := loadActiveTechnicalResourceBinding(tx, source.ID)
	if err != nil {
		return nil, err
	}
	if source.CredentialRevision != input.SourceCredentialRevision || binding.CredentialRevision != input.SourceCredentialRevision {
		return nil, ErrCredentialRevisionStale
	}
	if err := requireReportingLifecycle(&source); err != nil {
		return nil, err
	}
	return &source, nil
}

func resolveTrustedWorkloadNamespaceScope(tx *gorm.DB, source *model.TechnicalResource, clusterDigest, namespaceUID string, now time.Time) (*model.ResourceScope, error) {
	if tx == nil || source == nil {
		return nil, ErrWorkloadScopeNotTrusted
	}
	var scopes []model.ResourceScope
	err := tx.Table("resource_scope AS scope").Select("scope.*").
		Joins("JOIN platform_resource AS resource ON resource.id = scope.platform_resource_id AND resource.provider_id = scope.provider_id").
		Joins("JOIN namespace_observation AS namespace ON namespace.id = scope.namespace_observation_id AND namespace.cluster_resource_id = resource.id AND namespace.provider_id = resource.provider_id").
		Where("scope.type = ? AND scope.provider_id = ? AND resource.type = ? AND resource.stable_key = ?", model.ResourceScopeNamespace, source.ProviderID, model.SupplyResourceKubernetes, clusterDigest).
		Where("namespace.namespace_uid = ? AND namespace.state = ? AND julianday(namespace.lease_expires_at) > julianday(?)", namespaceUID, model.NamespaceObservationObserved, now).
		Where("resource.lifecycle_state = ? AND scope.lifecycle_state <> ?", model.PlatformResourceActive, model.ResourceScopeRetired).
		Where(`EXISTS (
			SELECT 1 FROM platform_resource_source resource_source
			JOIN supply_candidate candidate ON candidate.id = resource_source.supply_candidate_id
			WHERE resource_source.platform_resource_id = resource.id
				AND candidate.provider_id = ?
				AND candidate.review_state = ? AND candidate.identity_quality = ? AND candidate.conflict_code = ''
				AND julianday(candidate.lease_expires_at) > julianday(?)
				AND (candidate.technical_resource_id = ? OR candidate.technical_resource_id = ?
					OR EXISTS (SELECT 1 FROM technical_resource candidate_source
						WHERE candidate_source.id = candidate.technical_resource_id AND candidate_source.parent_id = ?))
		)`, source.ProviderID, model.SupplyCandidateLinked, model.SupplyIdentityStrong, now, source.ID, source.ParentID, source.ID).
		Find(&scopes).Error
	if err != nil {
		return nil, err
	}
	if len(scopes) != 1 {
		return nil, ErrWorkloadScopeNotTrusted
	}
	allowed, err := workloadSourceHasCapability(tx, source, scopes[0].PlatformResourceID, now)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrWorkloadTechnicalCapabilityDenied
	}
	return &scopes[0], nil
}

func workloadSourceHasCapability(tx *gorm.DB, source *model.TechnicalResource, platformResourceID string, now time.Time) (bool, error) {
	if tx == nil || source == nil {
		return false, nil
	}
	if source.Type == model.TechnicalResourceEndpoint {
		binding, err := loadActiveTechnicalResourceBinding(tx, source.ID)
		if err != nil {
			return false, err
		}
		if binding.SourceType != model.TechnicalResourceBindingLegacyEndpoint {
			return false, nil
		}
		var endpoint model.Endpoint
		if err := tx.Where("id = ? AND revoked = ?", binding.SourceID, false).First(&endpoint).Error; err != nil {
			return false, nil
		}
		if !endpoint.K8SAPIEnabled && !endpoint.K8SServiceEnabled {
			return false, nil
		}
	}
	query := tx.Table("supply_candidate AS candidate").Select("candidate.*").
		Joins("JOIN platform_resource_source AS resource_source ON resource_source.supply_candidate_id = candidate.id").
		Where("candidate.provider_id = ? AND candidate.review_state = ? AND candidate.identity_quality = ? AND candidate.conflict_code = ''", source.ProviderID, model.SupplyCandidateLinked, model.SupplyIdentityStrong).
		Where("candidate.lease_expires_at > ?", now).
		Where("candidate.technical_resource_id = ? OR candidate.technical_resource_id = ? OR EXISTS (SELECT 1 FROM technical_resource candidate_source WHERE candidate_source.id = candidate.technical_resource_id AND candidate_source.parent_id = ?)", source.ID, source.ParentID, source.ID)
	if platformResourceID != "" {
		query = query.Where("resource_source.platform_resource_id = ?", platformResourceID)
	}
	var candidates []model.SupplyCandidate
	if err := query.Find(&candidates).Error; err != nil {
		return false, err
	}
	for i := range candidates {
		var evidence supplyClusterEvidence
		if json.Unmarshal([]byte(candidates[i].ObservationSnapshot), &evidence) != nil {
			continue
		}
		for _, capability := range evidence.Capabilities {
			if strings.TrimSpace(capability) == "workload_inventory_v1" {
				return true, nil
			}
		}
	}
	return false, nil
}

func workloadTargetWithTrustedNamespace(targetJSON string, namespace *model.NamespaceObservation) (string, error) {
	if namespace == nil || strings.TrimSpace(namespace.NamespaceUID) == "" || strings.TrimSpace(namespace.Name) == "" {
		return "", ErrWorkloadScopeNotTrusted
	}
	var target map[string]any
	if err := json.Unmarshal([]byte(targetJSON), &target); err != nil {
		return "", ErrWorkloadInventoryInvalidInput
	}
	target["namespace_uid"] = namespace.NamespaceUID
	target["namespace_name"] = namespace.Name
	canonical, err := json.Marshal(target)
	if err != nil {
		return "", ErrWorkloadInventoryInvalidInput
	}
	return string(canonical), nil
}

func workloadExposureAllowed(labelsJSON string) bool {
	var labels map[string]string
	if json.Unmarshal([]byte(labelsJSON), &labels) != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(labels[workloadExposeLabel]), "true")
}

func workloadTechnicalResourceBindingCurrent(tx *gorm.DB, technical *model.TechnicalResource) (bool, error) {
	if technical == nil || technical.LifecycleState != model.TechnicalResourceRegistered ||
		(technical.Type != model.TechnicalResourceAgent && technical.Type != model.TechnicalResourceEndpoint) {
		return false, nil
	}
	binding, err := loadActiveTechnicalResourceBinding(tx, technical.ID)
	if errors.Is(err, ErrTechnicalResourceUnbound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return binding.CredentialRevision == technical.CredentialRevision, nil
}

func (s *WorkloadInventoryService) ReportingSource(ctx context.Context, sourceType model.TechnicalResourceBindingSourceType, sourceID string) (*model.TechnicalResource, error) {
	if s == nil || s.db == nil {
		return nil, ErrWorkloadInventoryInvalidInput
	}
	var resource model.TechnicalResource
	err := s.db.WithContext(ctx).Table("technical_resource AS resource").Select("resource.*").
		Joins("JOIN technical_resource_binding AS binding ON binding.technical_resource_id = resource.id").
		Where("binding.source_type = ? AND binding.source_id = ? AND binding.enabled = ? AND binding.credential_revision = resource.credential_revision", sourceType, strings.TrimSpace(sourceID), true).
		Where("resource.lifecycle_state = ?", model.TechnicalResourceRegistered).First(&resource).Error
	if err != nil {
		return nil, ErrTechnicalResourceUnbound
	}
	allowed, err := workloadSourceHasCapability(s.db.WithContext(ctx), &resource, "", s.now().UTC())
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrWorkloadTechnicalCapabilityDenied
	}
	return &resource, nil
}

func canonicalizeWorkloadInventoryPayload(kind model.WorkloadObservationKind, payload []byte) ([]byte, error) {
	if len(payload) == 0 || len(payload) > maxWorkloadInventoryPayloadBytes {
		return nil, ErrWorkloadInventoryInvalidInput
	}
	value, err := decodeJSONObject(payload)
	if err != nil {
		return nil, ErrWorkloadInventoryInvalidInput
	}
	if field, found := findSensitiveJSONField(value); found {
		return nil, fmt.Errorf("%w: %s", ErrWorkloadPayloadForbidden, field)
	}
	if field, found := findWorkloadAuthorityField(value); found {
		return nil, fmt.Errorf("%w: %s", ErrWorkloadPayloadForbidden, field)
	}
	want := "service_ports"
	if kind == model.WorkloadObservationContainer {
		want = "containers"
	}
	if len(value) != 1 || value[want] == nil {
		return nil, ErrWorkloadInventoryInvalidInput
	}
	canonical, err := json.Marshal(value)
	if err != nil || len(canonical) > maxWorkloadInventoryPayloadBytes {
		return nil, ErrWorkloadInventoryInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	var document workloadInventoryDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, ErrWorkloadInventoryInvalidInput
	}
	count := len(document.ServicePorts) + len(document.Containers)
	if count > maxWorkloadInventoryObjectsPerBatch || (kind == model.WorkloadObservationServicePort && len(document.Containers) != 0) ||
		(kind == model.WorkloadObservationContainer && len(document.ServicePorts) != 0) {
		return nil, ErrWorkloadInventoryInvalidInput
	}
	if _, err := normalizeWorkloadDocument(kind, document); err != nil {
		return nil, err
	}
	return canonical, nil
}

func findWorkloadAuthorityField(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			normalized := strings.NewReplacer("-", "", "_", "", ".", "").Replace(strings.ToLower(key))
			switch normalized {
			case "providerid", "tenantid", "allocationid", "resourceid", "grantid", "sessionid", "userid", "deviceid", "namespacescopeid", "technicalresourceid":
				return key, true
			}
			if field, found := findWorkloadAuthorityField(nested); found {
				return key + "." + field, true
			}
		}
	case []any:
		for _, nested := range typed {
			if field, found := findWorkloadAuthorityField(nested); found {
				return field, true
			}
		}
	}
	return "", false
}

func decodeWorkloadDocument(kind model.WorkloadObservationKind, payload string) ([]workloadProjection, error) {
	var document workloadInventoryDocument
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, ErrWorkloadInventoryInvalidInput
	}
	return normalizeWorkloadDocument(kind, document)
}

func normalizeWorkloadDocument(kind model.WorkloadObservationKind, document workloadInventoryDocument) ([]workloadProjection, error) {
	projections := make([]workloadProjection, 0, len(document.ServicePorts)+len(document.Containers))
	switch kind {
	case model.WorkloadObservationServicePort:
		for i := range document.ServicePorts {
			projection, err := normalizeWorkloadServicePort(document.ServicePorts[i])
			if err != nil {
				return nil, err
			}
			projections = append(projections, projection)
		}
	case model.WorkloadObservationContainer:
		for i := range document.Containers {
			projection, err := normalizeWorkloadContainer(document.Containers[i])
			if err != nil {
				return nil, err
			}
			projections = append(projections, projection)
		}
	default:
		return nil, ErrWorkloadInventoryInvalidInput
	}
	return projections, nil
}

func normalizeWorkloadServicePort(item workloadServicePortEvidence) (workloadProjection, error) {
	item.ServiceUID, item.ServiceName = strings.TrimSpace(item.ServiceUID), strings.TrimSpace(item.ServiceName)
	item.PortName, item.Protocol = strings.ToLower(strings.TrimSpace(item.PortName)), strings.ToUpper(strings.TrimSpace(item.Protocol))
	if validateRequired("service_uid", item.ServiceUID, 128) != nil || validateRequired("service_name", item.ServiceName, 253) != nil ||
		len(item.PortName) > 63 || item.PortNumber <= 0 || item.PortNumber > 65535 {
		return workloadProjection{}, ErrWorkloadIdentityInsufficient
	}
	if item.Protocol != "TCP" {
		return workloadProjection{}, ErrWorkloadProtocolUnsupported
	}
	address, err := netip.ParseAddr(strings.TrimSpace(item.ClusterIP))
	if err != nil || address.IsUnspecified() || address.IsMulticast() {
		return workloadProjection{}, ErrWorkloadInventoryInvalidInput
	}
	item.ClusterIP = address.String()
	labels, err := normalizeWorkloadLabels(item.LabelsAllowlist)
	if err != nil {
		return workloadProjection{}, err
	}
	item.LabelsAllowlist = labels
	labelsJSON, _ := json.Marshal(labels)
	target := map[string]any{
		"service_uid": item.ServiceUID, "service_name": item.ServiceName, "cluster_ip": item.ClusterIP,
		"port_name": item.PortName, "port_number": item.PortNumber, "protocol": item.Protocol, "labels_allowlist": labels,
	}
	targetJSON, _ := json.Marshal(target)
	payloadJSON, _ := json.Marshal(item)
	return workloadProjection{
		StableKey:       supplyStableDigest("workload-service-port-v1", strings.Join([]string{item.ServiceUID, item.PortName, fmt.Sprint(item.PortNumber), item.Protocol}, "\x00")),
		IdentityQuality: model.WorkloadIdentityStrong, Ready: item.Ready, Labels: string(labelsJSON), Target: string(targetJSON),
		PayloadHash: sha256Hex(payloadJSON), DisplayName: fmt.Sprintf("%s:%d", item.ServiceName, item.PortNumber),
	}, nil
}

func normalizeWorkloadContainer(item workloadContainerEvidence) (workloadProjection, error) {
	item.WorkloadUID, item.WorkloadKind, item.WorkloadName = strings.TrimSpace(item.WorkloadUID), strings.TrimSpace(item.WorkloadKind), strings.TrimSpace(item.WorkloadName)
	item.PodUID, item.PodName, item.ContainerName = strings.TrimSpace(item.PodUID), strings.TrimSpace(item.PodName), strings.TrimSpace(item.ContainerName)
	if validateRequired("pod_uid", item.PodUID, 128) != nil || validateRequired("pod_name", item.PodName, 253) != nil ||
		validateRequired("container_name", item.ContainerName, 253) != nil || len(item.WorkloadUID) > 128 || len(item.WorkloadKind) > 64 || len(item.WorkloadName) > 253 {
		return workloadProjection{}, ErrWorkloadIdentityInsufficient
	}
	if item.Ready && (len(item.SSHUsers) != 1 || !validContainerSSHUser(strings.TrimSpace(item.SSHUsers[0]))) {
		return workloadProjection{}, ErrWorkloadInventoryInvalidInput
	}
	if len(item.SSHUsers) > 1 {
		return workloadProjection{}, ErrWorkloadInventoryInvalidInput
	}
	for i := range item.SSHUsers {
		item.SSHUsers[i] = strings.TrimSpace(item.SSHUsers[i])
	}
	quality := model.WorkloadIdentityStrong
	identityDomain, identityValue := "workload-container-v1", item.WorkloadUID
	if item.WorkloadUID == "" {
		quality = model.WorkloadIdentityEphemeral
		identityDomain, identityValue = "workload-container-ephemeral-v1", item.PodUID
	} else if item.WorkloadKind == "" || item.WorkloadName == "" {
		return workloadProjection{}, ErrWorkloadIdentityInsufficient
	}
	labels, err := normalizeWorkloadLabels(item.LabelsAllowlist)
	if err != nil {
		return workloadProjection{}, err
	}
	item.LabelsAllowlist = labels
	labelsJSON, _ := json.Marshal(labels)
	target := map[string]any{
		"workload_uid": item.WorkloadUID, "workload_kind": item.WorkloadKind, "workload_name": item.WorkloadName,
		"pod_uid": item.PodUID, "pod_name": item.PodName, "container_name": item.ContainerName, "labels_allowlist": labels,
		"ssh_users": item.SSHUsers,
	}
	targetJSON, _ := json.Marshal(target)
	payloadJSON, _ := json.Marshal(item)
	displayName := item.WorkloadName + "/" + item.ContainerName
	if item.WorkloadName == "" {
		displayName = item.PodName + "/" + item.ContainerName
	}
	return workloadProjection{
		StableKey: supplyStableDigest(identityDomain, identityValue+"\x00"+item.ContainerName), IdentityQuality: quality,
		Ready: item.Ready, Labels: string(labelsJSON), Target: string(targetJSON), PayloadHash: sha256Hex(payloadJSON), DisplayName: displayName,
	}, nil
}

func normalizeWorkloadLabels(labels map[string]string) (map[string]string, error) {
	result := make(map[string]string)
	for rawKey, rawValue := range labels {
		key, value := strings.TrimSpace(rawKey), strings.TrimSpace(rawValue)
		if len(key) > 253 || len(value) > 1024 || key == "" {
			return nil, ErrWorkloadInventoryInvalidInput
		}
		if key != workloadExposeLabel && !allowedSupplyNamespaceLabel(key) {
			continue
		}
		result[key] = value
		if len(result) > maxWorkloadLabelsPerObject {
			return nil, ErrWorkloadInventoryInvalidInput
		}
	}
	return result, nil
}

func sortWorkloadProjections(projections []workloadProjection) {
	sort.Slice(projections, func(i, j int) bool {
		if projections[i].StableKey == projections[j].StableKey {
			return projections[i].PayloadHash < projections[j].PayloadHash
		}
		return projections[i].StableKey < projections[j].StableKey
	})
}
