package metadata

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsEquivalentTo(t *testing.T) {
	m1 := Metadata{"key1": "value1", "key2": "value2"}
	m2 := Metadata{"key1": "value1", "key2": "value2"}
	m3 := Metadata{"key1": "value1", "key3": "value3"}

	require.True(t, m1.IsEquivalentTo(m2), "Les métadonnées identiques devraient être équivalentes")
	require.False(t, m1.IsEquivalentTo(m3), "Les métadonnées différentes ne devraient pas être équivalentes")
}

func TestMerge(t *testing.T) {
	m1 := Metadata{"key1": "value1", "key2": "value2"}
	m2 := Metadata{"key2": "newvalue2", "key3": "value3"}

	merged := m1.Merge(m2)

	require.Equal(t, "value1", merged["key1"], "La clé key1 devrait être conservée")
	require.Equal(t, "newvalue2", merged["key2"], "La clé key2 devrait être écrasée")
	require.Equal(t, "value3", merged["key3"], "La clé key3 devrait être ajoutée")
}

func TestScan(t *testing.T) {
	var m1 Metadata
	err := m1.Scan(nil)
	require.NoError(t, err, "Le scan d'une valeur nil ne devrait pas échouer")

	jsonData := []byte(`{"key1":"value1","key2":"value2"}`)
	var m2 Metadata
	err = m2.Scan(jsonData)
	require.NoError(t, err, "Le scan d'un tableau d'octets ne devrait pas échouer")
	require.Equal(t, "value1", m2["key1"], "La clé key1 devrait être correctement décodée")
	require.Equal(t, "value2", m2["key2"], "La clé key2 devrait être correctement décodée")

	var m3 Metadata
	err = m3.Scan(`{"key1":"value1","key2":"value2"}`)
	require.NoError(t, err, "Le scan d'une chaîne de caractères ne devrait pas échouer")
	require.Equal(t, "value1", m3["key1"], "La clé key1 devrait être correctement décodée")
	require.Equal(t, "value2", m3["key2"], "La clé key2 devrait être correctement décodée")
}

func TestConvertValue(t *testing.T) {
	m := Metadata{"key1": "value1", "key2": "value2"}

	value, err := m.ConvertValue(m)
	require.NoError(t, err, "La conversion ne devrait pas échouer")

	var decoded Metadata
	err = json.Unmarshal(value.([]byte), &decoded)
	require.NoError(t, err, "La valeur convertie devrait être un JSON valide")
	require.Equal(t, m, decoded, "La valeur décodée devrait correspondre à l'originale")
}

func TestCopy(t *testing.T) {
	m1 := Metadata{"key1": "value1", "key2": "value2"}

	m2 := m1.Copy()

	require.Equal(t, m1, m2, "La copie devrait être identique à l'original")

	m2["key3"] = "value3"
	require.NotEqual(t, m1, m2, "La modification de la copie ne devrait pas affecter l'original")
	require.NotContains(t, m1, "key3", "L'original ne devrait pas contenir la nouvelle clé")
}

func TestComputeMetadata(t *testing.T) {
	m := ComputeMetadata("key", "value")

	require.Len(t, m, 1, "Les métadonnées devraient contenir une seule entrée")
	require.Equal(t, "value", m["key"], "La valeur de la clé devrait être correcte")
}

func TestMarshalValue(t *testing.T) {
	value := "test"
	marshaled := MarshalValue(value)
	require.Equal(t, `"test"`, marshaled, "La valeur marshaled devrait être correcte")

	value2 := 42
	marshaled2 := MarshalValue(value2)
	require.Equal(t, "42", marshaled2, "La valeur marshaled devrait être correcte")

	value3 := map[string]string{"key": "value"}
	marshaled3 := MarshalValue(value3)
	require.Equal(t, `{"key":"value"}`, marshaled3, "La valeur marshaled devrait être correcte")
}

func TestUnmarshalValue(t *testing.T) {
	value := `"test"`
	unmarshaled := UnmarshalValue[string](value)
	require.Equal(t, "test", unmarshaled, "La valeur unmarshaled devrait être correcte")

	value2 := "42"
	unmarshaled2 := UnmarshalValue[int](value2)
	require.Equal(t, 42, unmarshaled2, "La valeur unmarshaled devrait être correcte")

	value3 := `{"key":"value"}`
	unmarshaled3 := UnmarshalValue[map[string]string](value3)
	require.Equal(t, map[string]string{"key": "value"}, unmarshaled3, "La valeur unmarshaled devrait être correcte")
}
