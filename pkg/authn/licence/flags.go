package licence

import (
	"time"

	"github.com/spf13/pflag"
)

var licenceEnabled = false

const (
	LicenceTokenFlag          = "licence-token"
	LicenceValidateTickFlag   = "licence-validate-tick"
	LicenceClusterIDFlag      = "licence-cluster-id"
	LicenceExpectedIssuerFlag = "licence-issuer"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.String(LicenceTokenFlag, "", "Licence token")
	flags.Duration(LicenceValidateTickFlag, 2*time.Minute, "Licence validate tick")
	flags.String(LicenceClusterIDFlag, "", "Licence cluster ID")
	flags.String(LicenceExpectedIssuerFlag, "", "Licence expected issuer")
}

func IsEnabled() bool {
	return licenceEnabled
}
