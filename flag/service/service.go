package service

import (
	"github.com/giantswarm/draughtsman-eventer/flag/service/eventer"
	"github.com/giantswarm/draughtsman-eventer/flag/service/httpclient"
	"github.com/giantswarm/draughtsman-eventer/flag/service/kubernetes"
)

type Service struct {
	Eventer    eventer.Eventer
	HTTPClient httpclient.HTTPClient
	Kubernetes kubernetes.Kubernetes
}
