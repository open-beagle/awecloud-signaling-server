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
	WorkloadInventoryResultAccepted           = "WORKLOAD_ACCEPTED"
	WorkloadInventoryResultReplayed           = "WORKLOAD_REPLAYED"
	WorkloadInventoryResultSnapshotIncomplete = "WORKLOAD_SNAPSHOT_INCOMPLETE"
	workloadInventorySchemaVersion            = 1
	workloadInventoryLeaseDuration            = 2 * time.Minute
	workloadInventoryPayloadRetention         = 24 * time.Hour
	maxWorkloadInventoryPayloadBytes          = 1024 * 1024
	maxWorkloadInventoryBatchCount            = 1024
	maxWorkloadInventoryObjectsPerBatch       = 4096
	maxWorkloadLabelsPerObject                = 64
	workloadExposeLabel                       = "signal.beagle.io/expose"
)

var (
	ErrWorkloadInventoryInvalidInput     = errors.New("WORKLOAD_INVALID_INVENTORY")
	ErrWorkloadPayloadHashMismatch       = errors.New("WORKLOAD_PAYLOAD_HASH_MISMATCH")
	ErrWorkloadSequenceGap               = errors.New("WORKLOAD_SEQUENCE_GAP")
	ErrWorkloadSequenceConflict          = errors.New("WORKLOAD_SEQUENCE_CONFLICT")
	ErrWorkloadSourceEpochStale          = errors.New("WORKLOAD_SOURCE_EPOCH_STALE")
	ErrWorkloadSnapshotMetadataConflict  = errors.New("WORKLOAD_SNAPSHOT_METADATA_CONFLICT")
	ErrWorkloadSnapshotIncomplete        = errors.New(WorkloadInventoryResultSnapshotIncomplete)
	ErrWorkloadScopeNotTrusted           = errors.New("WORKLOAD_SCOPE_NOT_TRUSTED")
	ErrWorkloadProtocolUnsupported       = errors.New("WORKLOAD_PROTOCOL_UNSUPPORTED")
	ErrWorkloadIdentityInsufficient      = errors.New("WORKLOAD_IDENTITY_INSUFFICIENT")
	ErrWorkloadPayloadForbidden          = errors.New("WORKLOAD_PAYLOAD_FORBIDDEN")
	ErrWorkloadTechnicalCapabilityDenied = errors.New("WORKLOAD_CAPABILITY_NOT_ALLOWED")
)

type WorkloadInventoryService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewWorkloadInventoryService(database *gorm.DB) *WorkloadInventoryService {
	return &WorkloadInventoryService{db: database, now: time.Now}
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
	if s == nil || s.db == nil {
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
	var ack *WorkloadInventoryAck
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		source, err := resolveWorkloadInventorySource(tx, input)
		if err != nil {
			return err
		}
		scope, err := resolveTrustedWorkloadNamespaceScope(tx, source, input.ClusterIdentityDigest, input.NamespaceUID, now)
		if err != nil {
			return err
		}

		var existing model.WorkloadInventoryReceipt
		err = tx.Where("source_technical_resource_id = ? AND source_epoch = ? AND sequence = ?", source.ID, input.SourceEpoch, input.Sequence).First(&existing).Error
		if err == nil {
			if !sameWorkloadReceiptMetadata(&existing, input) {
				return ErrWorkloadSequenceConflict
			}
			ack = workloadAckFromReceipt(&existing, true, now)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := validateWorkloadSequence(tx, source.ID, input.SourceEpoch, input.Sequence); err != nil {
			return err
		}

		var snapshotReceipts []model.WorkloadInventoryReceipt
		if err := tx.Where("source_technical_resource_id = ? AND snapshot_id = ?", source.ID, input.SnapshotID).
			Order("batch_index ASC").Find(&snapshotReceipts).Error; err != nil {
			return err
		}
		for i := range snapshotReceipts {
			if snapshotReceipts[i].Status == model.WorkloadInventoryReceiptRejected {
				return ErrWorkloadSnapshotIncomplete
			}
			if !sameWorkloadSnapshotMetadata(&snapshotReceipts[i], input) || snapshotReceipts[i].Status != model.WorkloadInventoryReceiptStaging {
				return ErrWorkloadSnapshotMetadataConflict
			}
		}

		receipt := &model.WorkloadInventoryReceipt{
			ID: uuid.NewString(), SourceTechnicalResourceID: source.ID, SourceEpoch: input.SourceEpoch, Sequence: input.Sequence,
			SchemaVersion: input.SchemaVersion, SnapshotID: input.SnapshotID, BatchIndex: input.BatchIndex, BatchCount: input.BatchCount,
			ClusterIdentityDigest: input.ClusterIdentityDigest, NamespaceUID: input.NamespaceUID, NamespaceName: input.NamespaceName,
			Kind: input.Kind, PayloadHash: input.PayloadHash, ObservedAt: input.ObservedAt, ReceivedAt: now,
			LeaseExpiresAt: now.Add(workloadInventoryLeaseDuration), Status: model.WorkloadInventoryReceiptStaging,
			ResultCode: WorkloadInventoryResultAccepted, Retryable: false,
			PayloadDeleteAfter: timePointer(now.Add(workloadInventoryPayloadRetention)),
		}
		if err := tx.Create(receipt).Error; err != nil {
			if isDatabaseConstraintError(err) {
				return ErrWorkloadSequenceConflict
			}
			return err
		}
		if err := tx.Create(&model.WorkloadInventoryBatch{
			ID: uuid.NewString(), ReceiptID: receipt.ID, CanonicalPayload: string(canonicalPayload), CreatedAt: now,
		}).Error; err != nil {
			return err
		}

		snapshotReceipts = append(snapshotReceipts, *receipt)
		if len(snapshotReceipts) == input.BatchCount {
			if err := projectWorkloadSnapshot(tx, source, scope, snapshotReceipts, now); err != nil {
				return err
			}
			committedAt := now
			updated := tx.Model(&model.WorkloadInventoryReceipt{}).
				Where("source_technical_resource_id = ? AND snapshot_id = ? AND status = ?", source.ID, input.SnapshotID, model.WorkloadInventoryReceiptStaging).
				Updates(map[string]any{
					"status": model.WorkloadInventoryReceiptCommitted, "result_code": WorkloadInventoryResultAccepted,
					"committed_at": committedAt,
				})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != int64(input.BatchCount) {
				return ErrWorkloadSnapshotMetadataConflict
			}
			receipt.Status = model.WorkloadInventoryReceiptCommitted
			receipt.CommittedAt = &committedAt
		}
		ack = workloadAckFromReceipt(receipt, false, now)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ack, nil
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
	if input.SchemaVersion != workloadInventorySchemaVersion || input.Sequence <= 0 || input.BatchCount <= 0 || input.BatchCount > maxWorkloadInventoryBatchCount ||
		input.BatchIndex < 0 || input.BatchIndex >= input.BatchCount || !input.Kind.Valid() || input.ObservedAt.IsZero() ||
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

func validateWorkloadSequence(tx *gorm.DB, sourceID, epoch string, sequence int64) error {
	var latest model.WorkloadInventoryReceipt
	err := tx.Where("source_technical_resource_id = ? AND source_epoch = ?", sourceID, epoch).
		Order("sequence DESC").First(&latest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if sequence != 1 {
			return ErrWorkloadSequenceGap
		}
		return nil
	}
	if err != nil {
		return err
	}
	var newerEpochCount int64
	if err := tx.Model(&model.WorkloadInventoryReceipt{}).
		Where("source_technical_resource_id = ? AND source_epoch <> ? AND received_at > ?", sourceID, epoch, latest.ReceivedAt).
		Count(&newerEpochCount).Error; err != nil {
		return err
	}
	if newerEpochCount != 0 {
		return ErrWorkloadSourceEpochStale
	}
	if sequence != latest.Sequence+1 {
		return ErrWorkloadSequenceGap
	}
	return nil
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

func sameWorkloadReceiptMetadata(receipt *model.WorkloadInventoryReceipt, input ReceiveWorkloadInventoryBatchInput) bool {
	return receipt != nil && receipt.PayloadHash == input.PayloadHash && sameWorkloadSnapshotMetadata(receipt, input) &&
		receipt.Sequence == input.Sequence && receipt.BatchIndex == input.BatchIndex
}

func sameWorkloadSnapshotMetadata(receipt *model.WorkloadInventoryReceipt, input ReceiveWorkloadInventoryBatchInput) bool {
	return receipt != nil && receipt.SourceEpoch == input.SourceEpoch && receipt.SchemaVersion == input.SchemaVersion && receipt.SnapshotID == input.SnapshotID &&
		receipt.BatchCount == input.BatchCount && receipt.ClusterIdentityDigest == input.ClusterIdentityDigest && receipt.NamespaceUID == input.NamespaceUID &&
		receipt.NamespaceName == input.NamespaceName && receipt.Kind == input.Kind && receipt.ObservedAt.Equal(input.ObservedAt)
}

func workloadAckFromReceipt(receipt *model.WorkloadInventoryReceipt, replayed bool, now time.Time) *WorkloadInventoryAck {
	resultCode := receipt.ResultCode
	if replayed && receipt.Status != model.WorkloadInventoryReceiptRejected {
		resultCode = WorkloadInventoryResultReplayed
	}
	return &WorkloadInventoryAck{
		TechnicalResourceID: receipt.SourceTechnicalResourceID, AcceptedSequence: receipt.Sequence, SnapshotID: receipt.SnapshotID,
		BatchIndex: receipt.BatchIndex, ResultCode: resultCode, Replayed: replayed,
		Committed: receipt.Status == model.WorkloadInventoryReceiptCommitted, Retryable: receipt.Retryable, ServerReceivedAt: now,
	}
}

func (s *WorkloadInventoryService) PurgeExpiredPayloads(ctx context.Context, at time.Time) (int64, error) {
	if s == nil || s.db == nil || at.IsZero() {
		return 0, ErrWorkloadInventoryInvalidInput
	}
	at = at.UTC()
	var purged int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var expired []model.WorkloadInventoryReceipt
		if err := tx.Where("payload_delete_after IS NOT NULL AND payload_delete_after <= ?", at).Find(&expired).Error; err != nil {
			return err
		}
		for i := range expired {
			updates := map[string]any{"payload_delete_after": nil}
			if expired[i].Status == model.WorkloadInventoryReceiptStaging {
				updates["status"] = model.WorkloadInventoryReceiptRejected
				updates["result_code"] = WorkloadInventoryResultSnapshotIncomplete
				updates["retryable"] = false
			}
			if err := tx.Model(&model.WorkloadInventoryReceipt{}).Where("id = ?", expired[i].ID).Updates(updates).Error; err != nil {
				return err
			}
			deleted := tx.Where("receipt_id = ?", expired[i].ID).Delete(&model.WorkloadInventoryBatch{})
			if deleted.Error != nil {
				return deleted.Error
			}
			purged += deleted.RowsAffected
		}
		return nil
	})
	return purged, err
}

func sortWorkloadProjections(projections []workloadProjection) {
	sort.Slice(projections, func(i, j int) bool {
		if projections[i].StableKey == projections[j].StableKey {
			return projections[i].PayloadHash < projections[j].PayloadHash
		}
		return projections[i].StableKey < projections[j].StableKey
	})
}
