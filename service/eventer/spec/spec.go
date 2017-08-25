package spec

// EventerType represents the type of Eventer to configure.
type EventerType string

// Eventer represents a Service that checks for deployment events.
type Eventer interface {
	// Boot initializes the eventer and makes it run. The eventer decides if the
	// TPO it watches should be initialized and/or updated using
	// NewDeploymentEvents.
	Boot()
	// NewDeploymentEvents returns a channel of DeploymentEvents. This channel can
	// be ranged over to receive DeploymentEvents as they come in. In case of
	// error during setup, the error will be non-nil.
	NewDeploymentEvents() (<-chan DeploymentEvent, error)
}

// DeploymentEvent represents a request for a chart to be deployed.
type DeploymentEvent struct {
	// ID is an identifier for the deployment event.
	ID int
	// Name is the name of the project of the chart to deploy, e.g: aws-operator.
	Name string
	// Sha is the version of the chart to deploy.
	Sha string
}
