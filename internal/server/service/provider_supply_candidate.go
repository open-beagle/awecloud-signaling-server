package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

const (
	supplyCandidateLeaseDuration              = 10 * time.Minute
	supplyConflictCrossProvider               = "CROSS_PROVIDER_SUPPLY_IDENTITY_CONFLICT"
	supplyConflictSourceIdentity              = "SOURCE_STABLE_IDENTITY_COLLISION"
	supplyConflictInsufficientClusterIdentity = "INSUFFICIENT_STABLE_IDENTITY"
	supplyConflictInsufficientNamespace       = "INSUFFICIENT_NAMESPACE_IDENTITY"
	supplyConflictNamespaceIdentity           = "NAMESPACE_IDENTITY_COLLISION"
	maxSupplyCandidateSnapshotBytes           = 64 * 1024
	maxSupplyCandidateCapabilities            = 64
	maxSupplyCandidateNamespaces              = 4096
)

type supplyInventoryDocument struct {
	KubernetesClusters []supplyClusterEvidence `json:"kubernetes_clusters"`
}

type supplyClusterEvidence struct {
	ClusterUID             string                    `json:"cluster_uid"`
	KubeSystemNamespaceUID string                    `json:"kube_system_namespace_uid"`
	CASHA256               string                    `json:"ca_sha256"`
	DisplayName            string                    `json:"display_name"`
	KubernetesVersion      string                    `json:"kubernetes_version"`
	Capabilities           []string                  `json:"capabilities"`
	Namespaces             []supplyNamespaceEvidence `json:"namespaces"`
}

type supplyNamespaceEvidence struct {
	UID    string            `json:"uid"`
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
	Status string            `json:"status"`
}

type supplyCandidateProjection struct {
	StableKey   string
	Quality     model.SupplyIdentityQuality
	Conflict    string
	Evidence    supplyClusterEvidence
	Payload     []byte
	PayloadHash string
}

func projectSupplyCandidatesFromSnapshot(tx *gorm.DB, source *model.TechnicalResource, receipts []model.SupplyInventoryReceipt, now time.Time) error {
	if tx == nil || source == nil || source.ID == "" || source.ProviderID == "" || len(receipts) == 0 || now.IsZero() {
		return ErrProviderSupplyInvalidInput
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].BatchIndex < receipts[j].BatchIndex })

	projections := make(map[string]*supplyCandidateProjection)
	for _, receipt := range receipts {
		if receipt.CanonicalPayload == "" {
			return ErrProviderSupplyInvalidInput
		}
		var document supplyInventoryDocument
		if err := json.Unmarshal([]byte(receipt.CanonicalPayload), &document); err != nil {
			return fmt.Errorf("%w: decode committed Supply Inventory: %v", ErrProviderSupplyInvalidInput, err)
		}
		for index := range document.KubernetesClusters {
			projection, err := normalizeSupplyCandidateProjection(source.ID, document.KubernetesClusters[index])
			if err != nil {
				return err
			}
			if existing := projections[projection.StableKey]; existing != nil {
				if err := mergeSupplyCandidateProjection(existing, projection); err != nil {
					existing.Quality = model.SupplyIdentityCollision
					existing.Conflict = supplyConflictSourceIdentity
				}
				continue
			}
			projections[projection.StableKey] = projection
			if len(projections) > maxSupplyCandidateNamespaces {
				return ErrProviderSupplyInvalidInput
			}
		}
	}

	stableKeys := make([]string, 0, len(projections))
	for stableKey := range projections {
		stableKeys = append(stableKeys, stableKey)
	}
	sort.Strings(stableKeys)
	for _, stableKey := range stableKeys {
		projection := projections[stableKey]
		if err := finalizeSupplyCandidateProjection(projection); err != nil {
			return err
		}
		if err := upsertSupplyCandidate(tx, source, projection, now.UTC()); err != nil {
			return err
		}
	}
	for _, stableKey := range stableKeys {
		if projections[stableKey].Quality == model.SupplyIdentityInsufficient {
			continue
		}
		if err := reconcileCrossProviderSupplyConflict(tx, model.SupplyResourceKubernetes, stableKey, now.UTC()); err != nil {
			return err
		}
		if err := reconcileLinkedPlatformResourcesForStableKey(tx, model.SupplyResourceKubernetes, stableKey, now.UTC()); err != nil {
			return err
		}
	}
	return reconcileLinkedPlatformResourcesForSource(tx, source, now.UTC())
}

func normalizeSupplyCandidateProjection(sourceID string, evidence supplyClusterEvidence) (*supplyCandidateProjection, error) {
	evidence.ClusterUID = strings.TrimSpace(evidence.ClusterUID)
	evidence.KubeSystemNamespaceUID = strings.TrimSpace(evidence.KubeSystemNamespaceUID)
	evidence.CASHA256 = strings.ToLower(strings.TrimSpace(evidence.CASHA256))
	evidence.DisplayName = strings.TrimSpace(evidence.DisplayName)
	evidence.KubernetesVersion = strings.TrimSpace(evidence.KubernetesVersion)
	if len(evidence.ClusterUID) > 128 || len(evidence.KubeSystemNamespaceUID) > 128 || len(evidence.DisplayName) > 200 ||
		len(evidence.KubernetesVersion) > 100 || validateOptionalSHA256("ca_sha256", evidence.CASHA256) != nil {
		return nil, ErrProviderSupplyInvalidInput
	}

	capabilities := make(map[string]struct{}, len(evidence.Capabilities))
	for _, raw := range evidence.Capabilities {
		capability := strings.TrimSpace(raw)
		if capability == "" || len(capability) > 100 {
			return nil, ErrProviderSupplyInvalidInput
		}
		capabilities[capability] = struct{}{}
		if len(capabilities) > maxSupplyCandidateCapabilities {
			return nil, ErrProviderSupplyInvalidInput
		}
	}
	evidence.Capabilities = evidence.Capabilities[:0]
	for capability := range capabilities {
		evidence.Capabilities = append(evidence.Capabilities, capability)
	}
	sort.Strings(evidence.Capabilities)

	quality := model.SupplyIdentityStrong
	conflict := ""
	seenNamespaces := make(map[string]supplyNamespaceEvidence, len(evidence.Namespaces))
	normalizedNamespaces := make([]supplyNamespaceEvidence, 0, len(evidence.Namespaces))
	for _, namespace := range evidence.Namespaces {
		normalized, err := normalizeSupplyNamespaceEvidence(namespace)
		if err != nil {
			quality = model.SupplyIdentityInsufficient
			conflict = supplyConflictInsufficientNamespace
			continue
		}
		if prior, exists := seenNamespaces[normalized.UID]; exists {
			if !equalSupplyNamespaceEvidence(prior, normalized) {
				quality = model.SupplyIdentityCollision
				conflict = supplyConflictNamespaceIdentity
			}
			continue
		}
		seenNamespaces[normalized.UID] = normalized
		normalizedNamespaces = append(normalizedNamespaces, normalized)
		if len(normalizedNamespaces) > maxSupplyCandidateNamespaces {
			return nil, ErrProviderSupplyInvalidInput
		}
	}
	sort.Slice(normalizedNamespaces, func(i, j int) bool { return normalizedNamespaces[i].UID < normalizedNamespaces[j].UID })
	evidence.Namespaces = normalizedNamespaces

	identityKind, identityValue := "cluster_uid", evidence.ClusterUID
	if identityValue == "" {
		identityKind, identityValue = "kube_system_namespace_uid", evidence.KubeSystemNamespaceUID
	}
	var stableKey string
	if identityValue == "" {
		quality = model.SupplyIdentityInsufficient
		conflict = supplyConflictInsufficientClusterIdentity
		raw, err := json.Marshal(evidence)
		if err != nil {
			return nil, err
		}
		stableKey = supplyStableDigest("kubernetes-insufficient-v1", sourceID+"\x00"+sha256Hex(raw))
	} else {
		stableKey = supplyStableDigest("kubernetes-cluster-v1:"+identityKind, identityValue)
	}
	return &supplyCandidateProjection{StableKey: stableKey, Quality: quality, Conflict: conflict, Evidence: evidence}, nil
}

func normalizeSupplyNamespaceEvidence(namespace supplyNamespaceEvidence) (supplyNamespaceEvidence, error) {
	namespace.UID = strings.TrimSpace(namespace.UID)
	namespace.Name = strings.TrimSpace(namespace.Name)
	namespace.Status = strings.TrimSpace(namespace.Status)
	if validateRequired("namespace_uid", namespace.UID, 128) != nil || validateRequired("namespace_name", namespace.Name, 253) != nil || len(namespace.Status) > 64 {
		return supplyNamespaceEvidence{}, ErrProviderSupplyInvalidInput
	}
	labels := make(map[string]string)
	for key, value := range namespace.Labels {
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !allowedSupplyNamespaceLabel(key) {
			continue
		}
		if len(key) > 253 || len(value) > 1024 {
			return supplyNamespaceEvidence{}, ErrProviderSupplyInvalidInput
		}
		labels[key] = value
	}
	labelSnapshot, err := json.Marshal(labels)
	if err != nil || len(labelSnapshot) > 16*1024 {
		return supplyNamespaceEvidence{}, ErrProviderSupplyInvalidInput
	}
	namespace.Labels = labels
	return namespace, nil
}

func allowedSupplyNamespaceLabel(key string) bool {
	switch key {
	case "environment", "team", "owner":
		return true
	default:
		return strings.HasPrefix(key, "app.kubernetes.io/")
	}
}

func mergeSupplyCandidateProjection(target, incoming *supplyCandidateProjection) error {
	if target == nil || incoming == nil || target.StableKey != incoming.StableKey ||
		target.Evidence.ClusterUID != incoming.Evidence.ClusterUID ||
		target.Evidence.KubeSystemNamespaceUID != incoming.Evidence.KubeSystemNamespaceUID ||
		(target.Evidence.CASHA256 != "" && incoming.Evidence.CASHA256 != "" && target.Evidence.CASHA256 != incoming.Evidence.CASHA256) {
		return ErrProviderSupplyConflict
	}
	if target.Evidence.CASHA256 == "" {
		target.Evidence.CASHA256 = incoming.Evidence.CASHA256
	}
	if target.Evidence.DisplayName == "" {
		target.Evidence.DisplayName = incoming.Evidence.DisplayName
	}
	if target.Evidence.KubernetesVersion == "" {
		target.Evidence.KubernetesVersion = incoming.Evidence.KubernetesVersion
	}
	capabilities := make(map[string]struct{})
	for _, capability := range append(append([]string(nil), target.Evidence.Capabilities...), incoming.Evidence.Capabilities...) {
		capabilities[capability] = struct{}{}
	}
	target.Evidence.Capabilities = target.Evidence.Capabilities[:0]
	for capability := range capabilities {
		target.Evidence.Capabilities = append(target.Evidence.Capabilities, capability)
	}
	sort.Strings(target.Evidence.Capabilities)

	namespaces := make(map[string]supplyNamespaceEvidence, len(target.Evidence.Namespaces)+len(incoming.Evidence.Namespaces))
	for _, namespace := range target.Evidence.Namespaces {
		namespaces[namespace.UID] = namespace
	}
	for _, namespace := range incoming.Evidence.Namespaces {
		if prior, ok := namespaces[namespace.UID]; ok && !equalSupplyNamespaceEvidence(prior, namespace) {
			return ErrProviderSupplyConflict
		}
		namespaces[namespace.UID] = namespace
	}
	target.Evidence.Namespaces = target.Evidence.Namespaces[:0]
	for _, namespace := range namespaces {
		target.Evidence.Namespaces = append(target.Evidence.Namespaces, namespace)
	}
	sort.Slice(target.Evidence.Namespaces, func(i, j int) bool { return target.Evidence.Namespaces[i].UID < target.Evidence.Namespaces[j].UID })
	if incoming.Quality == model.SupplyIdentityCollision || target.Quality == model.SupplyIdentityCollision {
		target.Quality = model.SupplyIdentityCollision
		target.Conflict = supplyConflictSourceIdentity
	} else if incoming.Quality == model.SupplyIdentityInsufficient {
		target.Quality = model.SupplyIdentityInsufficient
		target.Conflict = incoming.Conflict
	}
	return nil
}

func finalizeSupplyCandidateProjection(projection *supplyCandidateProjection) error {
	if projection == nil || projection.StableKey == "" {
		return ErrProviderSupplyInvalidInput
	}
	payload, err := json.Marshal(projection.Evidence)
	if err != nil || len(payload) == 0 || len(payload) > maxSupplyCandidateSnapshotBytes {
		return ErrProviderSupplyInvalidInput
	}
	projection.Payload = payload
	projection.PayloadHash = sha256Hex(payload)
	return nil
}

func upsertSupplyCandidate(tx *gorm.DB, source *model.TechnicalResource, projection *supplyCandidateProjection, now time.Time) error {
	var candidate model.SupplyCandidate
	err := tx.Where("technical_resource_id = ? AND resource_type = ? AND stable_key = ?", source.ID, model.SupplyResourceKubernetes, projection.StableKey).
		First(&candidate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		reviewState := model.SupplyCandidatePendingReview
		if projection.Conflict != "" && projection.Quality == model.SupplyIdentityCollision {
			reviewState = model.SupplyCandidateConflict
		}
		candidate = model.SupplyCandidate{
			ID: uuid.NewString(), ProviderID: source.ProviderID, TechnicalResourceID: source.ID,
			ResourceType: model.SupplyResourceKubernetes, StableKey: projection.StableKey,
			IdentityQuality: projection.Quality, PayloadHash: projection.PayloadHash, ObservationSnapshot: string(projection.Payload),
			FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.Add(supplyCandidateLeaseDuration),
			ReviewState: reviewState, ConflictCode: projection.Conflict, RowVersion: 1,
		}
		if err := tx.Create(&candidate).Error; err != nil {
			if isDatabaseConstraintError(err) {
				return ErrProviderSupplyConflict
			}
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if candidate.ProviderID != source.ProviderID {
		return ErrProviderSupplyConflict
	}
	reviewState := candidate.ReviewState
	if reviewState != model.SupplyCandidateRejected && reviewState != model.SupplyCandidateLinked && reviewState != model.SupplyCandidateAccepted {
		reviewState = model.SupplyCandidatePendingReview
		if projection.Conflict != "" && projection.Quality == model.SupplyIdentityCollision {
			reviewState = model.SupplyCandidateConflict
		}
	}
	updates := map[string]any{
		"identity_quality":     projection.Quality,
		"payload_hash":         projection.PayloadHash,
		"observation_snapshot": string(projection.Payload),
		"last_observed_at":     now,
		"lease_expires_at":     now.Add(supplyCandidateLeaseDuration),
		"review_state":         reviewState,
		"conflict_code":        projection.Conflict,
		"opaque_conflict_id":   "",
		"row_version":          gorm.Expr("row_version + 1"),
	}
	return tx.Model(&model.SupplyCandidate{}).Where("provider_id = ? AND id = ?", source.ProviderID, candidate.ID).Updates(updates).Error
}

func reconcileCrossProviderSupplyConflict(tx *gorm.DB, resourceType model.SupplyResourceType, stableKey string, now time.Time) error {
	var candidates []model.SupplyCandidate
	if err := tx.Where("resource_type = ? AND stable_key = ? AND julianday(lease_expires_at) > julianday(?) AND identity_quality <> ?",
		resourceType, stableKey, now, model.SupplyIdentityInsufficient).Find(&candidates).Error; err != nil {
		return err
	}
	providers := make(map[string]struct{})
	for _, candidate := range candidates {
		providers[candidate.ProviderID] = struct{}{}
	}
	if len(providers) <= 1 {
		return tx.Model(&model.SupplyCandidate{}).
			Where("resource_type = ? AND stable_key = ? AND julianday(lease_expires_at) > julianday(?) AND conflict_code = ?", resourceType, stableKey, now, supplyConflictCrossProvider).
			Updates(map[string]any{
				"identity_quality":   model.SupplyIdentityStrong,
				"conflict_code":      "",
				"opaque_conflict_id": "",
				"review_state":       gorm.Expr("CASE WHEN review_state = ? THEN ? ELSE review_state END", model.SupplyCandidateConflict, model.SupplyCandidatePendingReview),
				"row_version":        gorm.Expr("row_version + 1"),
			}).Error
	}
	opaqueID := supplyStableDigest("cross-provider-supply-conflict-v1", string(resourceType)+"\x00"+stableKey)
	return tx.Model(&model.SupplyCandidate{}).
		Where("resource_type = ? AND stable_key = ? AND julianday(lease_expires_at) > julianday(?)", resourceType, stableKey, now).
		Updates(map[string]any{
			"identity_quality":   model.SupplyIdentityCollision,
			"conflict_code":      supplyConflictCrossProvider,
			"opaque_conflict_id": opaqueID,
			"review_state": gorm.Expr("CASE WHEN review_state IN ? THEN review_state ELSE ? END",
				[]model.SupplyCandidateReviewState{model.SupplyCandidateRejected, model.SupplyCandidateAccepted, model.SupplyCandidateLinked}, model.SupplyCandidateConflict),
			"row_version": gorm.Expr("row_version + 1"),
		}).Error
}

type RejectSupplyCandidateInput struct {
	CandidateID        string
	ExpectedRowVersion int64
	Reason             string
}

func (s *ProviderSupplyService) RejectSupplyCandidate(ctx context.Context, authorization *ManagementAuthorizationContext, input RejectSupplyCandidateInput) (*model.SupplyCandidate, error) {
	if s == nil || s.db == nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	input.CandidateID = strings.TrimSpace(input.CandidateID)
	input.Reason = strings.TrimSpace(input.Reason)
	if validateRequired("candidate_id", input.CandidateID, 36) != nil || validateRequired("reason", input.Reason, 500) != nil || input.ExpectedRowVersion <= 0 {
		return nil, ErrProviderSupplyInvalidInput
	}
	now := s.now().UTC()
	var candidate model.SupplyCandidate
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		providerID, err := reauthorizeProviderPermission(tx, authorization, PermissionProviderResourcesWrite, now)
		if err != nil {
			return err
		}
		if err := tx.Where("provider_id = ? AND id = ?", providerID, input.CandidateID).First(&candidate).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProviderSupplyObjectNotFound
			}
			return err
		}
		if candidate.RowVersion != input.ExpectedRowVersion {
			return ErrProviderSupplyVersionConflict
		}
		if candidate.ReviewState == model.SupplyCandidateLinked || candidate.ReviewState == model.SupplyCandidateAccepted {
			return ErrProviderSupplyConflict
		}
		result := tx.Model(&model.SupplyCandidate{}).
			Where("provider_id = ? AND id = ? AND row_version = ?", providerID, candidate.ID, candidate.RowVersion).
			Updates(map[string]any{
				"review_state":        model.SupplyCandidateRejected,
				"reviewed_by_user_id": authorization.EffectiveUserID,
				"reviewed_at":         now,
				"row_version":         gorm.Expr("row_version + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrProviderSupplyVersionConflict
		}
		return tx.Where("provider_id = ? AND id = ?", providerID, candidate.ID).First(&candidate).Error
	})
	if err != nil {
		return nil, err
	}
	return &candidate, nil
}

type AcceptSupplyCandidateInput struct {
	CandidateID        string
	ExpectedRowVersion int64
	DisplayName        string
	Reason             string
}

type AcceptSupplyCandidateResult struct {
	Candidate       *model.SupplyCandidate        `json:"candidate"`
	Resource        *model.PlatformResource       `json:"resource"`
	Source          *model.PlatformResourceSource `json:"source"`
	ClusterScope    *model.ResourceScope          `json:"cluster_scope"`
	NamespaceScopes []model.ResourceScope         `json:"namespace_scopes"`
}

func (s *ProviderSupplyService) AcceptSupplyCandidate(ctx context.Context, authorization *ManagementAuthorizationContext, input AcceptSupplyCandidateInput) (*AcceptSupplyCandidateResult, error) {
	if s == nil || s.db == nil {
		return nil, ErrProviderSupplyInvalidInput
	}
	input.CandidateID = strings.TrimSpace(input.CandidateID)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Reason = strings.TrimSpace(input.Reason)
	if validateRequired("candidate_id", input.CandidateID, 36) != nil || validateRequired("reason", input.Reason, 500) != nil ||
		len(input.DisplayName) > 200 || input.ExpectedRowVersion <= 0 {
		return nil, ErrProviderSupplyInvalidInput
	}
	now := s.now().UTC()
	result := &AcceptSupplyCandidateResult{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		providerID, err := reauthorizeProviderPermission(tx, authorization, PermissionProviderResourcesWrite, now)
		if err != nil {
			return err
		}
		var candidate model.SupplyCandidate
		if err := tx.Where("provider_id = ? AND id = ?", providerID, input.CandidateID).First(&candidate).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProviderSupplyObjectNotFound
			}
			return err
		}
		if candidate.RowVersion != input.ExpectedRowVersion {
			return ErrProviderSupplyVersionConflict
		}
		if candidate.ResourceType != model.SupplyResourceKubernetes || candidate.ReviewState != model.SupplyCandidatePendingReview ||
			candidate.IdentityQuality != model.SupplyIdentityStrong || candidate.ConflictCode != "" || !candidate.LeaseExpiresAt.After(now) {
			return ErrProviderSupplyConflict
		}
		var evidence supplyClusterEvidence
		if err := json.Unmarshal([]byte(candidate.ObservationSnapshot), &evidence); err != nil {
			return ErrProviderSupplyInvalidInput
		}
		projection, err := normalizeSupplyCandidateProjection(candidate.TechnicalResourceID, evidence)
		if err != nil || projection.StableKey != candidate.StableKey || projection.Quality != model.SupplyIdentityStrong {
			return ErrProviderSupplyConflict
		}

		resource, err := findOrCreatePlatformResource(tx, &candidate, projection.Evidence, input.DisplayName)
		if err != nil {
			return err
		}
		source, err := linkPlatformResourceSource(tx, resource, &candidate, now)
		if err != nil {
			return err
		}
		clusterScope, namespaceScopes, err := materializeKubernetesScopes(tx, resource, projection.Evidence, now)
		if err != nil {
			return err
		}
		updated := tx.Model(&model.SupplyCandidate{}).
			Where("provider_id = ? AND id = ? AND row_version = ? AND review_state = ?", providerID, candidate.ID, candidate.RowVersion, model.SupplyCandidatePendingReview).
			Updates(map[string]any{
				"review_state":        model.SupplyCandidateLinked,
				"reviewed_by_user_id": authorization.EffectiveUserID,
				"reviewed_at":         now,
				"row_version":         gorm.Expr("row_version + 1"),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrProviderSupplyVersionConflict
		}
		if err := tx.Where("provider_id = ? AND id = ?", providerID, candidate.ID).First(&candidate).Error; err != nil {
			return err
		}
		result.Candidate, result.Resource, result.Source = &candidate, resource, source
		result.ClusterScope, result.NamespaceScopes = clusterScope, namespaceScopes
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func findOrCreatePlatformResource(tx *gorm.DB, candidate *model.SupplyCandidate, evidence supplyClusterEvidence, displayName string) (*model.PlatformResource, error) {
	var resource model.PlatformResource
	err := tx.Where("provider_id = ? AND type = ? AND stable_key = ?", candidate.ProviderID, candidate.ResourceType, candidate.StableKey).First(&resource).Error
	if err == nil {
		if resource.LifecycleState == model.PlatformResourceRetired {
			return nil, ErrProviderSupplyConflict
		}
		return &resource, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if displayName == "" {
		displayName = evidence.DisplayName
	}
	if displayName == "" {
		displayName = "Kubernetes " + candidate.StableKey[:12]
	}
	resource = model.PlatformResource{
		ID: uuid.NewString(), ProviderID: candidate.ProviderID, Type: candidate.ResourceType, StableKey: candidate.StableKey,
		DisplayName: displayName, LifecycleState: model.PlatformResourceDraft, HealthState: model.ResourceHealthOnline,
		CapabilityRevision: 1, AllocatableScopeCount: 0, RowVersion: 1,
	}
	if err := tx.Create(&resource).Error; err != nil {
		if isDatabaseConstraintError(err) {
			return nil, ErrProviderSupplyConflict
		}
		return nil, err
	}
	return &resource, nil
}

func linkPlatformResourceSource(tx *gorm.DB, resource *model.PlatformResource, candidate *model.SupplyCandidate, now time.Time) (*model.PlatformResourceSource, error) {
	var existing model.PlatformResourceSource
	err := tx.Where("supply_candidate_id = ?", candidate.ID).First(&existing).Error
	if err == nil {
		if existing.PlatformResourceID != resource.ID || existing.ProviderID != resource.ProviderID {
			return nil, ErrProviderSupplyConflict
		}
		if err := tx.Model(&model.PlatformResourceSource{}).Where("id = ?", existing.ID).Update("last_confirmed_at", now).Error; err != nil {
			return nil, err
		}
		existing.LastConfirmedAt = now
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var sourceCount int64
	if err := tx.Model(&model.PlatformResourceSource{}).Where("provider_id = ? AND platform_resource_id = ?", resource.ProviderID, resource.ID).Count(&sourceCount).Error; err != nil {
		return nil, err
	}
	source := &model.PlatformResourceSource{
		ID: uuid.NewString(), ProviderID: resource.ProviderID, PlatformResourceID: resource.ID, SupplyCandidateID: candidate.ID,
		IsPrimary: sourceCount == 0, LinkedAt: now, LastConfirmedAt: now,
	}
	if err := tx.Create(source).Error; err != nil {
		if isDatabaseConstraintError(err) {
			return nil, ErrProviderSupplyConflict
		}
		return nil, err
	}
	return source, nil
}

func LegacyHostStableKey(sourceType model.TechnicalResourceBindingSourceType, sourceID string) string {
	return fmt.Sprintf("legacy-host-%s:%s", sourceType, strings.TrimSpace(sourceID))
}

func EnsureLegacyHostPlatformResource(tx *gorm.DB, source *model.TechnicalResource, node *model.Node, reviewedByUserID uint64, now time.Time) error {
	if tx == nil || source == nil || node == nil || source.ID == "" || source.ProviderID == "" || node.ID == 0 || now.IsZero() {
		return ErrProviderSupplyInvalidInput
	}
	sourceID := fmt.Sprint(node.ID)
	stableKey := LegacyHostStableKey(model.TechnicalResourceBindingLegacyNode, sourceID)
	displayName := strings.TrimSpace(node.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(node.Hostname)
	}
	if displayName == "" {
		displayName = stableKey
	}
	evidence := map[string]any{
		"source_type":       string(model.TechnicalResourceBindingLegacyNode),
		"source_id":         sourceID,
		"node_id":           node.ID,
		"name":              node.Name,
		"hostname":          node.Hostname,
		"host_domain_label": node.HostDomainLabel,
	}
	payload, err := json.Marshal(evidence)
	if err != nil || len(payload) == 0 || len(payload) > maxSupplyCandidateSnapshotBytes {
		return ErrProviderSupplyInvalidInput
	}
	observedAt := now.UTC()
	leaseExpiresAt := observedAt.Add(supplyCandidateLeaseDuration)
	var reviewedBy *uint64
	if reviewedByUserID != 0 {
		reviewedBy = &reviewedByUserID
	}

	var candidate model.SupplyCandidate
	err = tx.Where("technical_resource_id = ? AND resource_type = ? AND stable_key = ?", source.ID, model.SupplyResourceHost, stableKey).
		First(&candidate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		candidate = model.SupplyCandidate{
			ID: uuid.NewString(), ProviderID: source.ProviderID, TechnicalResourceID: source.ID,
			ResourceType: model.SupplyResourceHost, StableKey: stableKey, IdentityQuality: model.SupplyIdentityStrong,
			PayloadHash: sha256Hex(payload), ObservationSnapshot: string(payload),
			FirstObservedAt: observedAt, LastObservedAt: observedAt, LeaseExpiresAt: leaseExpiresAt,
			ReviewState: model.SupplyCandidateLinked, ReviewedByUserID: reviewedBy, ReviewedAt: &observedAt, RowVersion: 1,
		}
		if err := tx.Create(&candidate).Error; err != nil {
			if isDatabaseConstraintError(err) {
				return ErrProviderSupplyConflict
			}
			return err
		}
	} else if err != nil {
		return err
	} else if candidate.ProviderID != source.ProviderID {
		return ErrProviderSupplyConflict
	} else {
		updates := map[string]any{
			"identity_quality":     model.SupplyIdentityStrong,
			"payload_hash":         sha256Hex(payload),
			"observation_snapshot": string(payload),
			"last_observed_at":     observedAt,
			"lease_expires_at":     leaseExpiresAt,
			"review_state":         model.SupplyCandidateLinked,
			"conflict_code":        "",
			"reviewed_at":          observedAt,
			"row_version":          gorm.Expr("row_version + 1"),
		}
		if reviewedBy != nil {
			updates["reviewed_by_user_id"] = *reviewedBy
		}
		if err := tx.Model(&model.SupplyCandidate{}).Where("provider_id = ? AND id = ?", candidate.ProviderID, candidate.ID).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Where("provider_id = ? AND id = ?", candidate.ProviderID, candidate.ID).First(&candidate).Error; err != nil {
			return err
		}
	}

	var resource model.PlatformResource
	err = tx.Where("provider_id = ? AND type = ? AND stable_key = ?", source.ProviderID, model.SupplyResourceHost, stableKey).First(&resource).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		resource = model.PlatformResource{
			ID: uuid.NewString(), ProviderID: source.ProviderID, Type: model.SupplyResourceHost, StableKey: stableKey,
			DisplayName: displayName, LifecycleState: model.PlatformResourceActive, HealthState: model.ResourceHealthOnline,
			CapabilityRevision: 1, AllocatableScopeCount: 0, RowVersion: 1,
		}
		if err := tx.Create(&resource).Error; err != nil {
			if isDatabaseConstraintError(err) {
				return ErrProviderSupplyConflict
			}
			return err
		}
	} else if err != nil {
		return err
	} else if resource.LifecycleState == model.PlatformResourceRetired {
		return ErrProviderSupplyConflict
	}
	_, err = linkPlatformResourceSource(tx, &resource, &candidate, observedAt)
	return err
}

func materializeKubernetesScopes(tx *gorm.DB, resource *model.PlatformResource, evidence supplyClusterEvidence, now time.Time) (*model.ResourceScope, []model.ResourceScope, error) {
	clusterScope, err := findOrCreateClusterScope(tx, resource)
	if err != nil {
		return nil, nil, err
	}
	namespaceScopes := make([]model.ResourceScope, 0, len(evidence.Namespaces))
	for _, namespace := range evidence.Namespaces {
		observation, err := upsertNamespaceObservation(tx, resource, namespace, now)
		if err != nil {
			return nil, nil, err
		}
		scope, err := findOrCreateNamespaceScope(tx, resource, clusterScope, observation)
		if err != nil {
			return nil, nil, err
		}
		namespaceScopes = append(namespaceScopes, *scope)
	}
	return clusterScope, namespaceScopes, nil
}

func findOrCreateClusterScope(tx *gorm.DB, resource *model.PlatformResource) (*model.ResourceScope, error) {
	var scope model.ResourceScope
	err := tx.Where("provider_id = ? AND platform_resource_id = ? AND type = ? AND stable_key = ?",
		resource.ProviderID, resource.ID, model.ResourceScopeCluster, resource.StableKey).First(&scope).Error
	if err == nil {
		if scope.LifecycleState == model.ResourceScopeRetired {
			return nil, ErrProviderSupplyConflict
		}
		return &scope, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	scope = model.ResourceScope{
		ID: uuid.NewString(), ProviderID: resource.ProviderID, PlatformResourceID: resource.ID,
		Type: model.ResourceScopeCluster, StableKey: resource.StableKey, LifecycleState: model.ResourceScopeDraft,
		IsolationMode: model.ResourceScopeIsolationNone, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
	}
	if err := tx.Create(&scope).Error; err != nil {
		return nil, err
	}
	return &scope, nil
}

func upsertNamespaceObservation(tx *gorm.DB, resource *model.PlatformResource, namespace supplyNamespaceEvidence, now time.Time) (*model.NamespaceObservation, error) {
	labels, err := json.Marshal(namespace.Labels)
	if err != nil || len(labels) > 16*1024 {
		return nil, ErrProviderSupplyInvalidInput
	}
	var observation model.NamespaceObservation
	err = tx.Where("cluster_resource_id = ? AND namespace_uid = ?", resource.ID, namespace.UID).First(&observation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		observation = model.NamespaceObservation{
			ID: uuid.NewString(), ProviderID: resource.ProviderID, ClusterResourceID: resource.ID,
			NamespaceUID: namespace.UID, Name: namespace.Name, LabelSnapshot: string(labels), Revision: 1,
			ObservedAt: now, LeaseExpiresAt: now.Add(supplyCandidateLeaseDuration), State: model.NamespaceObservationObserved,
		}
		if err := tx.Create(&observation).Error; err != nil {
			return nil, err
		}
		return &observation, nil
	}
	if err != nil {
		return nil, err
	}
	revision := observation.Revision
	if observation.Name != namespace.Name || observation.LabelSnapshot != string(labels) || observation.State != model.NamespaceObservationObserved {
		revision++
	}
	leaseExpiresAt := now.Add(supplyCandidateLeaseDuration)
	if observation.Name == namespace.Name && observation.LabelSnapshot == string(labels) &&
		observation.State == model.NamespaceObservationObserved && observation.ObservedAt.Equal(now) && observation.LeaseExpiresAt.Equal(leaseExpiresAt) {
		return &observation, nil
	}
	if err := tx.Model(&model.NamespaceObservation{}).Where("provider_id = ? AND id = ?", resource.ProviderID, observation.ID).
		Updates(map[string]any{
			"name": namespace.Name, "label_snapshot": string(labels), "revision": revision,
			"observed_at": now, "lease_expires_at": leaseExpiresAt, "state": model.NamespaceObservationObserved,
		}).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("provider_id = ? AND id = ?", resource.ProviderID, observation.ID).First(&observation).Error; err != nil {
		return nil, err
	}
	return &observation, nil
}

func findOrCreateNamespaceScope(tx *gorm.DB, resource *model.PlatformResource, parent *model.ResourceScope, observation *model.NamespaceObservation) (*model.ResourceScope, error) {
	stableKey := supplyStableDigest("kubernetes-namespace-v1", resource.ID+"\x00"+observation.NamespaceUID)
	var scope model.ResourceScope
	err := tx.Where("provider_id = ? AND platform_resource_id = ? AND type = ? AND stable_key = ?",
		resource.ProviderID, resource.ID, model.ResourceScopeNamespace, stableKey).First(&scope).Error
	if err == nil {
		if scope.ParentID == nil || *scope.ParentID != parent.ID || scope.NamespaceObservationID == nil || *scope.NamespaceObservationID != observation.ID {
			return nil, ErrProviderSupplyConflict
		}
		if scope.LifecycleState != model.ResourceScopeRetired && scope.EvidenceRevision != observation.Revision {
			if err := tx.Model(&model.ResourceScope{}).Where("provider_id = ? AND id = ?", resource.ProviderID, scope.ID).
				Updates(map[string]any{"evidence_revision": observation.Revision, "row_version": gorm.Expr("row_version + 1")}).Error; err != nil {
				return nil, err
			}
			scope.EvidenceRevision = observation.Revision
			scope.RowVersion++
		}
		return &scope, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	parentID, observationID := parent.ID, observation.ID
	scope = model.ResourceScope{
		ID: uuid.NewString(), ProviderID: resource.ProviderID, PlatformResourceID: resource.ID,
		Type: model.ResourceScopeNamespace, StableKey: stableKey, ParentID: &parentID, NamespaceObservationID: &observationID,
		LifecycleState: model.ResourceScopeDraft, IsolationMode: model.ResourceScopeIsolationNamespaceIsolated,
		ConfigRevision: 1, EvidenceRevision: observation.Revision, RowVersion: 1,
	}
	if err := tx.Create(&scope).Error; err != nil {
		return nil, err
	}
	return &scope, nil
}

func equalSupplyNamespaceEvidence(left, right supplyNamespaceEvidence) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func supplyStableDigest(domain, value string) string {
	return sha256Hex([]byte(domain + "\x00" + strings.TrimSpace(value)))
}
