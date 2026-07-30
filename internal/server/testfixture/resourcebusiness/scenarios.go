package resourcebusiness

import "time"

func scenarioFactories() map[string]func() Scenario {
	return map[string]func() Scenario{
		ScenarioEmpty:                    emptyScenario,
		ScenarioSingleProviderTenant:     singleProviderTenantScenario,
		ScenarioProviderIdentityConflict: providerIdentityConflictScenario,
		ScenarioDualTenantScopes:         dualTenantScopesScenario,
		ScenarioScopeTimeConflict:        scopeTimeConflictScenario,
		ScenarioAdminUserSameName:        adminUserSameNameScenario,
		ScenarioLegacyAmbiguity:          legacyAmbiguityScenario,
		ScenarioRuntimeEvolution:         runtimeEvolutionScenario,
	}
}

func emptyScenario() Scenario {
	return Scenario{Name: ScenarioEmpty}
}

func singleProviderTenantScenario() Scenario {
	return Scenario{
		Name:      ScenarioSingleProviderTenant,
		Providers: []Provider{{ID: "provider-a", StableIdentity: "provider-key-a"}},
		TechnicalResources: []TechnicalResource{{
			ID: "technical-agent-a", ProviderID: "provider-a", Type: "agent", StableKey: "agent-stable-a",
			SourceType: "legacy_node", SourceID: "fixture-node-a",
		}},
		SupplyInventories: []SupplyInventory{{
			ID: "inventory-a", TechnicalResourceID: "technical-agent-a", SourceEpoch: "epoch-a", Sequence: 1,
			SnapshotID: "snapshot-a", ClusterUID: "cluster-uid-a", NamespaceUIDs: []string{"namespace-uid-a"},
		}},
		Tenants: []Tenant{{ID: "tenant-a", Key: "tenant-key-a"}},
		Identities: []Identity{
			{ID: 1001, Kind: "agent", NameToken: "agent-a", ProofToken: "agent-proof-a"},
			{ID: 2001, Kind: "user", NameToken: "member-a", ProofToken: "user-proof-a", TenantID: "tenant-a", Role: "member"},
		},
		Scopes:    []Scope{{ID: "scope-a", ProviderID: "provider-a", TenantID: "tenant-a", Kind: "namespace", ClusterID: "cluster-a", Namespace: "namespace-a", StartsAt: FixedTime, EndsAt: FixedTime.Add(24 * time.Hour)}},
		Workloads: []Workload{{ID: "workload-a", ProviderID: "provider-a", ClusterID: "cluster-a", Namespace: "namespace-a", StableKey: "cluster-a/namespace-a/deployment-a/container-a", PodUID: "pod-a-1", ContainerName: "container-a"}},
		Services:  []Service{{ID: "service-a", TenantID: "tenant-a", Namespace: "namespace-a", Name: "service-a", Ports: []int{443}}},
		Grants:    []Grant{{ID: "grant-a", TenantID: "tenant-a", SubjectID: 2001, ResourceID: "resource-a", Actions: []string{"shell"}, Traceable: true}},
		Sessions:  []Session{{ID: "session-a", TenantID: "tenant-a", ResourceID: "resource-a", GrantActive: true, SourceActive: true, TargetReady: true, ExpectedPermitted: true}},
	}
}

func providerIdentityConflictScenario() Scenario {
	scenario := singleProviderTenantScenario()
	scenario.Name = ScenarioProviderIdentityConflict
	scenario.Providers = append(scenario.Providers, Provider{ID: "provider-b", StableIdentity: "provider-key-a"})
	scenario.TechnicalResources = append(scenario.TechnicalResources, TechnicalResource{
		ID: "technical-agent-b", ProviderID: "provider-b", Type: "agent", StableKey: "agent-stable-b",
		SourceType: "legacy_node", SourceID: "fixture-node-b",
	})
	scenario.SupplyInventories = append(scenario.SupplyInventories, SupplyInventory{
		ID: "inventory-b", TechnicalResourceID: "technical-agent-b", SourceEpoch: "epoch-b", Sequence: 1,
		SnapshotID: "snapshot-b", ClusterUID: "cluster-uid-a", NamespaceUIDs: []string{"namespace-uid-b"},
	})
	scenario.Issues = []string{"PROVIDER_STABLE_IDENTITY_CONFLICT", "CROSS_PROVIDER_SUPPLY_IDENTITY_CONFLICT"}
	return scenario
}

func dualTenantScopesScenario() Scenario {
	scenario := singleProviderTenantScenario()
	scenario.Name = ScenarioDualTenantScopes
	scenario.Tenants = append(scenario.Tenants, Tenant{ID: "tenant-b", Key: "tenant-key-b"})
	scenario.Identities = append(scenario.Identities,
		Identity{ID: 2002, Kind: "user", NameToken: "member-b", ProofToken: "user-proof-b", TenantID: "tenant-b", Role: "tenant_admin"},
	)
	scenario.Scopes = append(scenario.Scopes,
		Scope{ID: "scope-b", ProviderID: "provider-a", TenantID: "tenant-b", Kind: "namespace", ClusterID: "cluster-a", Namespace: "namespace-b", StartsAt: FixedTime, EndsAt: FixedTime.Add(24 * time.Hour)},
	)
	scenario.Issues = []string{"TENANT_ROLE_DIFFERENCE", "SIBLING_NAMESPACE_SCOPES"}
	return scenario
}

func scopeTimeConflictScenario() Scenario {
	scenario := dualTenantScopesScenario()
	scenario.Name = ScenarioScopeTimeConflict
	scenario.Scopes = []Scope{
		{ID: "scope-cluster", ProviderID: "provider-a", TenantID: "tenant-a", Kind: "cluster", ClusterID: "cluster-a", StartsAt: FixedTime, EndsAt: FixedTime.Add(48 * time.Hour)},
		{ID: "scope-namespace", ProviderID: "provider-a", TenantID: "tenant-b", Kind: "namespace", ParentID: "scope-cluster", ClusterID: "cluster-a", Namespace: "namespace-b", StartsAt: FixedTime.Add(time.Hour), EndsAt: FixedTime.Add(12 * time.Hour)},
	}
	scenario.Issues = []string{"ANCESTOR_DESCENDANT_SCOPE_TIME_CONFLICT"}
	return scenario
}

func adminUserSameNameScenario() Scenario {
	return Scenario{
		Name:    ScenarioAdminUserSameName,
		Tenants: []Tenant{{ID: "tenant-a", Key: "tenant-key-a"}},
		Identities: []Identity{
			{ID: 3001, Kind: "admin", NameToken: "shared-login", ProofToken: "admin-proof-a", Role: "platform_viewer"},
			{ID: 2001, Kind: "user", NameToken: "shared-login", ProofToken: "user-proof-a", TenantID: "tenant-a", Role: "member"},
		},
		Issues: []string{"ADMIN_USER_SAME_NAME_DISTINCT_PROOF"},
	}
}

func legacyAmbiguityScenario() Scenario {
	scenario := singleProviderTenantScenario()
	scenario.Name = ScenarioLegacyAmbiguity
	scenario.Identities[1].Role = "tenant_admin"
	scenario.Grants = []Grant{{ID: "legacy-grant-a", TenantID: "tenant-a", SubjectID: 2001, ResourceID: "untraceable-resource", Actions: []string{"legacy"}, Ambiguous: true, LegacyRole: "tenant_admin", Traceable: false}}
	scenario.Issues = []string{"LEGACY_TENANT_ADMIN_SEMANTICS", "ACL_ACTION_AMBIGUOUS", "RESOURCE_SOURCE_UNTRACEABLE"}
	return scenario
}

func runtimeEvolutionScenario() Scenario {
	scenario := singleProviderTenantScenario()
	scenario.Name = ScenarioRuntimeEvolution
	scenario.Workloads[0].PreviousPodUID = "pod-a-1"
	scenario.Workloads[0].PodUID = "pod-a-2"
	scenario.Services[0].Ports = []int{443, 8443, 9443}
	scenario.Sessions = []Session{
		{ID: "session-source-stopped", TenantID: "tenant-a", ResourceID: "resource-a", GrantActive: true, SourceActive: false, TargetReady: true, ExpectedPermitted: false, BlockReason: "SOURCE_INACTIVE"},
		{ID: "session-target-stale", TenantID: "tenant-a", ResourceID: "resource-a", GrantActive: true, SourceActive: true, TargetReady: false, ExpectedPermitted: false, BlockReason: "TARGET_NOT_READY"},
	}
	scenario.Issues = []string{"POD_REBUILT_STABLE_WORKLOAD", "SERVICE_MULTI_PORT", "UPSTREAM_SESSION_BLOCK"}
	return scenario
}
