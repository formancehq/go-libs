package temporal

import (
	"context"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	temporalworkflow "go.temporal.io/sdk/workflow"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
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
