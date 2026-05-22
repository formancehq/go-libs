package audit

import (
	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
)

type Options struct {
	KeySets        map[string]oidc.KeySet
	OrganizationID string
	StackID        string
}

type Option func(*Options)

// WithAuth enables JWT claims extraction from the Authorization header.
func WithAuth(keySets map[string]oidc.KeySet) Option {
	return func(o *Options) {
		o.KeySets = keySets
	}
}

// WithOrganizationID sets the organization ID included in audit events.
func WithOrganizationID(id string) Option {
	return func(o *Options) {
		o.OrganizationID = id
	}
}

// WithStackID sets the stack ID included in audit events.
func WithStackID(id string) Option {
	return func(o *Options) {
		o.StackID = id
	}
}

func NewOptions(opts ...Option) *Options {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}
