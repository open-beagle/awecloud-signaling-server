package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestProviderSupplyQueriesAreProviderScoped(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	accepted := createLifecycleSupplyResource(t, fixture)
	otherAuthorization := createOtherProviderAuthorization(t, fixture)
	otherResource := createBoundAgentForProvider(t, fixture, otherAuthorization, "agent-other-query", 1002)

	technical, err := fixture.service.ListTechnicalResources(context.Background(), fixture.authorization, ProviderSupplyListInput{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, technical.Items, 1)
	require.Equal(t, accepted.Candidate.TechnicalResourceID, technical.Items[0].ID)

	otherTechnical, err := fixture.service.ListTechnicalResources(context.Background(), otherAuthorization, ProviderSupplyListInput{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, otherTechnical.Items, 1)
	require.Equal(t, otherResource.ID, otherTechnical.Items[0].ID)

	_, err = fixture.service.GetTechnicalResource(context.Background(), fixture.authorization, otherResource.ID)
	require.ErrorIs(t, err, ErrProviderSupplyObjectNotFound)

	candidates, err := fixture.service.ListSupplyCandidates(context.Background(), fixture.authorization, ProviderSupplyListInput{
		Type: string(model.SupplyResourceKubernetes), State: string(model.SupplyCandidateLinked), Page: 1, PageSize: 10,
	})
	require.NoError(t, err)
	require.Len(t, candidates.Items, 1)
	require.Equal(t, accepted.Candidate.ID, candidates.Items[0].ID)

	resources, err := fixture.service.ListPlatformResources(context.Background(), fixture.authorization, ProviderSupplyListInput{
		Search: "Lifecycle", State: string(model.PlatformResourceDraft), Page: 1, PageSize: 10,
	})
	require.NoError(t, err)
	require.Len(t, resources.Items, 1)
	require.Equal(t, accepted.Resource.ID, resources.Items[0].ID)

	detail, err := fixture.service.GetPlatformResource(context.Background(), fixture.authorization, accepted.Resource.ID)
	require.NoError(t, err)
	require.Len(t, detail.Sources, 1)
	require.Len(t, detail.Scopes, 3)

	scopes, err := fixture.service.ListResourceScopes(context.Background(), fixture.authorization, ResourceScopeListInput{
		ProviderSupplyListInput: ProviderSupplyListInput{Type: string(model.ResourceScopeNamespace), Page: 1, PageSize: 1},
		PlatformResourceID:      accepted.Resource.ID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), scopes.Total)
	require.Len(t, scopes.Items, 1)
	require.Equal(t, model.ResourceScopeNamespace, scopes.Items[0].Type)

	scopeDetail, err := fixture.service.GetResourceScope(context.Background(), fixture.authorization, accepted.NamespaceScopes[0].ID)
	require.NoError(t, err)
	require.NotNil(t, scopeDetail.Observation)
	require.Equal(t, *accepted.NamespaceScopes[0].NamespaceObservationID, scopeDetail.Observation.ID)

	_, err = fixture.service.GetPlatformResource(context.Background(), otherAuthorization, accepted.Resource.ID)
	require.ErrorIs(t, err, ErrProviderSupplyObjectNotFound)
	_, err = fixture.service.ListResourceScopes(context.Background(), otherAuthorization, ResourceScopeListInput{
		ProviderSupplyListInput: ProviderSupplyListInput{Page: 1, PageSize: 10}, PlatformResourceID: accepted.Resource.ID,
	})
	require.ErrorIs(t, err, ErrProviderSupplyObjectNotFound)
}

func TestProviderSupplyQueriesValidateFiltersAndReauthorize(t *testing.T) {
	fixture := newProviderSupplyFixture(t)
	createLifecycleSupplyResource(t, fixture)

	_, err := fixture.service.ListPlatformResources(context.Background(), fixture.authorization, ProviderSupplyListInput{State: "unknown", Page: 1, PageSize: 10})
	require.ErrorIs(t, err, ErrProviderSupplyInvalidInput)
	_, err = fixture.service.ListTechnicalResources(context.Background(), fixture.authorization, ProviderSupplyListInput{Page: 1, PageSize: 101})
	require.ErrorIs(t, err, ErrProviderSupplyInvalidInput)

	stale := *fixture.authorization
	stale.PermissionRevision++
	_, err = fixture.service.ListSupplyCandidates(context.Background(), &stale, ProviderSupplyListInput{Page: 1, PageSize: 10})
	require.ErrorIs(t, err, ErrManagementPermissionDenied)
}
