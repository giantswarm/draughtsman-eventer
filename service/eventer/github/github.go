package github

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
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

	go func() {
		// TODO this map just grows which means we fill the programm with memory.
		etagMap := make(map[string]string)

		for c := ticker.C; ; <-c {
			for _, p := range projects {
				deployments, err := e.fetchNewDeploymentEvents(p, environment, etagMap)
				if err != nil {
					e.logger.Log("error", "could not fetch deployment events", "message", err.Error())
				}

				for _, d := range deployments {
					deploymentEventChannel <- d.DeploymentEvent(p)
				}
			}
		}
	}()

	return deploymentEventChannel, nil
}

func (e *Eventer) FetchLatest(project, environment string) (eventerspec.DeploymentEvent, error) {
	e.logger.Log("debug", "fetching latest deployment", "project", project)

	var err error

	var req *http.Request
	{
		u := fmt.Sprintf(
			DeploymentUrlFormat,
			e.organisation,
			project,
			environment,
		)

		req, err = http.NewRequest("GET", u, nil)
		if err != nil {
			return eventerspec.DeploymentEvent{}, microerror.Mask(err)
		}
	}

	var res *http.Response
	{
		startTime := time.Now()

		res, err := e.request(req)
		if err != nil {
			return eventerspec.DeploymentEvent{}, microerror.Mask(err)
		}
		defer res.Body.Close()

		updateDeploymentMetrics(e.organisation, project, res.StatusCode, startTime)

		if res.StatusCode != http.StatusOK {
			return eventerspec.DeploymentEvent{}, microerror.Maskf(unexpectedStatusCode, fmt.Sprintf("received non-%d status code: %d", http.StatusOK, res.StatusCode))
		}
	}

	var d eventerspec.DeploymentEvent
	{
		bytes, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return eventerspec.DeploymentEvent{}, microerror.Mask(err)
		}

		var deployments []deployment
		if err := json.Unmarshal(bytes, &deployments); err != nil {
			return eventerspec.DeploymentEvent{}, microerror.Mask(err)
		}

		for i, depl := range deployments {
			deploymentStatuses, err := e.fetchDeploymentStatus(project, depl)
			if err != nil {
				return eventerspec.DeploymentEvent{}, microerror.Mask(err)
			}

			deployments[i].Statuses = deploymentStatuses
		}

		deployments = e.filterDeploymentsByStatus(deployments)

		if len(deployments) == 0 {
			return eventerspec.DeploymentEvent{}, microerror.Mask(notFoundError)
		}

		d = deployments[0].DeploymentEvent(project)
	}

	return d, nil
}
