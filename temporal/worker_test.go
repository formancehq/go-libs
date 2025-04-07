package temporal

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/worker"
)

func TestNewDefinitionSet(t *testing.T) {
	// Tester la création d'un ensemble de définitions vide
	definitionSet := NewDefinitionSet()
	require.NotNil(t, definitionSet, "L'ensemble de définitions ne devrait pas être nil")
	require.Len(t, definitionSet, 0, "L'ensemble de définitions devrait être vide")
}

func TestDefinitionSetAppend(t *testing.T) {
	// Tester l'ajout d'une définition à un ensemble
	definitionSet := NewDefinitionSet()

	// Créer une définition de test
	testFunc := func() {}
	definition := Definition{
		Func: testFunc,
		Name: "testFunc",
	}

	// Ajouter la définition à l'ensemble
	newSet := definitionSet.Append(definition)
	require.Len(t, newSet, 1, "L'ensemble de définitions devrait contenir un élément")
	require.Equal(t, definition.Name, newSet[0].Name, "Le nom de la définition devrait être correct")
	require.NotNil(t, newSet[0].Func, "La fonction de la définition ne devrait pas être nil")
}

func TestNewWorkerModule(t *testing.T) {
	// Tester la création d'un module worker
	taskQueue := "test-queue"
	options := worker.Options{}

	// Créer le module
	module := NewWorkerModule(nil, taskQueue, options)
	require.NotNil(t, module, "Le module worker ne devrait pas être nil")
}
