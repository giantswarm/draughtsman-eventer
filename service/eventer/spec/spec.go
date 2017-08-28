package spec

// EventerType represents the type of Eventer to configure.
type EventerType string

// Eventer represents a Service that checks for deployment events.
type Eventer interface {
	// FetchContinuously returns a channel of DeploymentEvents. This channel can
	// be ranged over to receive DeploymentEvents as they come in. In case of an
	// error during setup, the error will be non-nil.
	FetchContinuously(projects []string, environment string) (<-chan DeploymentEvent, error)
	// FetchLatest returns the latest DeploymentEvent for the given project in the
	// given environment.
	FetchLatest(project, environment string) (DeploymentEvent, error)
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
