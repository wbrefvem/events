/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/drone/go-scm/scm"
	"github.com/go-logr/logr"
	tektonv1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"

	//pipelineclientset "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EventReconciler reconciles a Event object
type EventReconciler struct {
	client.Client
	Log              logr.Logger
	Scheme           *runtime.Scheme
	ScmClient        *scm.Client
	StartupTimestamp time.Time
}

var statusMap = map[string]scm.State{
	"Failed":    scm.StateFailure,
	"Started":   scm.StatePending,
	"Running":   scm.StateRunning,
	"Succeeded": scm.StateSuccess,
}

// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tekton.dev,resources=pipelineruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tekton.dev,resources=pipelineruns/status,verbs=get;update;patch

// Reconcile process Tekton events and updates a Bitbucket Server build status
func (r *EventReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("event", req.NamespacedName)

	event := &corev1.Event{}
	err := r.Client.Get(ctx, req.NamespacedName, event)
	// If the event doesn't exist, don't requeue it
	if err != nil {
		logger.Info(fmt.Sprintf("error getting event: %s", err))
		return ctrl.Result{}, nil
	}

	if event.InvolvedObject.Kind == "PipelineRun" {

		// Idempotence is idemportant :)

		// If an event happened before the controller started, do nothing
		if r.StartupTimestamp.After(event.LastTimestamp.Time) {
			return ctrl.Result{}, nil
		}

		// If we've already processed the event, do nothing.
		eventAnnotations := event.GetAnnotations()
		if processed, ok := eventAnnotations["events.trystero.dev/processed"]; ok == true && processed == "true" {
			return ctrl.Result{}, nil
		}

		// If the event happened before the last event we processed for this PipelineRun, do nothing.
		pipelineRun := &tektonv1beta1.PipelineRun{}
		prName := types.NamespacedName{
			Namespace: event.InvolvedObject.Namespace,
			Name:      event.InvolvedObject.Name,
		}
		err = r.Client.Get(ctx, prName, pipelineRun)
		if err != nil {
			return ctrl.Result{}, err
		}

		prAnnotations := pipelineRun.GetAnnotations()
		if timestamp, ok := prAnnotations["events.trystero.dev/last-event-timestamp"]; ok {
			parsedPRTimestamp, err := time.Parse(time.RFC3339, timestamp)
			if err != nil {
				return ctrl.Result{}, err
			}

			if parsedPRTimestamp.After(event.LastTimestamp.Time) {
				return ctrl.Result{}, nil
			}
		}

		logger.Info(fmt.Sprintf("Processing PipelineRun event for pipelinerun/%s", event.InvolvedObject.Name))

		nameSegments := strings.Split(event.InvolvedObject.Name, "-")
		project := nameSegments[0]
		repo := nameSegments[1]
		prID := nameSegments[2]
		commit := nameSegments[3]

		if len(commit) <= 20 {
			return ctrl.Result{}, nil
		}

		logger.Info(fmt.Sprintf("Creating new build status for %s/%s", project, repo))

		nameKey := fmt.Sprintf("PR-%s-%s", prID, event.InvolvedObject.Name)
		tektonDashboardURL := os.Getenv("TEKTON_DASHBOARD_URL")
		status := &scm.StatusInput{
			State:  statusMap[event.Reason],
			Label:  nameKey,
			Target: fmt.Sprintf("%s/#/namespaces/tekton-pipelines/pipelineruns/%s", tektonDashboardURL, event.InvolvedObject.Name),
			Desc:   event.Message,
		}

		_, _, err = r.ScmClient.Repositories.CreateStatus(ctx, "", commit, status)

		if err != nil {
			return ctrl.Result{}, err
		}

		// Annotate the Event so we don't process it twice.
		annotations := event.GetAnnotations()
		if len(annotations) == 0 {
			annotations = make(map[string]string)
		}
		annotations["events.trystero.dev/processed"] = "true"
		event.SetAnnotations(annotations)
		err = r.Client.Update(ctx, event)
		if err != nil {
			return ctrl.Result{}, err
		}

		// Annotate the PipelineRun with the timestamp of the last Event we processed for it.
		// This will ensure that we don't update a commit with a stale status.
		annotations = pipelineRun.GetAnnotations()
		if len(annotations) == 0 {
			annotations = make(map[string]string)
		}
		annotations["events.trystero.dev/last-event-timestamp"] = event.LastTimestamp.Format(time.RFC3339)
		pipelineRun.SetAnnotations(annotations)
		err = r.Client.Update(ctx, pipelineRun)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager is boilerplate generated by kubebuilder
func (r *EventReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Event{}).
		Complete(r)
}
