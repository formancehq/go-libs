package temporal

import (
	"github.com/spf13/pflag"
)

const (
	TemporalAddressFlag                      = "temporal-address"
	TemporalNamespaceFlag                    = "temporal-namespace"
	TemporalSSLClientKeyFlag                 = "temporal-ssl-client-key"
	TemporalSSLClientCertFlag                = "temporal-ssl-client-cert"
	TemporalTaskQueueFlag                    = "temporal-task-queue"
	TemporalInitSearchAttributesFlag         = "temporal-init-search-attributes"
	TemporalMaxParallelActivitiesFlag        = "temporal-max-parallel-activities"
	TemporalMaxConcurrentWorkflowTaskPollers = "temporal-max-concurrent-workflow-task-pollers"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.String(TemporalAddressFlag, "", "Temporal server address")
	flags.String(TemporalNamespaceFlag, "default", "Temporal namespace")
	flags.String(TemporalSSLClientKeyFlag, "", "Temporal client key")
	flags.String(TemporalSSLClientCertFlag, "", "Temporal client cert")
	flags.String(TemporalTaskQueueFlag, "default", "Temporal task queue name")
	flags.Bool(TemporalInitSearchAttributesFlag, false, "Init temporal search attributes")
	flags.Float64(TemporalMaxParallelActivitiesFlag, 10, "Maximum number of parallel activities")
	// MaxConcurrentWorkflowTaskPollers cannot be < 2, otherwise, temporal will panic. 2 is the default
	// and minimum value.
	flags.Int(TemporalMaxConcurrentWorkflowTaskPollers, 2, "Maximum number of concurrent workflow task pollers")
}
