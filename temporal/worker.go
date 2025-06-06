package temporal

import (
	"context"

	"github.com/formancehq/go-libs/v3/logging"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	temporalworkflow "go.temporal.io/sdk/workflow"
	"go.uber.org/fx"
)

type Definition struct {
	Func any
	Name string
}

type DefinitionSet []Definition

func NewDefinitionSet() DefinitionSet {
	return DefinitionSet{}
}

func (d DefinitionSet) Append(definition Definition) DefinitionSet {
	d = append(d, definition)

	return d
}

func New(ctx context.Context, logger logging.Logger, c client.Client, taskQueue string, workflows, activities []DefinitionSet, options worker.Options) worker.Worker {
	options.BackgroundActivityContext = logging.ContextWithLogger(ctx, logger)
	worker := worker.New(c, taskQueue, options)

	for _, set := range workflows {
		for _, workflow := range set {
			worker.RegisterWorkflowWithOptions(workflow.Func, temporalworkflow.RegisterOptions{
				Name: workflow.Name,
			})
		}
	}

	for _, set := range activities {
		for _, act := range set {
			worker.RegisterActivityWithOptions(act.Func, activity.RegisterOptions{
				Name: act.Name,
			})
		}
	}

	return worker
}

func NewWorkerModule(ctx context.Context, taskQueue string, options worker.Options) fx.Option {
	return fx.Options(
		fx.Provide(
			fx.Annotate(func(logger logging.Logger, c client.Client, workflows, activities []DefinitionSet) worker.Worker {
				return New(ctx, logger, c, taskQueue, workflows, activities, options)
			}, fx.ParamTags(``, ``, `group:"workflows"`, `group:"activities"`)),
		),
		fx.Invoke(func(lc fx.Lifecycle, w worker.Worker) {
			willStop := false
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					go func() {
						err := w.Run(worker.InterruptCh())
						if err != nil {
							// If the worker is started/stopped fast, the Run method can return an error
							if !willStop {
								panic(err)
							}
						}
					}()
					return nil
				},
				OnStop: func(ctx context.Context) error {
					willStop = true
					w.Stop()
					return nil
				},
			})
		}),
	)
}
