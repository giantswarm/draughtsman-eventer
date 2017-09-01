package informer

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/cenk/backoff"
	"github.com/giantswarm/draughtsmantpr"
	"github.com/giantswarm/micrologger/microloggertest"

	"github.com/giantswarm/draughtsman-eventer/service/eventer"
	eventerspec "github.com/giantswarm/draughtsman-eventer/service/eventer/spec"
	draughtsmantprspec "github.com/giantswarm/draughtsmantpr/spec"
)

func Test_Informer_BackOff_NoRetries(t *testing.T) {
	environment := "master"
	projects := []string{
		"api-name",
		"cluster-service-name",
		"kubernetesd-name",
	}

	var err error

	tpoController := &testTPOController{
		EnsureCalled: 0,
		Err:          fmt.Errorf("test error"),
		GetCalled:    0,
		Mutex:        sync.Mutex{},
		TPO:          &draughtsmantpr.CustomObject{},
	}

	var newInformer *Service
	{
		informerConfig := DefaultConfig()

		informerConfig.BackOff = &backoff.StopBackOff{}
		informerConfig.Eventer = &testEventer{}
		informerConfig.Logger = microloggertest.New()
		informerConfig.TPO = tpoController

		informerConfig.Environment = environment
		informerConfig.Projects = projects

		newInformer, err = New(informerConfig)
		if err != nil {
			t.Fatalf("expected %#v got %#v", nil, err)
		}
	}

	newInformer.Boot()

	if tpoController.GetCalled != 1 {
		t.Fatalf("expected %d got %d", 1, tpoController.GetCalled)
	}
}

func Test_Informer_BackOff_MultipleRetries(t *testing.T) {
	environment := "master"
	projects := []string{
		"api-name",
		"cluster-service-name",
		"kubernetesd-name",
	}

	var err error

	tpoController := &testTPOController{
		EnsureCalled: 0,
		Err:          fmt.Errorf("test error"),
		GetCalled:    0,
		Mutex:        sync.Mutex{},
		TPO:          &draughtsmantpr.CustomObject{},
	}

	var newInformer *Service
	{
		informerConfig := DefaultConfig()

		informerConfig.BackOff = backoff.WithMaxTries(&backoff.ZeroBackOff{}, 3)
		informerConfig.Eventer = &testEventer{}
		informerConfig.Logger = microloggertest.New()
		informerConfig.TPO = tpoController

		informerConfig.Environment = environment
		informerConfig.Projects = projects

		newInformer, err = New(informerConfig)
		if err != nil {
			t.Fatalf("expected %#v got %#v", nil, err)
		}
	}

	newInformer.Boot()

	if tpoController.GetCalled != 4 {
		t.Fatalf("expected %d got %d", 4, tpoController.GetCalled)
	}
}

// Test_Informer_EventManagement_NilTPO ensures the boot process does not panic
// even if there is no TPO found or a nil TPO be used.
func Test_Informer_EventManagement_NilTPO(t *testing.T) {
	environment := "master"
	projects := []string{
		"api-name",
	}

	var err error

	tpoController := &testTPOController{
		EnsureCalled: 0,
		Err:          nil, // Err is nil so the informer process goes on.
		GetCalled:    0,
		Mutex:        sync.Mutex{},
		TPO:          nil, // TPO is nil so the code should not panic.
	}

	te := &testEventer{
		ContinuousEvents: nil,
		LatestEvents: map[string]eventerspec.DeploymentEvent{
			"api-name": {
				ID:   100,
				Name: "api-name",
				Sha:  "api-sha-1",
			},
		},
	}

	var newInformer *Service
	{
		informerConfig := DefaultConfig()

		informerConfig.BackOff = &backoff.StopBackOff{}
		informerConfig.Eventer = te
		informerConfig.Logger = microloggertest.New()
		informerConfig.TPO = tpoController

		informerConfig.Environment = environment
		informerConfig.Projects = projects

		newInformer, err = New(informerConfig)
		if err != nil {
			t.Fatalf("expected %#v got %#v", nil, err)
		}
	}

	go newInformer.Boot()

	expectedEnsuredCalled := 1

	done := make(chan struct{}, 1)
	go func() {
		for {
			tpoController.Mutex.Lock()
			if tpoController.EnsureCalled >= expectedEnsuredCalled {
				tpoController.Mutex.Unlock()
				break
			}
			tpoController.Mutex.Unlock()
			time.Sleep(10 * time.Millisecond)
		}
		close(done)
	}()
	wait := func() {
		for {
			select {
			case <-time.After(100 * time.Millisecond):
				t.Fatalf("timed out")
			case <-done:
				return
			}
		}
	}
	wait()
}

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
		ExpectedTPO           *draughtsmantpr.CustomObject
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
				GetCalled:    0,
				Mutex:        sync.Mutex{},
				TPO:          &draughtsmantpr.CustomObject{},
			},
			ExpectedEnsuredCalled: 1,
			ExpectedTPO: &draughtsmantpr.CustomObject{
				Spec: draughtsmantpr.Spec{
					Projects: []draughtsmantprspec.Project{
						{
							ID:   "100",
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
				GetCalled:    0,
				Mutex:        sync.Mutex{},
				TPO:          &draughtsmantpr.CustomObject{},
			},
			ExpectedEnsuredCalled: 1,
			ExpectedTPO: &draughtsmantpr.CustomObject{
				Spec: draughtsmantpr.Spec{
					Projects: []draughtsmantprspec.Project{
						{
							ID:   "100",
							Name: "api-name",
							Ref:  "api-sha-1",
						},
						{
							ID:   "101",
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
				GetCalled:    0,
				Mutex:        sync.Mutex{},
				TPO:          &draughtsmantpr.CustomObject{},
			},
			ExpectedEnsuredCalled: 2,
			ExpectedTPO: &draughtsmantpr.CustomObject{
				Spec: draughtsmantpr.Spec{
					Projects: []draughtsmantprspec.Project{
						{
							ID:   "101",
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
				GetCalled:    0,
				Mutex:        sync.Mutex{},
				TPO:          &draughtsmantpr.CustomObject{},
			},
			ExpectedEnsuredCalled: 3,
			ExpectedTPO: &draughtsmantpr.CustomObject{
				Spec: draughtsmantpr.Spec{
					Projects: []draughtsmantprspec.Project{
						{
							ID:   "101",
							Name: "api-name",
							Ref:  "api-sha-2",
						},
						{
							ID:   "103",
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

			informerConfig.BackOff = &backoff.ZeroBackOff{}
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

func Test_Informer_ensureProject(t *testing.T) {
	testCases := []struct {
		Projects         []draughtsmantprspec.Project
		Project          draughtsmantprspec.Project
		ExpectedProjects []draughtsmantprspec.Project
		ExpectedUpdated  bool
	}{
		// Test 1
		{
			Projects:         []draughtsmantprspec.Project{},
			Project:          draughtsmantprspec.Project{},
			ExpectedProjects: []draughtsmantprspec.Project{},
			ExpectedUpdated:  false,
		},

		// Test 2
		{
			Projects: []draughtsmantprspec.Project{
				{
					ID:   "api-id-1",
					Name: "api-name",
					Ref:  "api-sha-1",
				},
			},
			Project: draughtsmantprspec.Project{
				ID:   "api-id-1",
				Name: "api-name",
				Ref:  "api-sha-1",
			},
			ExpectedProjects: []draughtsmantprspec.Project{
				{
					ID:   "api-id-1",
					Name: "api-name",
					Ref:  "api-sha-1",
				},
			},
			ExpectedUpdated: false,
		},

		// Test 3
		{
			Projects: []draughtsmantprspec.Project{
				{
					ID:   "api-id-1",
					Name: "api-name",
					Ref:  "api-sha-1",
				},
			},
			Project: draughtsmantprspec.Project{
				ID:   "api-id-2",
				Name: "api-name",
				Ref:  "api-sha-2",
			},
			ExpectedProjects: []draughtsmantprspec.Project{
				{
					ID:   "api-id-2",
					Name: "api-name",
					Ref:  "api-sha-2",
				},
			},
			ExpectedUpdated: true,
		},

		// Test 4
		{
			Projects: []draughtsmantprspec.Project{
				{
					ID:   "api-id-1",
					Name: "api-name",
					Ref:  "api-sha-1",
				},
				{
					ID:   "cluster-service-id-1",
					Name: "cluster-service-name",
					Ref:  "cluster-service-sha-1",
				},
			},
			Project: draughtsmantprspec.Project{
				ID:   "api-id-2",
				Name: "api-name",
				Ref:  "api-sha-2",
			},
			ExpectedProjects: []draughtsmantprspec.Project{
				{
					ID:   "api-id-2",
					Name: "api-name",
					Ref:  "api-sha-2",
				},
				{
					ID:   "cluster-service-id-1",
					Name: "cluster-service-name",
					Ref:  "cluster-service-sha-1",
				},
			},
			ExpectedUpdated: true,
		},
	}

	for i, tc := range testCases {
		projects, updated := ensureProject(tc.Projects, tc.Project)
		if !reflect.DeepEqual(projects, tc.ExpectedProjects) {
			t.Fatalf("test %d expected %#v got %#v", i+1, tc.ExpectedProjects, projects)
		}
		if updated != tc.ExpectedUpdated {
			t.Fatalf("test %d expected %#v got %#v", i+1, tc.ExpectedUpdated, updated)
		}
	}
}

type testTPOController struct {
	EnsureCalled int
	Err          error
	GetCalled    int
	Mutex        sync.Mutex
	TPO          *draughtsmantpr.CustomObject
}

func (c *testTPOController) Ensure(TPO *draughtsmantpr.CustomObject) error {
	c.Mutex.Lock()
	c.TPO = TPO
	c.EnsureCalled++
	c.Mutex.Unlock()
	return c.Err
}

func (c *testTPOController) Get() (*draughtsmantpr.CustomObject, error) {
	c.GetCalled++
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

func (e *testEventer) SetPendingStatus(event eventerspec.DeploymentEvent) error {
	return nil
}
