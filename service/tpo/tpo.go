package tpo

import (
	"encoding/json"

	"github.com/giantswarm/draughtsmantpr"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/giantswarm/operatorkit/tpr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
)

const (
	// TODO make TPO namespace configurable in separate PR.
	DefaultNamespace = "default"
	// Name is the name of the TPO the eventer watches.
	Name = "draughtsman-tpo"
)

// Config represents the configuration used to create a TPO service.
type Config struct {
	// Dependencies.
	K8sClient kubernetes.Interface
	Logger    micrologger.Logger
}

// DefaultConfig provides a default configuration to create a new TPO service by
// best effort.
func DefaultConfig() Config {
	return Config{
		// Dependencies.
		K8sClient: nil,
		Logger:    nil,
	}
}

type Service struct {
	// Dependencies.
	k8sClient kubernetes.Interface
	logger    micrologger.Logger

	// Internals.
	draughtsmanTPR *tpr.TPR
}

// New creates a new configured TPO service.
func New(config Config) (*Service, error) {
	// Dependencies.
	if config.K8sClient == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.K8sClient must not be empty")
	}
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "config.Logger must not be empty")
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

	eventer := &Service{
		// Dependencies.
		k8sClient: config.K8sClient,
		logger:    config.Logger,

		// Internals.
		draughtsmanTPR: draughtsmanTPR,
	}

	return eventer, nil
}

func (s *Service) Ensure(tpo draughtsmantpr.CustomObject) error {
	if tpo.TypeMeta.APIVersion == "" {
		tpo.TypeMeta.APIVersion = s.draughtsmanTPR.APIVersion()
	}
	if tpo.TypeMeta.Kind == "" {
		tpo.TypeMeta.Kind = s.draughtsmanTPR.Kind()
	}

	endpoint := s.draughtsmanTPR.Endpoint(DefaultNamespace) + "/" + Name
	_, err := s.k8sClient.Core().RESTClient().Post().Body(tpo).AbsPath(endpoint).DoRaw()
	if apierrors.IsNotFound(err) {
		return microerror.Mask(notFoundError)
	} else if apierrors.IsAlreadyExists(err) {
		_, err := s.k8sClient.Core().RESTClient().Put().Body(tpo).AbsPath(endpoint).DoRaw()
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

func (s *Service) Get() (draughtsmantpr.CustomObject, error) {
	endpoint := s.draughtsmanTPR.Endpoint(DefaultNamespace) + "/" + Name

	b, err := s.k8sClient.Core().RESTClient().Get().AbsPath(endpoint).DoRaw()
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
