package tpo

import "github.com/giantswarm/draughtsmantpr"

type Controller interface {
	Ensure(tpo draughtsmantpr.CustomObject) error
	Get() (draughtsmantpr.CustomObject, error)
}
