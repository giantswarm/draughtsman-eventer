package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/giantswarm/microerror"
)

const (
	// deploymentStatusUrlFormat is the string format for the
	// GitHub API call for Deployment Statuses.
	// See: https://developer.github.com/v3/repos/deployments/#create-a-deployment-status
	deploymentStatusUrlFormat = "https://api.github.com/repos/%s/%s/deployments/%v/statuses"

	// etagHeader is the header used for etag.
	// See: https://en.wikipedia.org/wiki/HTTP_ETag.
	etagHeader = "Etag"

	// DeploymentUrlFormat is the format string used to compute the Github
	// API URL used to fetch the latest deployment event for a specific
	// environment.
	DeploymentUrlFormat = "https://api.github.com/repos/%s/%s/deployments"
)

// request makes a request, handling any metrics and logging.
func (e *Eventer) request(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", fmt.Sprintf("token %s", e.oauthToken))

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	// Update rate limit metrics.
	if err := updateRateLimitMetrics(resp); err != nil {
		return nil, microerror.Mask(err)
	}

	return resp, err
}

// filterDeploymentsWithoutStatuses filters out deployments that are finished -
// that is, there exists at least one status that is not pending.
func (e *Eventer) filterDeploymentsWithoutStatuses(deployments []deployment) []deployment {
	matches := []deployment{}

	for _, deployment := range deployments {
		// If there are any statuses apart from pending, we consider the
		// deployment finished, and do not act on it.
		if len(deployment.Statuses) != 0 {
			continue
		}

		matches = append(matches, deployment)
	}

	return matches
}

// fetchNewDeploymentEvents fetches any new GitHub Deployment Events for the
// given project.
func (e *Eventer) fetchNewDeploymentEvents(project, environment string, etagMap map[string]string, filterStatuses bool) ([]deployment, error) {
	var err error

	var u *url.URL
	{
		u, err = url.Parse(fmt.Sprintf(DeploymentUrlFormat, e.organisation, project))
		if err != nil {
			return nil, microerror.Mask(err)
		}
		q := u.Query()
		q.Set("environment", environment)
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	// If we have an etag header for this project, then we have already
	// requested deployment events for it.
	// So, set the header so we only get notified of new events.
	if val, ok := etagMap[project]; ok {
		req.Header.Set("If-None-Match", val)
	}

	startTime := time.Now()

	resp, err := e.request(req)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	defer resp.Body.Close()

	updateDeploymentMetrics(e.organisation, project, resp.StatusCode, startTime)

	// Save the new etag header, so we don't get these deployment events again.
	etagMap[project] = resp.Header.Get(etagHeader)

	if resp.StatusCode == http.StatusNotModified {
		return nil, microerror.Mask(notFoundError)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, microerror.Maskf(unexpectedStatusCode, fmt.Sprintf("received non-200 status code: %v", resp.StatusCode))
	}

	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	var deployments []deployment
	if err := json.Unmarshal(bytes, &deployments); err != nil {
		return nil, microerror.Mask(err)
	}

	for index, deployment := range deployments {
		deploymentStatuses, err := e.fetchDeploymentStatus(project, deployment)
		if err != nil {
			return nil, microerror.Mask(err)
		}

		deployments[index].Statuses = deploymentStatuses
	}

	if filterStatuses {
		deployments = e.filterDeploymentsWithoutStatuses(deployments)
	}

	if len(deployments) == 0 {
		return nil, microerror.Mask(notFoundError)
	}

	return deployments, nil
}

// fetchDeploymentStatus fetches Deployment Statuses for the given Deployment.
func (e *Eventer) fetchDeploymentStatus(project string, deployment deployment) ([]deploymentStatus, error) {
	url := fmt.Sprintf(
		deploymentStatusUrlFormat,
		e.organisation,
		project,
		deployment.ID,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	startTime := time.Now()

	resp, err := e.request(req)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	defer resp.Body.Close()

	updateDeploymentStatusMetrics("GET", e.organisation, project, resp.StatusCode, startTime)

	if resp.StatusCode != http.StatusOK {
		return nil, microerror.Maskf(unexpectedStatusCode, fmt.Sprintf("received non-200 status code: %v", resp.StatusCode))
	}

	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	var deploymentStatuses []deploymentStatus
	if err := json.Unmarshal(bytes, &deploymentStatuses); err != nil {
		return nil, microerror.Mask(err)
	}

	return deploymentStatuses, nil
}

// postDeploymentStatus posts a Deployment Status for the given Deployment.
func (e *Eventer) postDeploymentStatus(project string, id int, state deploymentStatusState) error {
	e.logger.Log("debug", "posting deployment status", "project", project, "id", id, "state", state)

	url := fmt.Sprintf(
		deploymentStatusUrlFormat,
		e.organisation,
		project,
		id,
	)

	status := deploymentStatus{
		State: state,
	}

	payload, err := json.Marshal(status)
	if err != nil {
		return microerror.Mask(err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return microerror.Mask(err)
	}

	startTime := time.Now()

	resp, err := e.request(req)
	if err != nil {
		return microerror.Mask(err)
	}
	defer resp.Body.Close()

	updateDeploymentStatusMetrics("POST", e.organisation, project, resp.StatusCode, startTime)

	if resp.StatusCode != http.StatusCreated {
		return microerror.Maskf(unexpectedStatusCode, fmt.Sprintf("received non-200 status code: %v", resp.StatusCode))
	}

	return nil
}
