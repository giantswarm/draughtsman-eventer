package github

import (
	"sync"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"

	eventerspec "github.com/giantswarm/draughtsman-eventer/service/eventer/spec"
	httpspec "github.com/giantswarm/draughtsman-eventer/service/http"
)

var (
	// GithubEventerType is an Eventer that uses Github Deployment Events as a backend.
	GithubEventerType eventerspec.EventerType = "GithubEventer"
)

// Config represents the configuration used to create a GitHub Eventer.
type Config struct {
	// Dependencies.
	HTTPClient httpspec.Client
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
	client httpspec.Client
	logger micrologger.Logger

	// Internals.
	bootOnce sync.Once

	// Settings.
	environment  string
	oauthToken   string
	organisation string
	pollInterval time.Duration
	projectList  []string
}

// New creates a new configured GitHub Eventer.
func New(config Config) (*Eventer, error) {
	if config.HTTPClient == nil {
		return nil, microerror.Maskf(invalidConfigError, "http client must not be empty")
	}
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "logger must not be empty")
	}

	if config.Environment == "" {
		return nil, microerror.Maskf(invalidConfigError, "environment must not be empty")
	}
	if config.OAuthToken == "" {
		return nil, microerror.Maskf(invalidConfigError, "oauth token must not be empty")
	}
	if config.Organisation == "" {
		return nil, microerror.Maskf(invalidConfigError, "organisation must not be empty")
	}
	if config.PollInterval.Seconds() == 0 {
		return nil, microerror.Maskf(invalidConfigError, "interval must be greater than zero")
	}
	if len(config.ProjectList) == 0 {
		return nil, microerror.Maskf(invalidConfigError, "project list must not be empty")
	}

	eventer := &Eventer{
		// Dependencies.
		client: config.HTTPClient,
		logger: config.Logger,

		// Internals
		bootOnce: sync.Once{},

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
		// TODO check if TPO exists

		// TODO if TPO does not exist
		// TODO     go to start

		// TODO if TPO exists
		// TODO     check if TPO's project list is empty
		// TODO         if TPO's project list is empty
		// TODO             read all recent deployment events of configured projects
		// TODO             update TPO with project information

		// TODO loop over deployment events channel and periodically update TPO
	})
}

func (e *Eventer) NewDeploymentEvents() (<-chan eventerspec.DeploymentEvent, error) {
	e.logger.Log("debug", "starting polling for github deployment events", "interval", e.pollInterval)

	deploymentEventChannel := make(chan eventerspec.DeploymentEvent)
	ticker := time.NewTicker(e.pollInterval)

	go func() {
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

func (e *Eventer) SetPending(event eventerspec.DeploymentEvent) error {
	return e.postDeploymentStatus(event.Name, event.ID, pendingState)
}

func (e *Eventer) SetSuccess(event eventerspec.DeploymentEvent) error {
	return e.postDeploymentStatus(event.Name, event.ID, successState)
}

func (e *Eventer) SetFailed(event eventerspec.DeploymentEvent) error {
	return e.postDeploymentStatus(event.Name, event.ID, failureState)
}
