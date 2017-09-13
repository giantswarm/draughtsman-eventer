// Package service implements business logic to create Kubernetes resources
// against the Kubernetes API.
package service

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cenk/backoff"
	"github.com/giantswarm/microendpoint/service/version"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/giantswarm/operatorkit/client/k8s"
	"github.com/spf13/viper"
	"k8s.io/client-go/kubernetes"

	"github.com/giantswarm/draughtsman-eventer/flag"
	"github.com/giantswarm/draughtsman-eventer/service/eventer"
	eventerspec "github.com/giantswarm/draughtsman-eventer/service/eventer/spec"
	"github.com/giantswarm/draughtsman-eventer/service/healthz"
	"github.com/giantswarm/draughtsman-eventer/service/informer"
	"github.com/giantswarm/draughtsman-eventer/service/tpo"
)

// Config represents the configuration used to create a new service.
type Config struct {
	// Dependencies.
	Logger micrologger.Logger

	// Settings.
	Flag  *flag.Flag
	Viper *viper.Viper

	Description string
	GitCommit   string
	Name        string
	Source      string
}

// DefaultConfig provides a default configuration to create a new service by
// best effort.
func DefaultConfig() Config {
	return Config{
		// Dependencies.
		Logger: nil,

		// Settings.
		Flag:  nil,
		Viper: nil,

		Description: "",
		GitCommit:   "",
		Name:        "",
		Source:      "",
	}
}

type Service struct {
	// Dependencies.
	Healthz  *healthz.Service
	Informer *informer.Service
	Version  *version.Service

	// Internals.
	bootOnce sync.Once
}

// New creates a new configured service object.
func New(config Config) (*Service, error) {
	// Dependencies.
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.Logger must not be empty")
	}
	config.Logger.Log("debug", fmt.Sprintf("creating draughtsman-eventer with config: %#v", config))

	// Settings.
	if config.Flag == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.Flag must not be empty")
	}
	if config.Viper == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.Viper must not be empty")
	}

	var err error

	var k8sClient kubernetes.Interface
	{
		k8sConfig := k8s.DefaultConfig()

		k8sConfig.Address = config.Viper.GetString(config.Flag.Service.Kubernetes.Address)
		k8sConfig.Logger = config.Logger
		k8sConfig.InCluster = config.Viper.GetBool(config.Flag.Service.Kubernetes.InCluster)
		k8sConfig.TLS.CAFile = config.Viper.GetString(config.Flag.Service.Kubernetes.TLS.CAFile)
		k8sConfig.TLS.CrtFile = config.Viper.GetString(config.Flag.Service.Kubernetes.TLS.CrtFile)
		k8sConfig.TLS.KeyFile = config.Viper.GetString(config.Flag.Service.Kubernetes.TLS.KeyFile)

		k8sClient, err = k8s.NewClient(k8sConfig)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var httpClient *http.Client
	{
		timeout := config.Viper.GetDuration(config.Flag.Service.HTTPClient.Timeout)
		if timeout.Seconds() == 0 {
			return nil, microerror.Maskf(invalidConfigError, "http client timeout must be greater than zero")
		}

		httpClient = &http.Client{
			Timeout: timeout,
		}
	}

	var healthzService *healthz.Service
	{
		healthzConfig := healthz.DefaultConfig()

		healthzConfig.K8sClient = k8sClient
		healthzConfig.Logger = config.Logger

		healthzService, err = healthz.New(healthzConfig)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var eventerService eventerspec.Eventer
	{
		eventerConfig := eventer.DefaultConfig()

		eventerConfig.HTTPClient = httpClient
		eventerConfig.Logger = config.Logger

		eventerConfig.Flag = config.Flag
		eventerConfig.Viper = config.Viper

		eventerService, err = eventer.New(eventerConfig)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var tpoService *tpo.Service
	{
		tpoConfig := tpo.DefaultConfig()

		tpoConfig.K8sClient = k8sClient
		tpoConfig.Logger = config.Logger

		tpoService, err = tpo.New(tpoConfig)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var informerBackOff *backoff.ExponentialBackOff
	{
		informerBackOff = backoff.NewExponentialBackOff()
		informerBackOff.MaxElapsedTime = 5 * time.Minute
	}

	var informerService *informer.Service
	{
		informerConfig := informer.DefaultConfig()

		informerConfig.BackOff = informerBackOff
		informerConfig.Eventer = eventerService
		informerConfig.ExitFunc = os.Exit
		informerConfig.Logger = config.Logger
		informerConfig.TPO = tpoService

		informerConfig.Environment = config.Viper.GetString(config.Flag.Service.Eventer.Environment)
		informerConfig.Projects = strings.Split(config.Viper.GetString(config.Flag.Service.Eventer.GitHub.Projects), ",")

		informerService, err = informer.New(informerConfig)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var versionService *version.Service
	{
		versionConfig := version.DefaultConfig()

		versionConfig.Description = config.Description
		versionConfig.GitCommit = config.GitCommit
		versionConfig.Name = config.Name
		versionConfig.Source = config.Source

		versionService, err = version.New(versionConfig)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	newService := &Service{
		// Dependencies.
		Healthz:  healthzService,
		Informer: informerService,
		Version:  versionService,

		// Internals
		bootOnce: sync.Once{},
	}

	return newService, nil
}

func (s *Service) Boot() {
	s.bootOnce.Do(func() {
		s.Informer.Boot()
	})
}
