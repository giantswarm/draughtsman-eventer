package github

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/giantswarm/draughtsmantpr"
	draughtsmantprspec "github.com/giantswarm/draughtsmantpr/spec"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/giantswarm/operatorkit/tpr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	eventerspec "github.com/giantswarm/draughtsman-eventer/service/eventer/spec"
	httpspec "github.com/giantswarm/draughtsman-eventer/service/http"
)

var (
	// TODO make TPO namespace configurable in separate PR.
	DefaultNamespace = "default"
	// GithubEventerType is an Eventer that uses Github Deployment Events as a backend.
	GithubEventerType eventerspec.EventerType = "GithubEventer"
	// TPOName is the name of the TPO the eventer watches.
	TPOName = "draughtsman-tpo"
)

// Config represents the configuration used to create a GitHub Eventer.
type Config struct {
	// Dependencies.
	HTTPClient httpspec.Client
	K8sClient  kubernetes.Interface
	Logger     micrologger.Logger

	// Settings.
	Environment  string
	OAuthToken   string
	Organisation string
	PollInterval time.Duration
	ProjectList  []string
}

// DefaultConfig provides a default configuration to create a new GitHub
// Eventer by best effort.
func DefaultConfig() Config {
	return Config{
		// Dependencies.
		HTTPClient: nil,
		K8sClient:  nil,
		Logger:     nil,

		// Settings.
		Environment:  "",
		OAuthToken:   "",
		Organisation: "",
		PollInterval: 0,
		ProjectList:  nil,
	}
}

// Eventer is an implementation of the Eventer interface,
// that uses GitHub Deployment Events as a backend.
type Eventer struct {
	// Dependencies.
	client    httpspec.Client
	k8sClient kubernetes.Interface
	logger    micrologger.Logger

	// Internals.
	bootOnce       sync.Once
	draughtsmanTPR *tpr.TPR

	// Settings.
	environment  string
	oauthToken   string
	organisation string
	pollInterval time.Duration
	projectList  []string
}

// New creates a new configured GitHub Eventer.
func New(config Config) (*Eventer, error) {
	// Dependencies.
	if config.HTTPClient == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.HTTPClient must not be empty")
	}
	if config.K8sClient == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.K8sClient must not be empty")
	}
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.Logger must not be empty")
	}

	// Settings.
	if config.Environment == "" {
		return nil, microerror.Maskf(invalidConfigError, "config.Environment must not be empty")
	}
	if config.OAuthToken == "" {
		return nil, microerror.Maskf(invalidConfigError, "config.OAuthToken token must not be empty")
	}
	if config.Organisation == "" {
		return nil, microerror.Maskf(invalidConfigError, "config.Organisation must not be empty")
	}
	if config.PollInterval.Seconds() == 0 {
		return nil, microerror.Maskf(invalidConfigError, "config.PollInterval must be greater than zero")
	}
	if len(config.ProjectList) == 0 {
		return nil, microerror.Maskf(invalidConfigError, "config.ProjectList must not be empty")
	}

	var err error

	var draughtsmanTPR *tpr.TPR
	{
		tprConfig := tpr.DefaultConfig()

		tprConfig.K8sClient = config.K8sClient
		tprConfig.Logger = config.Logger

		tprConfig.Name = draughtsmantpr.Name
		tprConfig.Version = draughtsmantpr.VersionV1
		tprConfig.Description = draughtsmantpr.Description

		draughtsmanTPR, err = tpr.New(tprConfig)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	eventer := &Eventer{
		// Dependencies.
		client:    config.HTTPClient,
		k8sClient: config.K8sClient,
		logger:    config.Logger,

		// Internals.
		bootOnce:       sync.Once{},
		draughtsmanTPR: draughtsmanTPR,

		// Settings.
		environment:  config.Environment,
		oauthToken:   config.OAuthToken,
		organisation: config.Organisation,
		pollInterval: config.PollInterval,
		projectList:  config.ProjectList,
	}

	return eventer, nil
}

func (e *Eventer) Boot() {
	e.bootOnce.Do(func() {
		for {
			err := e.bootWithError()
			if err != nil {
				e.logger.Log("error", fmt.Sprintf("%#v", microerror.Mask(err)))
			}
		}
	})
}

// TODO create a Github service in a separate PR to ease testing.
func (e *Eventer) NewDeploymentEvents() (<-chan eventerspec.DeploymentEvent, error) {
	e.logger.Log("debug", "starting polling for github deployment events", "interval", e.pollInterval)

	deploymentEventChannel := make(chan eventerspec.DeploymentEvent)
	ticker := time.NewTicker(e.pollInterval)

	go func() {
		// TODO this map just grows which means we fill the programm with memory.
		etagMap := make(map[string]string)

		for c := ticker.C; ; <-c {
			for _, project := range e.projectList {
				deployments, err := e.fetchNewDeploymentEvents(project, etagMap)
				if err != nil {
					e.logger.Log("error", "could not fetch deployment events", "message", err.Error())
				}

				for _, deployment := range deployments {
					deploymentEventChannel <- deployment.DeploymentEvent(project)
				}
			}
		}
	}()

	return deploymentEventChannel, nil
}

func (e *Eventer) bootWithError() error {
	var err error

	// Get TPO to make sure it exists and to have the object which we use to
	// further update with deployment event information.
	var tpo draughtsmantpr.CustomObject
	{
		tpo, err = e.getTPO()
		if IsNotFound(err) {
			// In case the TPO does not yet exist we are going to initialize it below.
			// Then we simply fall through here.
		} else if err != nil {
			return microerror.Mask(err)
		}
	}

	// If the TPO was not found the project list is empty, which means we
	// initialize it.
	if len(tpo.Spec.Projects) == 0 {
		for _, p := range e.projectList {
			d, err := e.fetchLatestDeploymentEvent(p, e.environment)
			if IsNotFound(err) {
				// The current project cannot be deployed at the moment because there is
				// no deployment event yet. Thus we cannot bootstrap initially. This
				// will get fixed later as soon as there is a deployment event. Then the
				// eventer updates the TPO and the opertaor can do the magic.
				continue
			} else if err != nil {
				return microerror.Mask(err)
			}

			newProject := draughtsmantprspec.Project{
				Name: p,
				Ref:  d.Sha,
			}

			tpo.Spec.Projects = append(tpo.Spec.Projects, newProject)
		}
	}

	// At this point we have the TPO computed. Now we can make sure it is created
	// within the Kubernetes API.
	{
		err := e.ensureTPO(tpo)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	// From here on we watch for new deployment events and update the TPO
	// accordingly.
	{
		deploymentEventChannel, err := e.NewDeploymentEvents()
		if err != nil {
			return microerror.Mask(err)
		}

		for d := range deploymentEventChannel {
			tpo, err := e.getTPO()
			if err != nil {
				return microerror.Mask(err)
			}

			newProject := draughtsmantprspec.Project{
				Name: d.Name,
				Ref:  d.Sha,
			}

			tpo.Spec.Projects = ensureProject(tpo.Spec.Projects, newProject)

			err = e.ensureTPO(tpo)
			if err != nil {
				return microerror.Mask(err)
			}
		}
	}

	return nil
}

func (e *Eventer) getTPO() (draughtsmantpr.CustomObject, error) {
	endpoint := e.draughtsmanTPR.Endpoint(DefaultNamespace) + "/" + TPOName

	b, err := e.k8sClient.Core().RESTClient().Get().AbsPath(endpoint).DoRaw()
	if apierrors.IsNotFound(err) {
		return draughtsmantpr.CustomObject{}, microerror.Mask(notFoundError)
	} else if err != nil {
		return draughtsmantpr.CustomObject{}, microerror.Mask(err)
	}

	var tpo draughtsmantpr.CustomObject
	err = json.Unmarshal(b, &tpo)
	if err != nil {
		return draughtsmantpr.CustomObject{}, microerror.Mask(err)
	}

	return tpo, nil
}

// TODO create a TPO service in a separate PR to ease testing.
func (e *Eventer) ensureTPO(tpo draughtsmantpr.CustomObject) error {
	endpoint := e.draughtsmanTPR.Endpoint(DefaultNamespace) + "/" + TPOName
	_, err := e.k8sClient.Core().RESTClient().Post().Body(tpo).AbsPath(endpoint).DoRaw()
	if apierrors.IsNotFound(err) {
		return microerror.Mask(notFoundError)
	} else if apierrors.IsAlreadyExists(err) {
		_, err := e.k8sClient.Core().RESTClient().Put().Body(tpo).AbsPath(endpoint).DoRaw()
		if apierrors.IsNotFound(err) {
			return microerror.Mask(notFoundError)
		} else if err != nil {
			return microerror.Mask(err)
		}
	} else if err != nil {
		return microerror.Mask(err)
	}

	return nil
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
