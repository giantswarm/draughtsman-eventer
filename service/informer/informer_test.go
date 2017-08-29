package informer

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/giantswarm/draughtsmantpr"
	"github.com/giantswarm/micrologger/microloggertest"

	"github.com/giantswarm/draughtsman-eventer/service/eventer"
	eventerspec "github.com/giantswarm/draughtsman-eventer/service/eventer/spec"
	draughtsmantprspec "github.com/giantswarm/draughtsmantpr/spec"
)

func Test_Informer_EventManagement(t *testing.T) {
	environment := "master"
	projects := []string{
		"api-name",
		"cluster-service-name",
		"kubernetesd-name",
	}

	testCases := []struct {
		Eventer               *testEventer
		TPOController         *testTPOController
		ExpectedEnsuredCalled int
		ExpectedTPO           draughtsmantpr.CustomObject
	}{
		// Test 1 makes sure the deployment event of a single project will be
		// tracked within the TPO based on its latest deployment event.
		{
			Eventer: &testEventer{
				ContinuousEvents: nil,
				LatestEvents: map[string]eventerspec.DeploymentEvent{
					"api-name": {
						ID:   100,
						Name: "api-name",
						Sha:  "api-sha-1",
					},
				},
			},
			TPOController: &testTPOController{
				EnsureCalled: 0,
				Err:          nil,
				Mutex:        sync.Mutex{},
				TPO:          draughtsmantpr.CustomObject{},
			},
			ExpectedEnsuredCalled: 1,
			ExpectedTPO: draughtsmantpr.CustomObject{
				Spec: draughtsmantpr.Spec{
					Projects: []draughtsmantprspec.Project{
						{
							Name: "api-name",
							Ref:  "api-sha-1",
						},
					},
				},
			},
		},

		// Test 2 makes sure the deployments event of multiple projects will be
		// tracked within the TPO based on their latest deployment event.
		{
			Eventer: &testEventer{
				ContinuousEvents: nil,
				LatestEvents: map[string]eventerspec.DeploymentEvent{
					"api-name": {
						ID:   100,
						Name: "api-name",
						Sha:  "api-sha-1",
					},
					"cluster-service-name": {
						ID:   101,
						Name: "cluster-service-name",
						Sha:  "cluster-service-sha-1",
					},
				},
			},
			TPOController: &testTPOController{
				EnsureCalled: 0,
				Err:          nil,
				Mutex:        sync.Mutex{},
				TPO:          draughtsmantpr.CustomObject{},
			},
			ExpectedEnsuredCalled: 1,
			ExpectedTPO: draughtsmantpr.CustomObject{
				Spec: draughtsmantpr.Spec{
					Projects: []draughtsmantprspec.Project{
						{
							Name: "api-name",
							Ref:  "api-sha-1",
						},
						{
							Name: "cluster-service-name",
							Ref:  "cluster-service-sha-1",
						},
					},
				},
			},
		},

		// Test 3 makes sure the deployment event of a single project will be
		// tracked within the TPO based on its continuous deployment event, which
		// overwrites the latest one.
		{
			Eventer: &testEventer{
				ContinuousEvents: map[string]eventerspec.DeploymentEvent{
					"api-name": {
						ID:   101,
						Name: "api-name",
						Sha:  "api-sha-2",
					},
				},
				LatestEvents: map[string]eventerspec.DeploymentEvent{
					"api-name": {
						ID:   100,
						Name: "api-name",
						Sha:  "api-sha-1",
					},
				},
			},
			TPOController: &testTPOController{
				EnsureCalled: 0,
				Err:          nil,
				Mutex:        sync.Mutex{},
				TPO:          draughtsmantpr.CustomObject{},
			},
			ExpectedEnsuredCalled: 2,
			ExpectedTPO: draughtsmantpr.CustomObject{
				Spec: draughtsmantpr.Spec{
					Projects: []draughtsmantprspec.Project{
						{
							Name: "api-name",
							Ref:  "api-sha-2",
						},
					},
				},
			},
		},

		// Test 4 makes sure the deployment events of multiple projects will be
		// tracked within the TPO based on their continuous deployment events, which
		// overwrite the latest ones.
		{
			Eventer: &testEventer{
				ContinuousEvents: map[string]eventerspec.DeploymentEvent{
					"api-name": {
						ID:   101,
						Name: "api-name",
						Sha:  "api-sha-2",
					},
					"cluster-service-name": {
						ID:   103,
						Name: "cluster-service-name",
						Sha:  "cluster-service-sha-2",
					},
				},
				LatestEvents: map[string]eventerspec.DeploymentEvent{
					"api-name": {
						ID:   100,
						Name: "api-name",
						Sha:  "api-sha-1",
					},
					"cluster-service-name": {
						ID:   102,
						Name: "cluster-service-name",
						Sha:  "cluster-service-sha-1",
					},
				},
			},
			TPOController: &testTPOController{
				EnsureCalled: 0,
				Err:          nil,
				Mutex:        sync.Mutex{},
				TPO:          draughtsmantpr.CustomObject{},
			},
			ExpectedEnsuredCalled: 3,
			ExpectedTPO: draughtsmantpr.CustomObject{
				Spec: draughtsmantpr.Spec{
					Projects: []draughtsmantprspec.Project{
						{
							Name: "api-name",
							Ref:  "api-sha-2",
						},
						{
							Name: "cluster-service-name",
							Ref:  "cluster-service-sha-2",
						},
					},
				},
			},
		},
	}

	for i, tc := range testCases {
		var err error

		var newInformer *Service
		{
			informerConfig := DefaultConfig()

			informerConfig.Eventer = tc.Eventer
			informerConfig.Logger = microloggertest.New()
			informerConfig.TPO = tc.TPOController

			informerConfig.Environment = environment
			informerConfig.Projects = projects

			newInformer, err = New(informerConfig)
			if err != nil {
				t.Fatalf("test %d expected %#v got %#v", i+1, nil, err)
			}
		}

		go newInformer.Boot()

		done := make(chan struct{}, 1)
		go func() {
			for {
				tc.TPOController.Mutex.Lock()
				if tc.TPOController.EnsureCalled >= tc.ExpectedEnsuredCalled {
					tc.TPOController.Mutex.Unlock()
					break
				}
				tc.TPOController.Mutex.Unlock()
				time.Sleep(10 * time.Millisecond)
			}
			close(done)
		}()
		wait := func() {
			for {
				select {
				case <-time.After(100 * time.Millisecond):
					t.Fatalf("test %d timed out", i+1)
				case <-done:
					return
				}
			}
		}
		wait()

		controllerTPO, err := tc.TPOController.Get()
		if err != nil {
			t.Fatalf("test %d expected %#v got %#v", i+1, nil, err)
		}
		if !reflect.DeepEqual(controllerTPO.Spec.Projects, tc.ExpectedTPO.Spec.Projects) {
			t.Fatalf("test %d expected %#v got %#v", i+1, tc.ExpectedTPO.Spec.Projects, controllerTPO.Spec.Projects)
		}
	}
}

type testTPOController struct {
	EnsureCalled int
	Err          error
	Mutex        sync.Mutex
	TPO          draughtsmantpr.CustomObject
}

func (c *testTPOController) Ensure(tpo draughtsmantpr.CustomObject) error {
	c.Mutex.Lock()
	c.TPO = tpo
	c.EnsureCalled++
	c.Mutex.Unlock()
	return nil
}

func (c *testTPOController) Get() (draughtsmantpr.CustomObject, error) {
	return c.TPO, c.Err
}

type testEventer struct {
	ContinuousEvents map[string]eventerspec.DeploymentEvent
	LatestEvents     map[string]eventerspec.DeploymentEvent
}

func (e *testEventer) FetchContinuously(projects []string, environment string) (<-chan eventerspec.DeploymentEvent, error) {
	deploymentEventChannel := make(chan eventerspec.DeploymentEvent, len(e.ContinuousEvents))

	for project, deploymentEvent := range e.ContinuousEvents {
		for _, p := range projects {
			if project == p {
				deploymentEventChannel <- deploymentEvent
			}
		}
	}

	return deploymentEventChannel, nil
}

func (e *testEventer) FetchLatest(project, environment string) (eventerspec.DeploymentEvent, error) {
	event, ok := e.LatestEvents[project]
	if ok {
		return event, nil
	}

	return eventerspec.DeploymentEvent{}, eventer.NotFoundError
}
