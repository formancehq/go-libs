package temporal

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/converter"
)

func TestEncryption(t *testing.T) {

	type Payload struct {
		Foo string `json:"foo"`
	}

	t.Run("wrong key", func(t *testing.T) {
		_, err := NewEncryptionDataConverter([]byte("1234567890"))
		require.Error(t, err)
	})

	t.Run("encryption payloads", func(t *testing.T) {
		converter, err := NewEncryptionDataConverter([]byte("12345678901011121314151617181920"))
		require.NoError(t, err)

		payload := []Payload{
			{
				Foo: "bar",
			},
			{
				Foo: "baz",
			},
		}
		p, err := converter.ToPayloads(&payload[0], &payload[1])
		require.NoError(t, err)
		require.NotNil(t, p)

		result := make([]Payload, 2)
		err = converter.FromPayloads(p, &result[0], &result[1])
		require.NoError(t, err)
		require.Equal(t, payload[0].Foo, result[0].Foo)
		require.Equal(t, payload[1].Foo, result[1].Foo)
	})

	t.Run("decrypt truncated ciphertext", func(t *testing.T) {
		converter, err := NewEncryptionDataConverter([]byte("12345678901011121314151617181920"))
		require.NoError(t, err)

		payload := Payload{
			Foo: "bar",
		}
		p, err := converter.ToPayload(&payload)
		require.NoError(t, err)
		require.NotNil(t, p)

		// Truncate the encrypted data to less than the GCM nonce size
		p.Data = []byte(base64.StdEncoding.EncodeToString([]byte("short")))

		var result Payload
		err = converter.FromPayload(p, &result)
		require.Error(t, err)
		require.ErrorContains(t, err, "ciphertext too short")
	})

	t.Run("decrypt if metadata is not present", func(t *testing.T) {
		defaultConverter := converter.GetDefaultDataConverter()

		payload := []Payload{
			{
				Foo: "bar",
			},
			{
				Foo: "baz",
			},
		}
		p, err := defaultConverter.ToPayloads(&payload[0], &payload[1])
		require.NoError(t, err)
		require.NotNil(t, p)

		converter, err := NewEncryptionDataConverter([]byte("12345678901011121314151617181920"))
		require.NoError(t, err)
		result := make([]Payload, 2)
		err = converter.FromPayloads(p, &result[0], &result[1])
		require.NoError(t, err)
		require.Equal(t, payload[0].Foo, result[0].Foo)
		require.Equal(t, payload[1].Foo, result[1].Foo)
	})
}
