package collections

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLinkedListTakeFirstClearsLastNodeWhenListBecomesEmpty(t *testing.T) {
	t.Parallel()
	list := NewLinkedList[int]()
	list.Append(1)

	require.Equal(t, 1, list.TakeFirst())
	require.Nil(t, list.firstNode)
	require.Nil(t, list.lastNode)
}
