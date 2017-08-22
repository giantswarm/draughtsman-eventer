package github

import "github.com/giantswarm/microerror"

var invalidConfigError = microerror.New("invalid config")

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}

var unexpectedStatusCode = microerror.New("unexpected status code")

// IsUnexpectedStatusCode asserts unexpectedStatusCode.
func IsUnexpectedStatusCode(err error) bool {
	return microerror.Cause(err) == unexpectedStatusCode
}
