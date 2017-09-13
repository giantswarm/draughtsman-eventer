package informer

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/cenk/backoff"
	"github.com/giantswarm/draughtsmantpr"
	draughtsmantprspec "github.com/giantswarm/draughtsmantpr/spec"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"

	"github.com/giantswarm/draughtsman-eventer/service/eventer"
	eventerspec "github.com/giantswarm/draughtsman-eventer/service/eventer/spec"
	"github.com/giantswarm/draughtsman-eventer/service/tpo"
)

// Config represents the configuration used to create a informer service.
type Config struct {
	// Dependencies.
	BackOff  backoff.BackOff
	Eventer  eventerspec.Eventer
	ExitFunc func(code int)
	Logger   micrologger.Logger
	TPO      tpo.Controller

	// Settings.
	Environment string
	Projects    []string
}

// DefaultConfig provides a default configuration to create a new informer
// service by best effort.
func DefaultConfig() Config {
	return Config{
		// Dependencies.
		BackOff:  nil,
		Eventer:  nil,
		ExitFunc: nil,
		Logger:   nil,
		TPO:      nil,

		// Settings.
		Environment: "",
		Projects:    nil,
	}
}

type Service struct {
	// Dependencies.
	backOff  backoff.BackOff
	eventer  eventerspec.Eventer
	exitFunc func(code int)
	logger   micrologger.Logger
	tpo      tpo.Controller

	// Internals.
	bootOnce sync.Once

	// Settings.
	environment string
	projects    []string
}

// New creates a new configured informer service.
func New(config Config) (*Service, error) {
	// Dependencies.
	if config.BackOff == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.BackOff must not be empty")
	}
	if config.Eventer == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.Eventer must not be empty")
	}
	if config.ExitFunc == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.ExitFunc must not be empty")
	}
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.Logger must not be empty")
	}
	if config.TPO == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.TPO must not be empty")
	}

	// Settings.
	if config.Environment == "" {
		return nil, microerror.Maskf(invalidConfigError, "config.Environment must not be empty")
	}
	if len(config.Projects) == 0 || containsEmptyItems(config.Projects) {
		return nil, microerror.Maskf(invalidConfigError, "config.Projects must not be empty")
	}

	newInformer := &Service{
		// Dependencies.
		backOff:  config.BackOff,
		eventer:  config.Eventer,
		exitFunc: config.ExitFunc,
		logger:   config.Logger,
		tpo:      config.TPO,

		// Internals
		bootOnce: sync.Once{},

		// Settings.
		environment: config.Environment,
		projects:    config.Projects,
	}

	return newInformer, nil
}

func (s *Service) Boot() {
	s.bootOnce.Do(func() {
		o := func() error {
			err := s.bootWithError()
			if err != nil {
				return microerror.Mask(err)
			}

			return nil
		}

		n := func(err error, d time.Duration) {
			s.logger.Log("warning", fmt.Sprintf("retrying informer boot due to error: %#v", microerror.Mask(err)))
		}

		err := backoff.RetryNotify(o, s.backOff, n)
		if err != nil {
			s.logger.Log("error", fmt.Sprintf("stop informer boot retries due to too many errors: %#v", microerror.Mask(err)))
			s.exitFunc(1)
		}
	})
}

func (s *Service) bootWithError() error {
	var err error

	// Get TPO to make sure it exists and to have the object which we use to
	// further update with deployment event information.
	var TPO *draughtsmantpr.CustomObject
	{
		TPO, err = s.tpo.Get()
		if tpo.IsNotFound(err) {
			// In case the TPO does not yet exist we are going to initialize it below.
			// Then we simply fall through here.
		} else if err != nil {
			return microerror.Mask(err)
		}
		// In case the TPO is for whatever reason nil, we initialize the structure
		// with a new pointer to be able to setup properly below.
		if TPO == nil {
			TPO = &draughtsmantpr.CustomObject{}
		}
	}

	// If the TPO was not found the project list is empty, which means we
	// initialize it.
	for _, p := range s.projects {
		e, err := s.eventer.FetchLatest(p, s.environment)
		if eventer.IsNotFound(err) {
			// The current project cannot be deployed at the moment because there is
			// no deployment event yet. Thus we cannot bootstrap initially. This
			// will get fixed later as soon as there is a deployment event. Then the
			// eventer updates the TPO and the operator can do the magic.
			continue
		} else if err != nil {
			return microerror.Mask(err)
		}

		err = s.alignEventWithObject(e, TPO)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	// From here on we watch for new deployment events and update the TPO
	// accordingly.
	{
		deploymentEventChannel, err := s.eventer.FetchContinuously(s.projects, s.environment)
		if err != nil {
			return microerror.Mask(err)
		}

		for e := range deploymentEventChannel {
			TPO, err := s.tpo.Get()
			if err != nil {
				return microerror.Mask(err)
			}

			err = s.alignEventWithObject(e, TPO)
			if err != nil {
				return microerror.Mask(err)
			}
		}
	}

	return nil
}

func (s *Service) alignEventWithObject(e eventerspec.DeploymentEvent, TPO *draughtsmantpr.CustomObject) error {
	newProject := draughtsmantprspec.Project{
		ID:   strconv.Itoa(e.ID),
		Name: e.Name,
		Ref:  e.Sha,
	}

	var updated bool
	TPO.Spec.Projects, updated = ensureProject(TPO.Spec.Projects, newProject)
	if !updated {
		return nil
	}
	s.logger.Log("debug", "found new deployment", "project", newProject.Name)

	err := s.tpo.Ensure(TPO)
	if err != nil {
		return microerror.Mask(err)
	}

	err = s.eventer.SetPendingStatus(e)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func containsEmptyItems(projects []string) bool {
	for _, p := range projects {
		if p == "" {
			return true
		}
	}

	return false
}

// ensureProject takes care of updating the given projects list with the given
// project. In case the project cannot be found in the list, it is added. In
// case the project is found in the list, it is updated, if it changed. In case
// the list got updated somehow the returned boolean is true.
func ensureProject(projects []draughtsmantprspec.Project, project draughtsmantprspec.Project) ([]draughtsmantprspec.Project, bool) {
	if project.ID == "" || project.Name == "" || project.Ref == "" {
		return projects, false
	}

	_, err := getProjectByName(projects, project.Name)
	if IsNotFound(err) {
		projects = append(projects, project)
		return projects, true
	}

	for i, p := range projects {
		if p.Name == project.Name && p.ID != project.ID {
			projects[i] = project
			return projects, true
		}
	}

	return projects, false
}

func getProjectByName(projects []draughtsmantprspec.Project, name string) (draughtsmantprspec.Project, error) {
	for _, p := range projects {
		if p.Name == name {
			return p, nil
		}
	}

	return draughtsmantprspec.Project{}, microerror.Maskf(notFoundError, "project with name '%s'", name)
}
