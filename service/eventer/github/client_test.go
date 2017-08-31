package github

import (
	"reflect"
	"testing"
)

// TestFilterDeploymentsWithoutStatuses tests the
// filterDeploymentsWithoutStatuses method.
func TestFilterDeploymentsWithoutStatuses(t *testing.T) {
	tests := []struct {
		deployments         []deployment
		expectedDeployments []deployment
	}{
		// Test that no deployments filters to no deployments.
		{
			deployments:         []deployment{},
			expectedDeployments: []deployment{},
		},

		// Test that a pending deployment is kept.
		{
			deployments: []deployment{
				{
					Statuses: []deploymentStatus{
						{State: pendingState},
					},
				},
			},
			expectedDeployments: []deployment{},
		},

		// Test that a success only deployment is not kept.
		{
			deployments: []deployment{
				{
					Statuses: []deploymentStatus{
						{State: successState},
					},
				},
			},
			expectedDeployments: []deployment{},
		},

		// Test that a failure only deployment is not kept.
		{
			deployments: []deployment{
				{
					Statuses: []deploymentStatus{
						{State: failureState},
					},
				},
			},
			expectedDeployments: []deployment{},
		},

		// Test that a deployment that was pending, and is now successful, is not kept.
		{
			deployments: []deployment{
				{
					Statuses: []deploymentStatus{
						{State: pendingState},
						{State: successState},
					},
				},
			},
			expectedDeployments: []deployment{},
		},

		// Test that a deployment that was pending, and is now failed, is not kept.
		{
			deployments: []deployment{
				{
					Statuses: []deploymentStatus{
						{State: pendingState},
						{State: failureState},
					},
				},
			},
			expectedDeployments: []deployment{},
		},

		// Test that deployments that do not have statuses are returned.
		{
			deployments: []deployment{
				{
					Statuses: []deploymentStatus{},
				},
				{
					Statuses: []deploymentStatus{},
				},
			},
			expectedDeployments: []deployment{
				{
					Statuses: []deploymentStatus{},
				},
				{
					Statuses: []deploymentStatus{},
				},
			},
		},
	}

	for index, test := range tests {
		e := Eventer{}

		returnedDeployments := e.filterDeploymentsWithoutStatuses(test.deployments)

		if !reflect.DeepEqual(test.expectedDeployments, returnedDeployments) {
			t.Fatalf(
				"%v\nexpected: %#v\nreturned: %#v\n",
				index, test.expectedDeployments, returnedDeployments,
			)
		}
	}
}
