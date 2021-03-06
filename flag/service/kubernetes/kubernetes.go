package kubernetes

import (
	"github.com/giantswarm/draughtsman-eventer/flag/service/kubernetes/tls"
)

type Kubernetes struct {
	Address   string
	InCluster string
	TLS       tls.TLS
}
