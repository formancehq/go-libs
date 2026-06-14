package temporal

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/operatorservice/v1"
	temporalmocks "go.temporal.io/sdk/mocks"
	"google.golang.org/grpc"
)

func TestCreateSearchAttributesReturnsListSearchAttributesError(t *testing.T) {
	t.Parallel()

	operator := &fakeOperatorService{
		listErr: errors.New("temporarily unavailable"),
	}
	c := &temporalmocks.Client{}
	c.On("OperatorService").Return(operator)

	err := CreateSearchAttributes(context.Background(), c, "default", map[string]enums.IndexedValueType{
		"CustomKeyword": enums.INDEXED_VALUE_TYPE_KEYWORD,
	})

	require.ErrorContains(t, err, "list temporal search attributes")
	require.ErrorContains(t, err, "temporarily unavailable")
}

type fakeOperatorService struct {
	addErr  error
	listErr error
	listRsp *operatorservice.ListSearchAttributesResponse
}

func (f *fakeOperatorService) AddSearchAttributes(context.Context, *operatorservice.AddSearchAttributesRequest, ...grpc.CallOption) (*operatorservice.AddSearchAttributesResponse, error) {
	return &operatorservice.AddSearchAttributesResponse{}, f.addErr
}

func (f *fakeOperatorService) RemoveSearchAttributes(context.Context, *operatorservice.RemoveSearchAttributesRequest, ...grpc.CallOption) (*operatorservice.RemoveSearchAttributesResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeOperatorService) ListSearchAttributes(context.Context, *operatorservice.ListSearchAttributesRequest, ...grpc.CallOption) (*operatorservice.ListSearchAttributesResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listRsp, nil
}

func (f *fakeOperatorService) DeleteNamespace(context.Context, *operatorservice.DeleteNamespaceRequest, ...grpc.CallOption) (*operatorservice.DeleteNamespaceResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeOperatorService) AddOrUpdateRemoteCluster(context.Context, *operatorservice.AddOrUpdateRemoteClusterRequest, ...grpc.CallOption) (*operatorservice.AddOrUpdateRemoteClusterResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeOperatorService) RemoveRemoteCluster(context.Context, *operatorservice.RemoveRemoteClusterRequest, ...grpc.CallOption) (*operatorservice.RemoveRemoteClusterResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeOperatorService) ListClusters(context.Context, *operatorservice.ListClustersRequest, ...grpc.CallOption) (*operatorservice.ListClustersResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeOperatorService) GetNexusEndpoint(context.Context, *operatorservice.GetNexusEndpointRequest, ...grpc.CallOption) (*operatorservice.GetNexusEndpointResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeOperatorService) CreateNexusEndpoint(context.Context, *operatorservice.CreateNexusEndpointRequest, ...grpc.CallOption) (*operatorservice.CreateNexusEndpointResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeOperatorService) UpdateNexusEndpoint(context.Context, *operatorservice.UpdateNexusEndpointRequest, ...grpc.CallOption) (*operatorservice.UpdateNexusEndpointResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeOperatorService) DeleteNexusEndpoint(context.Context, *operatorservice.DeleteNexusEndpointRequest, ...grpc.CallOption) (*operatorservice.DeleteNexusEndpointResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeOperatorService) ListNexusEndpoints(context.Context, *operatorservice.ListNexusEndpointsRequest, ...grpc.CallOption) (*operatorservice.ListNexusEndpointsResponse, error) {
	return nil, errors.New("not implemented")
}
