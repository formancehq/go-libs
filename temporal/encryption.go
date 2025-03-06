package temporal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	"go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
)

type EncryptionDataConverter struct {
	converter.DataConverter
	key []byte
}

func NewEncryptionDataConverter(key []byte) *EncryptionDataConverter {
	return &EncryptionDataConverter{
		DataConverter: converter.GetDefaultDataConverter(),
		key:           key,
	}
}

func (c *EncryptionDataConverter) ToPayload(value interface{}) (*common.Payload, error) {
	payload, err := c.DataConverter.ToPayload(value)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, payload.Data, nil)
	payload.Data = []byte(base64.StdEncoding.EncodeToString(ciphertext))

	return payload, nil
}

func (c *EncryptionDataConverter) FromPayload(payload *common.Payload, valuePtr interface{}) error {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	ciphertext, err := base64.StdEncoding.DecodeString(string(payload.Data))
	if err != nil {
		return err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return err
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return err
	}

	payload.Data = plaintext
	return c.DataConverter.FromPayload(payload, valuePtr)
}

func (dc *EncryptionDataConverter) ToPayloads(values ...interface{}) (*common.Payloads, error) {
	if len(values) == 0 {
		return nil, nil
	}

	result := &common.Payloads{}
	for i, value := range values {
		payload, err := dc.ToPayload(value)
		if err != nil {
			return nil, fmt.Errorf("values[%d]: %w", i, err)
		}

		result.Payloads = append(result.Payloads, payload)
	}

	return result, nil
}

// FromPayloads converts to a list of values of different types.
func (dc *EncryptionDataConverter) FromPayloads(payloads *common.Payloads, valuePtrs ...interface{}) error {
	if payloads == nil {
		return nil
	}

	for i, payload := range payloads.GetPayloads() {
		if i >= len(valuePtrs) {
			break
		}

		err := dc.FromPayload(payload, valuePtrs[i])
		if err != nil {
			return fmt.Errorf("payload item %d: %w", i, err)
		}
	}

	return nil
}
