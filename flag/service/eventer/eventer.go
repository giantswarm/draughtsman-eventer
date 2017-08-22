package eventer

import (
	"github.com/giantswarm/draughtsman-eventer/flag/service/eventer/github"
)

type Eventer struct {
	Environment string
	GitHub      github.GitHub
	Type        string
}
