package ballast

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

func TestModule(t *testing.T) {
	t.Run("with zero ballast size", func(t *testing.T) {
		option := Module(0)
		app := fxtest.New(t, option)
		require.NoError(t, app.Start(context.Background()), "L'application devrait démarrer sans erreur")
		require.NoError(t, app.Stop(context.Background()), "L'application devrait s'arrêter sans erreur")
	})

	t.Run("with non-zero ballast size", func(t *testing.T) {
		originalBallast := ballast
		defer func() {
			ballast = originalBallast
			if ballast != nil {
				ReleaseForGC()
			}
		}()

		ReleaseForGC()
		require.Nil(t, ballast, "Le ballast devrait être nil avant le test")

		option := Module(1024)
		app := fxtest.New(t, option)

		require.NoError(t, app.Start(context.Background()), "L'application devrait démarrer sans erreur")
		require.NotNil(t, ballast, "Le ballast devrait être alloué après le démarrage")

		require.NoError(t, app.Stop(context.Background()), "L'application devrait s'arrêter sans erreur")
		require.Nil(t, ballast, "Le ballast devrait être libéré après l'arrêt")
	})

}
