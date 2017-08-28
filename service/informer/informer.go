package informer

import (
	"fmt"
	"sync"
	"time"

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
	Eventer eventerspec.Eventer
	Logger  micrologger.Logger
	TPO     tpo.Controller

	// Settings.
	Environment string
	Projects    []string
}

// DefaultConfig provides a default configuration to create a new informer
// service by best effort.
func DefaultConfig() Config {
	return Config{
		// Dependencies.
		Eventer: nil,
		Logger:  nil,
		TPO:     nil,

		// Settings.
		Environment: "",
		Projects:    nil,
	}
}

type Service struct {
	// Dependencies.
	eventer eventerspec.Eventer
	logger  micrologger.Logger
	tpo     tpo.Controller

	// Internals.
	bootOnce sync.Once

	// Settings.
	environment string
	projects    []string
}

// New creates a new configured informer service.
func New(config Config) (*Service, error) {
	// Dependencies.
	if config.Eventer == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.Eventer must not be empty")
	}
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.Logger must not be empty")
	}
	if config.TPO == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.TPO must not be empty")
	}

	// Dependencies.
	if config.Environment == "" {
		return nil, microerror.Maskf(invalidConfigError, "config.Environment must not be empty")
	}
	if len(config.Projects) == 0 || containsEmptyItems(config.Projects) {
		return nil, microerror.Maskf(invalidConfigError, "config.Projects must not be empty")
	}

	newInformer := &Service{
		// Dependencies.
		eventer: config.Eventer,
		logger:  config.Logger,
		tpo:     config.TPO,

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
		for {
			err := s.bootWithError()
			if err != nil {
				s.logger.Log("error", fmt.Sprintf("%#v", microerror.Mask(err)))
				time.Sleep(time.Second)
			}
		}
	})
}

func (s *Service) bootWithError() error {
	var err error

	// Get TPO to make sure it exists and to have the object which we use to
	// further update with deployment event information.
	var TPO draughtsmantpr.CustomObject
	{
		TPO, err = s.tpo.Get()
		if tpo.IsNotFound(err) {
			// In case the TPO does not yet exist we are going to initialize it below.
			// Then we simply fall through here.
		} else if err != nil {
			return microerror.Mask(err)
		}
	}

	// If the TPO was not found the project list is empty, which means we
	// initialize it.
	if len(TPO.Spec.Projects) == 0 {
		for _, p := range s.projects {
			d, err := s.eventer.FetchLatest(p, s.environment)
			if eventer.IsNotFound(err) {
				// The current project cannot be deployed at the moment because there is
				// no deployment event yet. Thus we cannot bootstrap initially. This
				// will get fixed later as soon as there is a deployment event. Then the
				// eventer updates the TPO and the operator can do the magic.
				continue
			} else if err != nil {
				return microerror.Mask(err)
			}

			newProject := draughtsmantprspec.Project{
				Name: p,
				Ref:  d.Sha,
			}

			TPO.Spec.Projects = append(TPO.Spec.Projects, newProject)
		}
	}

	// At this point we have the TPO computed. Now we can make sure it is created
	// within the Kubernetes API.
	{
		err := s.tpo.Ensure(TPO)
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

		for d := range deploymentEventChannel {
			TPO, err := s.tpo.Get()
			if err != nil {
				return microerror.Mask(err)
			}

			newProject := draughtsmantprspec.Project{
				Name: d.Name,
				Ref:  d.Sha,
			}

			TPO.Spec.Projects = ensureProject(TPO.Spec.Projects, newProject)

			err = s.tpo.Ensure(TPO)
			if err != nil {
				return microerror.Mask(err)
			}
		}
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

func ensureProject(projects []draughtsmantprspec.Project, project draughtsmantprspec.Project) []draughtsmantprspec.Project {
	var updated bool
	for _, p := range projects {
		if p.Name != project.Name {
			continue
		}

		p.Ref = project.Ref
		updated = true
	}

	if updated {
		return projects
	}

	projects = append(projects, project)

	return projects
}
