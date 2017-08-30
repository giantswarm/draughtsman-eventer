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
	OAuthToken   string
	Organisation string
	PollInterval time.Duration
}

// DefaultConfig provides a default configuration to create a new GitHub
// Eventer by best effort.
func DefaultConfig() Config {
	return Config{
		// Dependencies.
		HTTPClient: nil,
		Logger:     nil,

		// Settings.
		OAuthToken:   "",
		Organisation: "",
		PollInterval: 0,
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
	etagMap  map[string]string

	// Settings.
	oauthToken   string
	organisation string
	pollInterval time.Duration
}

// New creates a new configured GitHub Eventer.
func New(config Config) (*Eventer, error) {
	// Dependencies.
	if config.HTTPClient == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.HTTPClient must not be empty")
	}
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.Logger must not be empty")
	}

	// Settings.
	if config.OAuthToken == "" {
		return nil, microerror.Maskf(invalidConfigError, "config.OAuthToken token must not be empty")
	}
	if config.Organisation == "" {
		return nil, microerror.Maskf(invalidConfigError, "config.Organisation must not be empty")
	}
	if config.PollInterval.Seconds() == 0 {
		return nil, microerror.Maskf(invalidConfigError, "config.PollInterval must be greater than zero")
	}

	eventer := &Eventer{
		// Dependencies.
		client: config.HTTPClient,
		logger: config.Logger,

		// Internals.
		bootOnce: sync.Once{},
		etagMap:  map[string]string{},

		// Settings.
		oauthToken:   config.OAuthToken,
		organisation: config.Organisation,
		pollInterval: config.PollInterval,
	}

	return eventer, nil
}

func (e *Eventer) FetchContinuously(projects []string, environment string) (<-chan eventerspec.DeploymentEvent, error) {
	e.logger.Log("debug", "starting polling for github deployment events", "interval", e.pollInterval)

	deploymentEventChannel := make(chan eventerspec.DeploymentEvent)
	ticker := time.NewTicker(e.pollInterval)
	filterFinished := true

	go func() {
		for {
			select {
			case <-ticker.C:
				for _, p := range projects {
					deployments, err := e.fetchNewDeploymentEvents(p, environment, e.etagMap, filterFinished)
					if err != nil {
						e.logger.Log("error", "could not fetch deployment events", "message", err.Error())
					}

					for _, d := range deployments {
						deploymentEventChannel <- d.DeploymentEvent(p)
					}
				}
			}
		}
	}()

	return deploymentEventChannel, nil
}

func (e *Eventer) FetchLatest(project, environment string) (eventerspec.DeploymentEvent, error) {
	e.logger.Log("debug", "fetching latest deployment", "project", project)

	filterFinished := false
	deployments, err := e.fetchNewDeploymentEvents(project, environment, e.etagMap, filterFinished)
	if err != nil {
		return eventerspec.DeploymentEvent{}, microerror.Mask(err)
	}

	if len(deployments) == 0 {
		return eventerspec.DeploymentEvent{}, microerror.Mask(notFoundError)
	}

	return deployments[0].DeploymentEvent(project), nil
}
