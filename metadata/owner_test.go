package metadata

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type TestOwner struct {
	metadata Metadata
}

func (o *TestOwner) GetMetadata() Metadata {
	return o.metadata
}

func TestOwnerInterface(t *testing.T) {
	testMetadata := Metadata{"key1": "value1", "key2": "value2"}
	owner := &TestOwner{metadata: testMetadata}
	
	var _ Owner = owner
	
	metadata := owner.GetMetadata()
	require.Equal(t, testMetadata, metadata, "GetMetadata devrait retourner les métadonnées correctes")
}
