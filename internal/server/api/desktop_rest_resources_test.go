package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

const desktopRESTResourcesDeadline = 10 * time.Second

type desktopRESTResourcesResponse struct {
	Success      bool `json:"success"`
	ContainerSSH []struct {
		ResourceID string `json:"resource_id"`
		SessionID  string `json:"session_id"`
	} `json:"container_ssh"`
	ContainerService []struct {
		ResourceID string `json:"resource_id"`
		SessionID  string `json:"session_id"`
	} `json:"container_service"`
}

func TestDesktopRESTResourcesReturnsKubernetesServiceAndPodWithinDeadline(t *testing.T) {
	router, desktopID, secret, tenantID := newDesktopRESTResourcesFixture(t)

	first, firstDuration := requestDesktopRESTResources(t, router, desktopID, secret, tenantID)
	require.True(t, first.Success)
	require.Len(t, first.ContainerService, 1)
	require.Len(t, first.ContainerSSH, 1)
	require.NotEmpty(t, first.ContainerService[0].SessionID)
	require.NotEmpty(t, first.ContainerSSH[0].SessionID)
	require.Less(t, firstDuration, desktopRESTResourcesDeadline)

	var firstSessionCount int64
	require.NoError(t, db.DB.Model(&model.ResourceSession{}).Count(&firstSessionCount).Error)
	require.Equal(t, int64(2), firstSessionCount)

	second, secondDuration := requestDesktopRESTResources(t, router, desktopID, secret, tenantID)
	require.Len(t, second.ContainerService, 1)
	require.Len(t, second.ContainerSSH, 1)
	require.Equal(t, first.ContainerService[0].SessionID, second.ContainerService[0].SessionID)
	require.Equal(t, first.ContainerSSH[0].SessionID, second.ContainerSSH[0].SessionID)
	require.Less(t, secondDuration, desktopRESTResourcesDeadline)

	var secondSessionCount int64
	require.NoError(t, db.DB.Model(&model.ResourceSession{}).Count(&secondSessionCount).Error)
	require.Equal(t, firstSessionCount, secondSessionCount)
	t.Logf("desktop REST resources: cold=%s warm=%s service=%d pod=%d sessions=%d",
		firstDuration, secondDuration, len(second.ContainerService), len(second.ContainerSSH), secondSessionCount)
}

func TestDesktopRESTResourcesHandlesProductionScaleWithinDeadline(t *testing.T) {
	const resourcesPerType = 41
	router, desktopID, secret, tenantID := newDesktopRESTResourcesFixtureWithCounts(t, resourcesPerType, resourcesPerType)

	first, firstDuration := requestDesktopRESTResources(t, router, desktopID, secret, tenantID)
	require.Len(t, first.ContainerService, resourcesPerType)
	require.Len(t, first.ContainerSSH, resourcesPerType)
	require.Less(t, firstDuration, desktopRESTResourcesDeadline)

	second, secondDuration := requestDesktopRESTResources(t, router, desktopID, secret, tenantID)
	require.Len(t, second.ContainerService, resourcesPerType)
	require.Len(t, second.ContainerSSH, resourcesPerType)
	require.Less(t, secondDuration, desktopRESTResourcesDeadline)

	var sessionCount int64
	require.NoError(t, db.DB.Model(&model.ResourceSession{}).Count(&sessionCount).Error)
	require.Equal(t, int64(resourcesPerType*2), sessionCount)
	t.Logf("desktop REST production scale: cold=%s warm=%s service=%d pod=%d sessions=%d",
		firstDuration, secondDuration, len(second.ContainerService), len(second.ContainerSSH), sessionCount)
}

func TestDesktopRESTDataUsesReadOnlyCredentialVerificationAndBatchAssembler(t *testing.T) {
	router, desktopID, secret, _ := newDesktopRESTResourcesFixture(t)
	originalHeartbeat := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	require.NoError(t, db.DB.Model(&model.Node{}).Where("id = ?", desktopID).Update("last_heartbeat", originalHeartbeat).Error)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/desktop/data", nil)
	req.Header.Set("X-Desktop-ID", fmt.Sprint(desktopID))
	req.Header.Set("X-Desktop-Secret", secret)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var response map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Contains(t, response, "services")
	require.Contains(t, response, "hosts")
	require.Contains(t, response, "devices")
	require.Contains(t, response, "favorite_service_ids")

	var stored model.Node
	require.NoError(t, db.DB.First(&stored, desktopID).Error)
	require.NotNil(t, stored.LastHeartbeat)
	require.WithinDuration(t, originalHeartbeat, *stored.LastHeartbeat, time.Millisecond)
}

func requestDesktopRESTResources(t *testing.T, router http.Handler, desktopID uint64, secret, tenantID string) (desktopRESTResourcesResponse, time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), desktopRESTResourcesDeadline)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/desktop/resources?tenant_id="+tenantID, nil).WithContext(ctx)
	req.Header.Set("X-Desktop-ID", fmt.Sprint(desktopID))
	req.Header.Set("X-Desktop-Secret", secret)
	recorder := httptest.NewRecorder()
	startedAt := time.Now()
	router.ServeHTTP(recorder, req)
	duration := time.Since(startedAt)
	require.NoError(t, ctx.Err())
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var response desktopRESTResourcesResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response, duration
}

func newDesktopRESTResourcesFixture(t *testing.T) (*gin.Engine, uint64, string, string) {
	return newDesktopRESTResourcesFixtureWithCounts(t, 1, 1)
}

func newDesktopRESTResourcesFixtureWithCounts(t *testing.T, serviceCount, sshCount int) (*gin.Engine, uint64, string, string) {
	t.Helper()
	originalDB := db.DB
	t.Cleanup(func() { db.DB = originalDB })
	require.NoError(t, db.InitDB(config.DatabaseSection{Type: "sqlite", Path: filepath.Join(t.TempDir(), "signal.db")}))
	database := db.DB
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	now := time.Now().UTC()
	secret := "desktop-rest-secret"
	secretHash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.MinCost)
	require.NoError(t, err)
	owner := model.User{ID: 9101, Name: "rest-owner", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	member := model.User{ID: 9102, Name: "rest-member", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	require.NoError(t, database.Create(&[]model.User{owner, member}).Error)
	tenant := model.Tenant{ID: uuid.NewString(), Key: "rest-resources", Name: "REST Resources", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&tenant).Error)
	require.NoError(t, database.Create(&model.TenantMembership{ID: 9201, TenantID: tenant.ID, UserID: member.ID, Role: "member", Enabled: true}).Error)
	desktop := model.Node{ID: 9301, UserID: member.ID, Name: "rest-desktop", Type: model.NodeTypeDesktop, SecretHash: string(secretHash), HeadscaleNodeID: 9401, LastHeartbeat: &now}
	agent := model.Node{ID: 9302, UserID: owner.ID, Name: "rest-agent", Type: model.NodeTypeAgent, IP: "100.64.0.32", LastHeartbeat: &now}
	require.NoError(t, database.Create(&[]model.Node{desktop, agent}).Error)

	provider := model.ResourceProvider{ID: uuid.NewString(), Key: "rest-provider", DisplayName: "REST Provider", DomainScope: model.ProviderDomainNamed, DomainLabel: "rest-provider", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&provider).Error)
	technical := model.TechnicalResource{ID: uuid.NewString(), ProviderID: provider.ID, Type: model.TechnicalResourceAgent, StableKey: "rest-agent", DomainLabel: "rest-agent", LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline, CredentialRevision: 1, ConfigRevision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&technical).Error)
	require.NoError(t, database.Create(&model.TechnicalResourceBinding{ID: uuid.NewString(), TechnicalResourceID: technical.ID, SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: fmt.Sprint(agent.ID), CredentialRevision: 1, Enabled: true, BoundByUserID: owner.ID, Reason: "REST test", RowVersion: 1}).Error)
	candidate := model.SupplyCandidate{
		ID: uuid.NewString(), ProviderID: provider.ID, TechnicalResourceID: technical.ID,
		ResourceType: model.SupplyResourceKubernetes, StableKey: "rest-cluster", IdentityQuality: model.SupplyIdentityStrong,
		PayloadHash: fmt.Sprintf("%064x", 9001), ObservationSnapshot: `{"capabilities":["workload_inventory_v1"]}`,
		FirstObservedAt: now.Add(-time.Minute), LastObservedAt: now, LeaseExpiresAt: now.Add(time.Hour),
		ReviewState: model.SupplyCandidateLinked, RowVersion: 1,
	}
	require.NoError(t, database.Create(&candidate).Error)
	platform := model.PlatformResource{ID: uuid.NewString(), ProviderID: provider.ID, Type: model.SupplyResourceKubernetes, StableKey: "rest-cluster", DisplayName: "REST Cluster", LifecycleState: model.PlatformResourceActive, HealthState: model.ResourceHealthOnline, CapabilityRevision: 1, AllocatableScopeCount: 1, RowVersion: 1}
	require.NoError(t, database.Create(&platform).Error)
	require.NoError(t, database.Create(&model.PlatformResourceSource{
		ID: uuid.NewString(), ProviderID: provider.ID, PlatformResourceID: platform.ID,
		SupplyCandidateID: candidate.ID, IsPrimary: true, LinkedAt: now, LastConfirmedAt: now,
	}).Error)
	namespace := model.NamespaceObservation{ID: uuid.NewString(), ProviderID: provider.ID, ClusterResourceID: platform.ID, NamespaceUID: "rest-namespace-uid", Name: "rest-workloads", Revision: 1, ObservedAt: now, LeaseExpiresAt: now.Add(time.Hour), State: model.NamespaceObservationObserved}
	require.NoError(t, database.Create(&namespace).Error)
	clusterScope := model.ResourceScope{ID: uuid.NewString(), ProviderID: provider.ID, PlatformResourceID: platform.ID, Type: model.ResourceScopeCluster, StableKey: "rest-cluster", LifecycleState: model.ResourceScopeActive, IsolationMode: model.ResourceScopeIsolationNone, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&clusterScope).Error)
	namespaceScope := model.ResourceScope{ID: uuid.NewString(), ProviderID: provider.ID, PlatformResourceID: platform.ID, Type: model.ResourceScopeNamespace, StableKey: "rest-namespace", ParentID: &clusterScope.ID, NamespaceObservationID: &namespace.ID, LifecycleState: model.ResourceScopeAllocatable, IsolationMode: model.ResourceScopeIsolationNamespaceIsolated, ConfigRevision: 1, EvidenceRevision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&namespaceScope).Error)
	expiresAt := now.Add(time.Hour)
	allocation := model.ResourceAllocation{ID: uuid.NewString(), TenantID: tenant.ID, Mode: model.ResourceAllocationLeased, ValidFrom: now.Add(-time.Minute), ExpiresAt: &expiresAt, State: model.ResourceAllocationDraft, RowVersion: 1, CreatedByUserID: owner.ID}
	require.NoError(t, database.Create(&allocation).Error)
	item := model.ResourceAllocationItem{ID: uuid.NewString(), AllocationID: allocation.ID, ScopeID: namespaceScope.ID, ScopeRowVersionSnapshot: 1}
	require.NoError(t, database.Create(&item).Error)
	require.NoError(t, database.Model(&model.ResourceAllocation{}).Where("id = ?", allocation.ID).Updates(map[string]any{"state": model.ResourceAllocationActive, "row_version": int64(2)}).Error)

	for i := 0; i < serviceCount; i++ {
		seedDesktopRESTResource(t, database, now, owner.ID, member.ID, tenant.ID, allocation.ID, item.ID, namespaceScope.ID, technical.ID, model.TenantResourceContainerService, i+1)
	}
	for i := 0; i < sshCount; i++ {
		seedDesktopRESTResource(t, database, now, owner.ID, member.ID, tenant.ID, allocation.ID, item.ID, namespaceScope.ID, technical.ID, model.TenantResourceContainerSSH, serviceCount+i+1)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	desktopService := grpcserver.NewDesktopServiceServer(&config.ServerConfig{FeatureFlags: config.FeatureFlagsSection{ResourceModelWrite: true}})
	runtimeStore := cache.NewNodeRuntimeStore()
	require.NoError(t, runtimeStore.LoadFromDB(context.Background(), database))
	desktopService.SetRuntimeStore(runtimeStore)
	desktopService.SetDataAssembler(service.NewDesktopDataAssembler(database, runtimeStore, nil))
	desktopAPI := NewDesktopRESTAPI(desktopService, nil)
	router.GET("/api/v1/desktop/data", desktopAPI.GetData)
	router.GET("/api/v1/desktop/resources", desktopAPI.GetResources)
	return router, desktop.ID, secret, tenant.ID
}

func seedDesktopRESTResource(t *testing.T, database *gorm.DB, now time.Time, ownerID, memberID uint64, tenantID, allocationID, itemID, namespaceScopeID, technicalID string, resourceType model.TenantResourceType, index int) {
	t.Helper()
	kind := model.WorkloadObservationServicePort
	action := "connect"
	snapshot := fmt.Sprintf(`{"namespace_uid":"rest-namespace-uid","namespace_name":"rest-workloads","service_uid":"rest-service-%d","service_name":"rest-service-%d","cluster_ip":"10.0.0.%d","port_name":"https","port_number":443,"protocol":"TCP","labels_allowlist":{}}`, index, index, index+10)
	if resourceType == model.TenantResourceContainerSSH {
		kind = model.WorkloadObservationContainer
		action = "shell"
		snapshot = fmt.Sprintf(`{"namespace_uid":"rest-namespace-uid","namespace_name":"rest-workloads","workload_uid":"rest-workload-%d","workload_kind":"Deployment","workload_name":"rest-shell-%d","pod_name":"rest-shell-%d-0","pod_uid":"rest-pod-%d","container_name":"shell","labels_allowlist":{},"ssh_users":["code"]}`, index, index, index, index)
	}
	observation := model.WorkloadObservation{
		ID: uuid.NewString(), NamespaceScopeID: namespaceScopeID, Kind: kind,
		StableKey: fmt.Sprintf("%064d", index), IdentityQuality: model.WorkloadIdentityStrong,
		State: model.WorkloadObservationEligible, Ready: true, ObservedRevision: 1, LabelSnapshot: `{}`,
		FirstObservedAt: now, LastObservedAt: now, LeaseExpiresAt: now.Add(time.Hour), RowVersion: 1,
	}
	require.NoError(t, database.Create(&observation).Error)
	require.NoError(t, database.Create(&model.WorkloadObservationSource{
		ID: uuid.NewString(), WorkloadObservationID: observation.ID, SourceTechnicalResourceID: technicalID,
		SourceEpoch: uuid.NewString(), Sequence: int64(index), PayloadHash: fmt.Sprintf("%064x", index),
		State: model.WorkloadObservationSourceObserved, Ready: true, TargetSnapshot: snapshot,
		ObservedAt: now, ReceivedAt: now, LeaseExpiresAt: now.Add(time.Hour), SourceRevision: 1, RowVersion: 1,
	}).Error)
	resource := model.TenantResource{
		ID: uuid.NewString(), TenantID: tenantID, Type: resourceType, StableKey: fmt.Sprintf("%064x", index+100),
		EntitlementLineageID: allocationID, DisplayName: fmt.Sprintf("REST resource %d", index),
		VisibilityState: model.TenantResourceVisible, AvailabilityState: model.TenantResourceAvailable, Revision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&resource).Error)
	source := model.TenantResourceSource{
		ID: uuid.NewString(), TenantResourceID: resource.ID, AllocationItemID: itemID,
		WorkloadObservationID: observation.ID, Enabled: true, EnabledAt: now, SourceRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&source).Error)
	require.NoError(t, database.Create(&model.TenantResourceTargetRevision{
		ID: uuid.NewString(), TenantResourceSourceID: source.ID, Revision: 1, TargetType: kind, TargetSnapshot: snapshot,
		SourceTechnicalResourceID: technicalID, AccessTechnicalResourceID: technicalID, Ready: true,
		ObservedAt: now, ObservationRevision: 1, SourceRevision: 1,
	}).Error)
	require.NoError(t, database.Create(&model.TenantAccessGrant{
		ID: uuid.NewString(), TenantID: tenantID, TenantResourceID: resource.ID,
		SubjectType: model.TenantAccessGrantSubjectUser, SubjectKey: fmt.Sprint(memberID), SubjectUserID: &memberID,
		Actions: fmt.Sprintf(`["%s"]`, action), ValidFrom: now.Add(-time.Minute), MaxSessionSeconds: 3600,
		Status: model.TenantAccessGrantEnabled, Revision: 1, RowVersion: 1, CreatedByUserID: ownerID,
	}).Error)
}
