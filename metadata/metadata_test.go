package metadata_test

import (
	"testing"

	"github.com/formancehq/go-libs/v2/metadata"
	"github.com/stretchr/testify/require"
)

func TestMetadataIsEquivalentTo(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		m1       metadata.Metadata
		m2       metadata.Metadata
		expected bool
	}{
		{
			name:     "empty metadata",
			m1:       metadata.Metadata{},
			m2:       metadata.Metadata{},
			expected: true,
		},
		{
			name:     "same metadata",
			m1:       metadata.Metadata{"key1": "value1", "key2": "value2"},
			m2:       metadata.Metadata{"key1": "value1", "key2": "value2"},
			expected: true,
		},
		{
			name:     "different values",
			m1:       metadata.Metadata{"key1": "value1", "key2": "value2"},
			m2:       metadata.Metadata{"key1": "value1", "key2": "different"},
			expected: false,
		},
		{
			name:     "different keys",
			m1:       metadata.Metadata{"key1": "value1", "key2": "value2"},
			m2:       metadata.Metadata{"key1": "value1", "key3": "value2"},
			expected: false,
		},
		{
			name:     "subset",
			m1:       metadata.Metadata{"key1": "value1", "key2": "value2"},
			m2:       metadata.Metadata{"key1": "value1"},
			expected: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := tc.m1.IsEquivalentTo(tc.m2)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestMetadataMerge(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		m1       metadata.Metadata
		m2       metadata.Metadata
		expected metadata.Metadata
	}{
		{
			name:     "empty metadata",
			m1:       metadata.Metadata{},
			m2:       metadata.Metadata{},
			expected: metadata.Metadata{},
		},
		{
			name:     "merge with empty",
			m1:       metadata.Metadata{"key1": "value1", "key2": "value2"},
			m2:       metadata.Metadata{},
			expected: metadata.Metadata{"key1": "value1", "key2": "value2"},
		},
		{
			name:     "merge empty with non-empty",
			m1:       metadata.Metadata{},
			m2:       metadata.Metadata{"key1": "value1", "key2": "value2"},
			expected: metadata.Metadata{"key1": "value1", "key2": "value2"},
		},
		{
			name:     "merge with override",
			m1:       metadata.Metadata{"key1": "value1", "key2": "value2"},
			m2:       metadata.Metadata{"key2": "new_value", "key3": "value3"},
			expected: metadata.Metadata{"key1": "value1", "key2": "new_value", "key3": "value3"},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := tc.m1.Merge(tc.m2)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestMetadataScan(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name        string
		input       interface{}
		expected    metadata.Metadata
		expectError bool
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: metadata.Metadata(nil),
		},
		{
			name:     "string input",
			input:    `{"key1":"value1","key2":"value2"}`,
			expected: metadata.Metadata{"key1": "value1", "key2": "value2"},
		},
		{
			name:     "bytes input",
			input:    []byte(`{"key1":"value1","key2":"value2"}`),
			expected: metadata.Metadata{"key1": "value1", "key2": "value2"},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var m metadata.Metadata
			err := m.Scan(tc.input)
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, m)
			}
		})
	}
}

func TestMetadataCopy(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		original metadata.Metadata
	}{
		{
			name:     "empty metadata",
			original: metadata.Metadata{},
		},
		{
			name:     "non-empty metadata",
			original: metadata.Metadata{"key1": "value1", "key2": "value2"},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			copy := tc.original.Copy()
			require.Equal(t, tc.original, copy)

			// Verify it's a deep copy by modifying the copy
			if len(copy) > 0 {
				for k := range copy {
					copy[k] = "modified"
					break
				}
				require.NotEqual(t, tc.original, copy)
			}
		})
	}
}

func TestComputeMetadata(t *testing.T) {
	t.Parallel()
	key := "testKey"
	value := "testValue"
	m := metadata.ComputeMetadata(key, value)
	require.Equal(t, metadata.Metadata{key: value}, m)
}

func TestMarshalValue(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "string",
			input:    "test",
			expected: `"test"`,
		},
		{
			name:     "number",
			input:    123,
			expected: `123`,
		},
		{
			name:     "bool",
			input:    true,
			expected: `true`,
		},
		{
			name:     "struct",
			input:    struct{ Name string }{"test"},
			expected: `{"Name":"test"}`,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := metadata.MarshalValue(tc.input)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestUnmarshalValue(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    string
		expected interface{}
	}{
		{
			name:     "string",
			input:    `"test"`,
			expected: "test",
		},
		{
			name:     "number",
			input:    `123`,
			expected: float64(123),
		},
		{
			name:     "bool",
			input:    `true`,
			expected: true,
		},
		{
			name:     "struct",
			input:    `{"Name":"test"}`,
			expected: map[string]interface{}{"Name": "test"},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var result interface{}
			switch tc.expected.(type) {
			case string:
				result = metadata.UnmarshalValue[string](tc.input)
			case float64:
				result = metadata.UnmarshalValue[float64](tc.input)
			case bool:
				result = metadata.UnmarshalValue[bool](tc.input)
			case map[string]interface{}:
				result = metadata.UnmarshalValue[map[string]interface{}](tc.input)
			}
			require.Equal(t, tc.expected, result)
		})
	}
}
