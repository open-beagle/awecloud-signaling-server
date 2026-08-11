package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	serverdb "github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type report struct {
	ProviderID            string `json:"provider_id"`
	ProviderKey           string `json:"provider_key"`
	Agents                int    `json:"legacy_agents"`
	Endpoints             int    `json:"legacy_endpoints"`
	OrphanEndpointParents int    `json:"orphan_endpoint_parents"`
	TechnicalResources    int64  `json:"technical_resources"`
	TechnicalBindings     int64  `json:"technical_bindings"`
	HostResources         int64  `json:"host_resources"`
	KubernetesResources   int64  `json:"kubernetes_resources"`
	SupplyCandidates      int64  `json:"supply_candidates"`
	PlatformSources       int64  `json:"platform_resource_sources"`
	ClusterScopes         int64  `json:"cluster_scopes"`
	Integrity             string `json:"integrity"`
}

type legacySource struct {
	technical *model.TechnicalResource
	kind      model.TechnicalResourceBindingSourceType
	id        string
	name      string
	health    model.ResourceHealthState
	k8s       bool
}

func main() {
	database := flag.String("database", "", "SQLite database to update")
	providerKey := flag.String("provider-key", "beagle", "ResourceProvider key")
	actorUsername := flag.String("actor-username", "admin", "unified management username recorded as actor")
	output := flag.String("output", "", "report JSON path")
	apply := flag.Bool("apply", false, "required acknowledgement that the database may be modified")
	flag.Parse()
	if err := run(*database, *providerKey, *actorUsername, *output, *apply); err != nil {
		fmt.Fprintln(os.Stderr, "legacy-beagle-provider-backfill:", err)
		os.Exit(1)
	}
}

func run(databasePath, providerKey, actorUsername, outputPath string, apply bool) error {
	if !apply {
		return errors.New("-apply is required")
	}
	if strings.TrimSpace(databasePath) == "" || strings.TrimSpace(providerKey) == "" || strings.TrimSpace(actorUsername) == "" || strings.TrimSpace(outputPath) == "" {
		return errors.New("-database, -provider-key, -actor-username and -output are required")
	}
	if err := serverdb.InitDB(config.DatabaseSection{Type: "sqlite", Path: databasePath}); err != nil {
		return err
	}
	defer func() {
		if sqlDB, err := serverdb.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	var provider model.ResourceProvider
	if err := serverdb.DB.Where("key = ? AND status = ?", providerKey, model.ProviderStatusActive).First(&provider).Error; err != nil {
		return fmt.Errorf("load active ResourceProvider: %w", err)
	}
	var actor model.UserIdentityProfile
	if err := serverdb.DB.Where("username = ? AND enabled = ?", actorUsername, true).First(&actor).Error; err != nil {
		return fmt.Errorf("load unified actor: %w", err)
	}

	result := report{ProviderID: provider.ID, ProviderKey: provider.Key}
	err := serverdb.DB.Transaction(func(tx *gorm.DB) error {
		var nodes []model.Node
		if err := tx.Where("type = ?", model.NodeTypeAgent).Order("id").Find(&nodes).Error; err != nil {
			return err
		}
		var endpoints []model.Endpoint
		if err := tx.Order("id").Find(&endpoints).Error; err != nil {
			return err
		}
		result.Agents, result.Endpoints = len(nodes), len(endpoints)

		now := time.Now().UTC()
		parents := make(map[uint64]*model.TechnicalResource, len(nodes))
		parentBound := make(map[uint64]bool, len(nodes))
		sources := make([]legacySource, 0, len(nodes)+len(endpoints))
		for i := range nodes {
			node := &nodes[i]
			health := heartbeatHealth(node.LastHeartbeat, now)
			technical, err := ensureTechnical(tx, provider.ID, model.TechnicalResourceAgent, "legacy-node:"+strconv.FormatUint(node.ID, 10), nil, model.TechnicalResourceRegistered, health, node.LastHeartbeat, node.CreatedAt)
			if err != nil {
				return err
			}
			if err := ensureBinding(tx, technical, model.TechnicalResourceBindingLegacyNode, strconv.FormatUint(node.ID, 10), actor.UserID, now); err != nil {
				return err
			}
			parents[node.UserID], parentBound[node.UserID] = technical, true
			sources = append(sources, legacySource{technical: technical, kind: model.TechnicalResourceBindingLegacyNode, id: strconv.FormatUint(node.ID, 10), name: displayName(node.Name, node.Hostname), health: health, k8s: node.K8SEnabled != nil && *node.K8SEnabled})
		}

		for i := range endpoints {
			endpoint := &endpoints[i]
			parent := parents[endpoint.UserID]
			if parent == nil {
				var err error
				parent, err = ensureTechnical(tx, provider.ID, model.TechnicalResourceAgent, "legacy-agent-user:"+strconv.FormatUint(endpoint.UserID, 10), nil, model.TechnicalResourcePending, model.ResourceHealthOffline, nil, endpoint.CreatedAt)
				if err != nil {
					return err
				}
				parents[endpoint.UserID] = parent
				result.OrphanEndpointParents++
			}
			state := model.TechnicalResourcePending
			if parentBound[endpoint.UserID] {
				state = model.TechnicalResourceRegistered
			}
			health := endpointHealth(endpoint)
			technical, err := ensureTechnical(tx, provider.ID, model.TechnicalResourceEndpoint, "legacy-endpoint:"+endpoint.ID, &parent.ID, state, health, timePointer(endpoint.UpdatedAt), endpoint.CreatedAt)
			if err != nil {
				return err
			}
			if parentBound[endpoint.UserID] {
				if err := ensureBinding(tx, technical, model.TechnicalResourceBindingLegacyEndpoint, endpoint.ID, actor.UserID, now); err != nil {
					return err
				}
			}
			sources = append(sources, legacySource{technical: technical, kind: model.TechnicalResourceBindingLegacyEndpoint, id: endpoint.ID, name: displayName(endpoint.Name, endpoint.Alias), health: health, k8s: endpoint.K8SAPIEnabled})
		}

		for _, source := range sources {
			if err := ensurePlatformResource(tx, &provider, actor.UserID, source, model.SupplyResourceHost, "legacy-host-"+string(source.kind)+":"+source.id, source.name, now); err != nil {
				return err
			}
			if source.k8s {
				if err := ensurePlatformResource(tx, &provider, actor.UserID, source, model.SupplyResourceKubernetes, "legacy-kubernetes-"+string(source.kind)+":"+source.id, source.name+" Kubernetes", now); err != nil {
					return err
				}
			}
		}

		detail, _ := json.Marshal(map[string]any{"provider_key": provider.Key, "agents": len(nodes), "endpoints": len(endpoints), "reason": "all legacy infrastructure is managed by Beijing Beagle"})
		return tx.Create(&model.AuditLog{
			UserID: int64(actor.UserID), UserType: "user", ActorUsername: actor.Username, ActorUserID: actor.UserID, EffectiveUserID: actor.UserID,
			ScopeType: string(model.ManagementScopeProvider), ScopeID: provider.ID, RequiredPermission: "provider.resources.write",
			ActionType: "backfill_legacy_provider_resources", TargetType: "resource_provider", TargetID: provider.ID, TargetName: provider.DisplayName,
			Detail: string(detail), CreatedAt: now,
		}).Error
	})
	if err != nil {
		return err
	}

	if err := collectReport(serverdb.DB, &result); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, append(encoded, '\n'), 0o600)
}

func ensureTechnical(tx *gorm.DB, providerID string, resourceType model.TechnicalResourceType, stableKey string, parentID *string, state model.TechnicalResourceLifecycleState, health model.ResourceHealthState, receivedAt *time.Time, createdAt time.Time) (*model.TechnicalResource, error) {
	var current model.TechnicalResource
	err := tx.Where("provider_id = ? AND type = ? AND stable_key = ?", providerID, resourceType, stableKey).First(&current).Error
	if err == nil {
		return &current, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	current = model.TechnicalResource{
		ID: stableUUID(providerID, "technical", string(resourceType), stableKey), ProviderID: providerID, Type: resourceType, StableKey: stableKey, ParentID: parentID,
		LifecycleState: state, HealthState: health, CredentialRevision: 1, LastReceivedAt: receivedAt, ConfigRevision: 1, RowVersion: 1,
		CreatedAt: createdAt.UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := tx.Create(&current).Error; err != nil {
		return nil, err
	}
	return &current, nil
}

func ensureBinding(tx *gorm.DB, technical *model.TechnicalResource, sourceType model.TechnicalResourceBindingSourceType, sourceID string, actorUserID uint64, now time.Time) error {
	var count int64
	if err := tx.Model(&model.TechnicalResourceBinding{}).Where("source_type = ? AND source_id = ?", sourceType, sourceID).Count(&count).Error; err != nil || count != 0 {
		return err
	}
	return tx.Create(&model.TechnicalResourceBinding{
		ID: stableUUID(technical.ProviderID, "binding", string(sourceType), sourceID), TechnicalResourceID: technical.ID, SourceType: sourceType, SourceID: sourceID,
		CredentialRevision: technical.CredentialRevision, Enabled: true, BoundByUserID: actorUserID, Reason: "legacy Beijing Beagle infrastructure backfill", RowVersion: 1,
		CreatedAt: now, UpdatedAt: now,
	}).Error
}

func ensurePlatformResource(tx *gorm.DB, provider *model.ResourceProvider, actorUserID uint64, source legacySource, resourceType model.SupplyResourceType, stableKey, name string, now time.Time) error {
	snapshot := map[string]any{"legacy_source_type": source.kind, "legacy_source_id": source.id, "display_name": name}
	if resourceType == model.SupplyResourceKubernetes {
		snapshot = map[string]any{"cluster_uid": stableKey, "display_name": name, "capabilities": []string{"legacy_import"}, "namespaces": []any{}}
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(payload)
	candidateID := stableUUID(provider.ID, "candidate", string(resourceType), stableKey, source.technical.ID)
	var candidate model.SupplyCandidate
	err = tx.Where("id = ?", candidateID).First(&candidate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		candidate = model.SupplyCandidate{
			ID: candidateID, ProviderID: provider.ID, TechnicalResourceID: source.technical.ID, ResourceType: resourceType, StableKey: stableKey,
			IdentityQuality: model.SupplyIdentityStrong, PayloadHash: hex.EncodeToString(hash[:]), ObservationSnapshot: string(payload),
			FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.AddDate(100, 0, 0), ReviewState: model.SupplyCandidateLinked,
			ReviewedByUserID: &actorUserID, ReviewedAt: &now, RowVersion: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&candidate).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	resourceID := stableUUID(provider.ID, "platform", string(resourceType), stableKey)
	var resource model.PlatformResource
	err = tx.Where("id = ?", resourceID).First(&resource).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		resource = model.PlatformResource{
			ID: resourceID, ProviderID: provider.ID, Type: resourceType, StableKey: stableKey, DisplayName: name,
			LifecycleState: model.PlatformResourceActive, HealthState: source.health, CapabilityRevision: 1, AllocatableScopeCount: 0, RowVersion: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&resource).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	sourceID := stableUUID(provider.ID, "platform-source", resource.ID, candidate.ID)
	var sourceCount int64
	if err := tx.Model(&model.PlatformResourceSource{}).Where("id = ?", sourceID).Count(&sourceCount).Error; err != nil {
		return err
	}
	if sourceCount == 0 {
		if err := tx.Create(&model.PlatformResourceSource{
			ID: sourceID, ProviderID: provider.ID, PlatformResourceID: resource.ID, SupplyCandidateID: candidate.ID,
			IsPrimary: true, LinkedAt: now, LastConfirmedAt: now, CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			return err
		}
	}
	if resourceType != model.SupplyResourceKubernetes {
		return nil
	}

	scopeID := stableUUID(provider.ID, "cluster-scope", resource.ID)
	var scopeCount int64
	if err := tx.Model(&model.ResourceScope{}).Where("id = ?", scopeID).Count(&scopeCount).Error; err != nil {
		return err
	}
	if scopeCount == 0 {
		return tx.Create(&model.ResourceScope{
			ID: scopeID, ProviderID: provider.ID, PlatformResourceID: resource.ID, Type: model.ResourceScopeCluster, StableKey: stableKey,
			LifecycleState: model.ResourceScopeActive, IsolationMode: model.ResourceScopeIsolationNone, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1,
			CreatedAt: now, UpdatedAt: now,
		}).Error
	}
	return nil
}

func collectReport(database *gorm.DB, result *report) error {
	queries := []struct {
		model any
		where string
		args  []any
		out   *int64
	}{
		{&model.TechnicalResource{}, "provider_id = ?", []any{result.ProviderID}, &result.TechnicalResources},
		{&model.TechnicalResourceBinding{}, "technical_resource_id IN (SELECT id FROM technical_resource WHERE provider_id = ?)", []any{result.ProviderID}, &result.TechnicalBindings},
		{&model.PlatformResource{}, "provider_id = ? AND type = ?", []any{result.ProviderID, model.SupplyResourceHost}, &result.HostResources},
		{&model.PlatformResource{}, "provider_id = ? AND type = ?", []any{result.ProviderID, model.SupplyResourceKubernetes}, &result.KubernetesResources},
		{&model.SupplyCandidate{}, "provider_id = ?", []any{result.ProviderID}, &result.SupplyCandidates},
		{&model.PlatformResourceSource{}, "provider_id = ?", []any{result.ProviderID}, &result.PlatformSources},
		{&model.ResourceScope{}, "provider_id = ? AND type = ?", []any{result.ProviderID, model.ResourceScopeCluster}, &result.ClusterScopes},
	}
	for _, query := range queries {
		if err := database.Model(query.model).Where(query.where, query.args...).Count(query.out).Error; err != nil {
			return err
		}
	}
	return database.Raw("PRAGMA integrity_check").Scan(&result.Integrity).Error
}

func stableUUID(parts ...string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join(parts, "\x00"))).String()
}

func heartbeatHealth(heartbeat *time.Time, now time.Time) model.ResourceHealthState {
	if heartbeat != nil && heartbeat.After(now.Add(-10*time.Minute)) {
		return model.ResourceHealthOnline
	}
	return model.ResourceHealthOffline
}

func endpointHealth(endpoint *model.Endpoint) model.ResourceHealthState {
	if endpoint != nil && !endpoint.Revoked && strings.EqualFold(endpoint.Status, "online") {
		return model.ResourceHealthOnline
	}
	return model.ResourceHealthOffline
}

func displayName(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fallback)
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
