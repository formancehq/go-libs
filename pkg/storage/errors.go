// Package storage defines neutral storage verdicts independently of SDK backends.
package storage

import "errors"

var (
	// ErrCredentials identifies a definitive credential rejection, not a refreshable token.
	ErrCredentials = errors.New("storage: credentials rejected")
	// ErrPermission identifies an authoritative denial of the requested operation.
	ErrPermission = errors.New("storage: access denied")
)
