package resourcebusiness

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ScenarioEmpty                    = "empty"
	ScenarioSingleProviderTenant     = "single-provider-single-tenant"
	ScenarioProviderIdentityConflict = "dual-provider-identity-conflict"
	ScenarioDualTenantScopes         = "dual-tenant-roles-namespace-scopes"
	ScenarioScopeTimeConflict        = "cluster-namespace-time-conflict"
	ScenarioAdminUserSameName        = "admin-user-same-name-distinct"
	ScenarioLegacyAmbiguity          = "legacy-admin-acl-resource-ambiguity"
	ScenarioRuntimeEvolution         = "pod-rebuild-service-ports-upstream-failure"
)

var FixedTime = time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC)

type Scenario struct {
	Name               string
	Providers          []Provider
	TechnicalResources []TechnicalResource
	SupplyInventories  []SupplyInventory
	Tenants            []Tenant
	Identities         []Identity
	Scopes             []Scope
	Workloads          []Workload
	Services           []Service
	Grants             []Grant
	Sessions           []Session
	Issues             []string
}

type Provider struct {
	ID             string
	StableIdentity string
}

type TechnicalResource struct {
	ID         string
	ProviderID string
	Type       string
	StableKey  string
	SourceType string
	SourceID   string
}

type SupplyInventory struct {
	ID                  string
	TechnicalResourceID string
	SourceEpoch         string
	Sequence            int64
	SnapshotID          string
	ClusterUID          string
	NamespaceUIDs       []string
}

type Tenant struct {
	ID  string
	Key string
}

type Identity struct {
	ID         uint64
	Kind       string
	NameToken  string
	ProofToken string
	TenantID   string
	Role       string
}

type Scope struct {
	ID         string
	ProviderID string
	TenantID   string
	Kind       string
	ParentID   string
	ClusterID  string
	Namespace  string
	StartsAt   time.Time
	EndsAt     time.Time
}

type Workload struct {
	ID             string
	ProviderID     string
	ClusterID      string
	Namespace      string
	StableKey      string
	PreviousPodUID string
	PodUID         string
	ContainerName  string
}

type Service struct {
	ID        string
	TenantID  string
	Namespace string
	Name      string
	Ports     []int
}

type Grant struct {
	ID         string
	TenantID   string
	SubjectID  uint64
	ResourceID string
	Actions    []string
	Ambiguous  bool
	LegacyRole string
	Traceable  bool
}

type Session struct {
	ID                string
	TenantID          string
	ResourceID        string
	GrantActive       bool
	SourceActive      bool
	TargetReady       bool
	ExpectedPermitted bool
	BlockReason       string
}

func Names() []string {
	return []string{
		ScenarioEmpty,
		ScenarioSingleProviderTenant,
		ScenarioProviderIdentityConflict,
		ScenarioDualTenantScopes,
		ScenarioScopeTimeConflict,
		ScenarioAdminUserSameName,
		ScenarioLegacyAmbiguity,
		ScenarioRuntimeEvolution,
	}
}

func Load(name string) (Scenario, error) {
	factory := scenarioFactories()[name]
	if factory == nil {
		return Scenario{}, fmt.Errorf("unknown resource business fixture %q", name)
	}
	scenario := factory()
	return cloneScenario(scenario)
}

func MustLoad(name string) Scenario {
	scenario, err := Load(name)
	if err != nil {
		panic(err)
	}
	return scenario
}

func Validate(scenario Scenario) error {
	if scenario.Name == "" {
		return errors.New("scenario name is required")
	}
	if !contains(Names(), scenario.Name) {
		return fmt.Errorf("scenario %q is not registered", scenario.Name)
	}
	if err := uniqueIDs("provider", providerIDs(scenario.Providers)); err != nil {
		return err
	}
	if err := uniqueIDs("tenant", tenantIDs(scenario.Tenants)); err != nil {
		return err
	}
	if err := uniqueIDs("technical resource", technicalResourceIDs(scenario.TechnicalResources)); err != nil {
		return err
	}
	if err := uniqueIDs("supply inventory", supplyInventoryIDs(scenario.SupplyInventories)); err != nil {
		return err
	}
	if err := uniqueIDs("scope", scopeIDs(scenario.Scopes)); err != nil {
		return err
	}
	if err := uniqueIdentityIDs(scenario.Identities); err != nil {
		return err
	}
	providers, tenants := stringSet(providerIDs(scenario.Providers)), stringSet(tenantIDs(scenario.Tenants))
	technicalResources := stringSet(technicalResourceIDs(scenario.TechnicalResources))
	scopes := stringSet(scopeIDs(scenario.Scopes))
	identities := uintSet(scenario.Identities)
	for _, provider := range scenario.Providers {
		if provider.StableIdentity == "" {
			return fmt.Errorf("provider %s is missing a stable identity", provider.ID)
		}
	}
	for _, tenant := range scenario.Tenants {
		if tenant.Key == "" {
			return fmt.Errorf("tenant %s is missing a key", tenant.ID)
		}
	}
	boundSources := map[string]bool{}
	for _, resource := range scenario.TechnicalResources {
		if !providers[resource.ProviderID] || (resource.Type != "agent" && resource.Type != "endpoint") || resource.StableKey == "" {
			return fmt.Errorf("technical resource %s has incomplete provider or stable identity", resource.ID)
		}
		if resource.SourceType != "legacy_node" && resource.SourceType != "legacy_endpoint" {
			return fmt.Errorf("technical resource %s has invalid source type %q", resource.ID, resource.SourceType)
		}
		sourceKey := resource.SourceType + ":" + resource.SourceID
		if resource.SourceID == "" || boundSources[sourceKey] {
			return fmt.Errorf("technical resource %s has an empty or duplicated source", resource.ID)
		}
		boundSources[sourceKey] = true
	}
	for _, inventory := range scenario.SupplyInventories {
		if !technicalResources[inventory.TechnicalResourceID] || inventory.SourceEpoch == "" || inventory.Sequence <= 0 || inventory.SnapshotID == "" || inventory.ClusterUID == "" {
			return fmt.Errorf("supply inventory %s has incomplete source or cluster identity", inventory.ID)
		}
		seenNamespaceUIDs := map[string]bool{}
		for _, namespaceUID := range inventory.NamespaceUIDs {
			if namespaceUID == "" || seenNamespaceUIDs[namespaceUID] {
				return fmt.Errorf("supply inventory %s has an empty or duplicated namespace UID", inventory.ID)
			}
			seenNamespaceUIDs[namespaceUID] = true
		}
	}
	for _, scope := range scenario.Scopes {
		if !providers[scope.ProviderID] || !tenants[scope.TenantID] {
			return fmt.Errorf("scope %s has an unknown provider or tenant", scope.ID)
		}
		if scope.StartsAt.Location() != time.UTC || scope.EndsAt.Location() != time.UTC || !scope.StartsAt.Before(scope.EndsAt) {
			return fmt.Errorf("scope %s has an invalid UTC interval", scope.ID)
		}
		if scope.Kind != "cluster" && scope.Kind != "namespace" {
			return fmt.Errorf("scope %s has invalid kind %q", scope.ID, scope.Kind)
		}
		if scope.ParentID != "" && !scopes[scope.ParentID] {
			return fmt.Errorf("scope %s has an unknown parent", scope.ID)
		}
	}
	for _, identity := range scenario.Identities {
		if identity.TenantID != "" && !tenants[identity.TenantID] {
			return fmt.Errorf("identity %d has an unknown tenant", identity.ID)
		}
		if identity.NameToken == "" || identity.ProofToken == "" {
			return fmt.Errorf("identity %d is missing anonymous evidence", identity.ID)
		}
	}
	for _, workload := range scenario.Workloads {
		if !providers[workload.ProviderID] || workload.StableKey == "" || workload.PodUID == "" || workload.ContainerName == "" {
			return fmt.Errorf("workload %s has incomplete provider or runtime identity", workload.ID)
		}
	}
	for _, service := range scenario.Services {
		if !tenants[service.TenantID] || service.ID == "" || service.Namespace == "" || service.Name == "" {
			return fmt.Errorf("service %s has incomplete tenant or service identity", service.ID)
		}
		seen := map[int]bool{}
		for _, port := range service.Ports {
			if port < 1 || port > 65535 || seen[port] {
				return fmt.Errorf("service %s has an invalid or duplicate port", service.ID)
			}
			seen[port] = true
		}
	}
	for _, grant := range scenario.Grants {
		if !identities[grant.SubjectID] || !tenants[grant.TenantID] || grant.ResourceID == "" || len(grant.Actions) == 0 {
			return fmt.Errorf("grant %s has an unknown subject or tenant", grant.ID)
		}
	}
	for _, session := range scenario.Sessions {
		permitted := session.GrantActive && session.SourceActive && session.TargetReady
		if permitted != session.ExpectedPermitted {
			return fmt.Errorf("session %s permission expectation does not match upstream state", session.ID)
		}
		if !permitted && session.BlockReason == "" {
			return fmt.Errorf("session %s has no block reason", session.ID)
		}
	}
	return nil
}

func cloneScenario(source Scenario) (Scenario, error) {
	encoded, err := json.Marshal(source)
	if err != nil {
		return Scenario{}, err
	}
	var result Scenario
	if err := json.Unmarshal(encoded, &result); err != nil {
		return Scenario{}, err
	}
	return result, nil
}

func uniqueIDs(kind string, values []string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || seen[value] {
			return fmt.Errorf("%s ID %q is empty or duplicated", kind, value)
		}
		seen[value] = true
	}
	return nil
}

func uniqueIdentityIDs(values []Identity) error {
	seen := map[uint64]bool{}
	for _, value := range values {
		if value.ID == 0 || seen[value.ID] {
			return fmt.Errorf("identity ID %d is empty or duplicated", value.ID)
		}
		seen[value.ID] = true
	}
	return nil
}

func providerIDs(values []Provider) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.ID)
	}
	return result
}

func tenantIDs(values []Tenant) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.ID)
	}
	return result
}

func technicalResourceIDs(values []TechnicalResource) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.ID)
	}
	return result
}

func supplyInventoryIDs(values []SupplyInventory) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.ID)
	}
	return result
}

func scopeIDs(values []Scope) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.ID)
	}
	return result
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func uintSet(values []Identity) map[uint64]bool {
	result := make(map[uint64]bool, len(values))
	for _, value := range values {
		result[value.ID] = true
	}
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func HasIssue(scenario Scenario, issue string) bool {
	for _, candidate := range scenario.Issues {
		if candidate == issue {
			return true
		}
	}
	return false
}

func ContainsSensitiveFixtureText(scenario Scenario) bool {
	encoded, _ := json.Marshal(scenario)
	lower := strings.ToLower(string(encoded))
	for _, marker := range []string{"password", "secret", "bearer ", "private key", "authorization"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
