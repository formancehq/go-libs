package service

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/formancehq/go-libs/v2/errorsutils"
	"github.com/formancehq/go-libs/v2/logging"
	"github.com/formancehq/go-libs/v2/otlp/otlptraces"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

func TestNew(t *testing.T) {
	var buf bytes.Buffer
	app := New(&buf)
	require.NotNil(t, app, "L'application ne devrait pas être nil")
	require.Equal(t, &buf, app.output, "La sortie devrait être correctement définie")
}

func TestNewWithLogger(t *testing.T) {
	logger := logging.Testing()
	app := NewWithLogger(logger)
	require.NotNil(t, app, "L'application ne devrait pas être nil")
	require.Equal(t, logger, app.logger, "Le logger devrait être correctement définie")
}

func TestAddFlags(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(flags)

	// Vérifier que les drapeaux sont définis
	debug, err := flags.GetBool(DebugFlag)
	require.NoError(t, err)
	require.False(t, debug, "Le drapeau debug devrait être false par défaut")

	jsonFormatting, err := flags.GetBool(logging.JsonFormattingLoggerFlag)
	require.NoError(t, err)
	require.False(t, jsonFormatting, "Le drapeau json formatting devrait être false par défaut")

	gracePeriod, err := flags.GetDuration(GracePeriodFlag)
	require.NoError(t, err)
	require.Equal(t, time.Duration(0), gracePeriod, "La période de grâce devrait être 0 par défaut")
}

func TestIsDebug(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool(DebugFlag, false, "")

	require.False(t, IsDebug(cmd), "IsDebug devrait retourner false par défaut")

	cmd.Flags().Set(DebugFlag, "true")
	require.True(t, IsDebug(cmd), "IsDebug devrait retourner true quand le drapeau est défini")
}

func TestAppOptions(t *testing.T) {
	var buf bytes.Buffer

	// Tester l'ajout d'options fx
	option := fx.Provide(func() string { return "test" })
	app2 := New(&buf, option)
	require.Len(t, app2.options, 1, "L'option devrait être ajoutée")

	// Tester avec plusieurs options
	option2 := fx.Provide(func() int { return 42 })
	app3 := New(&buf, option, option2)
	require.Len(t, app3.options, 2, "Les options devraient être ajoutées")
}

func TestNewFxApp(t *testing.T) {
	var buf bytes.Buffer
	app := New(&buf)
	logger := logging.Testing()

	// Tester la création d'une app fx
	fxApp := app.newFxApp(logger, 0)
	require.NotNil(t, fxApp, "L'app fx ne devrait pas être nil")

	// Tester avec une période de grâce
	fxApp2 := app.newFxApp(logger, 5*time.Second)
	require.NotNil(t, fxApp2, "L'app fx avec période de grâce ne devrait pas être nil")
}

func TestApp_Run_WithoutLogger(t *testing.T) {
	var buf bytes.Buffer
	app := New(&buf)

	cmd := &cobra.Command{}
	cmd.Flags().Bool(DebugFlag, false, "")
	cmd.Flags().Bool(logging.JsonFormattingLoggerFlag, false, "")
	cmd.Flags().String(otlptraces.OtelTracesExporterFlag, "", "")
	cmd.Flags().Duration(GracePeriodFlag, 0, "")

	// Créer un contexte annulable
	ctx, cancel := context.WithCancel(context.Background())
	cmd.SetContext(ctx)

	// Annuler le contexte après un court délai
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := app.Run(cmd)
	require.NoError(t, err, "Run ne devrait pas échouer")

	output := buf.String()
	require.Contains(t, output, "Starting application", "Le log de démarrage devrait être présent")
	require.Contains(t, output, "Stopping app", "Le log d'arrêt devrait être présent")
}

func TestApp_Run_WithDebug(t *testing.T) {
	var buf bytes.Buffer
	app := New(&buf, fx.Invoke(func() error {
		return errors.New("test error")
	}))

	cmd := &cobra.Command{}
	cmd.Flags().Bool(DebugFlag, true, "")
	cmd.Flags().Bool(logging.JsonFormattingLoggerFlag, false, "")
	cmd.Flags().String(otlptraces.OtelTracesExporterFlag, "", "")
	cmd.Flags().Duration(GracePeriodFlag, 0, "")

	ctx := context.Background()
	cmd.SetContext(ctx)

	err := app.Run(cmd)
	require.Error(t, err, "Run devrait échouer")
	require.Contains(t, err.Error(), "test error", "L'erreur complète devrait être retournée en mode debug")
}

func TestApp_Run_WithErrorExitCode(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	
	originalExit := appOsExit
	defer func() { 
		mu.Lock()
		appOsExit = originalExit 
		mu.Unlock()
	}()

	exitCalled := false
	exitCode := 0
	
	mockExit := func(code int) {
		mu.Lock()
		exitCalled = true
		exitCode = code
		mu.Unlock()
	}
	
	mu.Lock()
	appOsExit = mockExit
	mu.Unlock()
	app := New(&buf, fx.Invoke(func() error {
		return &errorsutils.ErrorWithExitCode{
			Err:      errors.New("test error"),
			ExitCode: 42,
		}
	}))

	cmd := &cobra.Command{}
	cmd.Flags().Bool(DebugFlag, false, "")
	cmd.Flags().Bool(logging.JsonFormattingLoggerFlag, false, "")
	cmd.Flags().String(otlptraces.OtelTracesExporterFlag, "", "")
	cmd.Flags().Duration(GracePeriodFlag, 0, "")

	ctx := context.Background()
	cmd.SetContext(ctx)

	app.Run(cmd)

	require.True(t, exitCalled, "os.Exit devrait être appelé")
	require.Equal(t, 42, exitCode, "Le code de sortie devrait être 42")
}

func TestApp_Run_WithContextCancel(t *testing.T) {
	var buf bytes.Buffer
	app := New(&buf)

	cmd := &cobra.Command{}
	cmd.Flags().Bool(DebugFlag, false, "")
	cmd.Flags().Bool(logging.JsonFormattingLoggerFlag, false, "")
	cmd.Flags().String(otlptraces.OtelTracesExporterFlag, "", "")
	cmd.Flags().Duration(GracePeriodFlag, 0, "")

	// Créer un contexte annulable
	ctx, cancel := context.WithCancel(context.Background())
	cmd.SetContext(ctx)

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run(cmd)
	}()

	time.Sleep(100 * time.Millisecond)

	cancel()

	select {
	case err := <-errCh:
		require.NoError(t, err, "L'exécution de l'app ne devrait pas échouer")
	case <-time.After(1 * time.Second):
		t.Fatal("L'app n'a pas terminé à temps")
	}

	output := buf.String()
	require.Contains(t, output, "Starting application", "Le log de démarrage devrait être présent")
	require.Contains(t, output, "Stopping app", "Le log d'arrêt devrait être présent")
}

func TestLifecycleFromContext_NotFound(t *testing.T) {
	ctx := context.Background()

	lc := lifecycleFromContext(ctx)
	require.Nil(t, lc, "Le lifecycle devrait être nil quand il n'est pas dans le contexte")
}

func TestContextWithLifecycle(t *testing.T) {
	ctx := context.Background()
	lc := newLifecycle()

	newCtx := contextWithLifecycle(ctx, lc)

	retrievedLc := lifecycleFromContext(newCtx)
	require.Equal(t, lc, retrievedLc, "Le lifecycle devrait être récupéré du contexte")
}

func TestMarkAsAppReady(t *testing.T) {
	ctx := context.Background()

	// Tester sans lifecycle
	markAsAppReady(ctx)

	// Tester avec lifecycle
	lc := newLifecycle()
	ctx = contextWithLifecycle(ctx, lc)

	markAsAppReady(ctx)

	select {
	case <-lc.ready:
	default:
		t.Error("Le canal ready devrait être fermé")
	}
}

func TestMarkAsAppStopped(t *testing.T) {
	ctx := context.Background()

	// Tester sans lifecycle
	markAsAppStopped(ctx)

	// Tester avec lifecycle
	lc := newLifecycle()
	ctx = contextWithLifecycle(ctx, lc)

	markAsAppStopped(ctx)

	select {
	case <-lc.stopped:
	default:
		t.Error("Le canal stopped devrait être fermé")
	}
}
