package tpo

import "github.com/giantswarm/draughtsmantpr"

type Controller interface {
	Ensure(TPO *draughtsmantpr.CustomObject) error
	Get() (*draughtsmantpr.CustomObject, error)
}
