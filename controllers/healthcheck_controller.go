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
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/go-logr/logr"
	"github.com/go-test/deep"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	route53v1 "github.com/skpr/r53-check/api/v1"
)

const finalizerName = "healthcheck.route53.finalizers.skpr.io"

// HealthCheckReconciler reconciles a HealthCheck object
type HealthCheckReconciler struct {
	client.Client
	Log              logr.Logger
	Scheme           *runtime.Scheme
	Route53Client    *route53.Route53
	CloudwatchClient *cloudwatch.CloudWatch
}

// +kubebuilder:rbac:groups=route53.skpr.io,resources=healthchecks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route53.skpr.io,resources=healthchecks/status,verbs=get;update;patch

func (r *HealthCheckReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("healthcheck", req.NamespacedName)

	var healthCheck route53v1.HealthCheck

	if err := r.Get(ctx, req.NamespacedName, &healthCheck); err != nil {
		log.Error(err, "unable to fetch HealthCheck")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	healthCheckId, err := r.syncHealthCheck(&healthCheck)
	if err != nil {
		return ctrl.Result{}, err
	}

	var alarmName string
	if !healthCheck.Spec.AlarmDisabled {
		alarmName, err = r.syncAlarm(&healthCheck, &healthCheckId)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	err = r.syncStatus(healthCheck, route53v1.HealthCheckStatus{
		HealthCheckId: healthCheckId,
		AlarmName:     alarmName,
	}, ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	if healthCheck.ObjectMeta.DeletionTimestamp.IsZero() {
		// The health check is not being deleted. Register the finalizer.
		if !containsString(healthCheck.ObjectMeta.Finalizers, finalizerName) {
			healthCheck.ObjectMeta.Finalizers = append(healthCheck.ObjectMeta.Finalizers, finalizerName)
			if err := r.Update(context.Background(), &healthCheck); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The health check is being deleted. Handled external resources.
		if containsString(healthCheck.ObjectMeta.Finalizers, finalizerName) {
			// our finalizer is present, so lets handle any external dependency
			if err := r.deleteExternalResources(&healthCheck); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return ctrl.Result{}, err
			}

			// remove our finalizer from the list and update it.
			healthCheck.ObjectMeta.Finalizers = removeString(healthCheck.ObjectMeta.Finalizers, finalizerName)
			if err := r.Update(context.Background(), &healthCheck); err != nil {
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, err
	}

	result := ctrl.Result{
		Requeue:      false,
		RequeueAfter: time.Second * 30,
	}

	return result, nil
}

// deleteExternalResources deletes external resources on health check deletion.
func (r *HealthCheckReconciler) deleteExternalResources(healthCheck *route53v1.HealthCheck) error {
	err := r.deleteAlarms(healthCheck)
	if err != nil {
		return err
	}
	err = r.deleteHealthCheck(healthCheck)
	if err != nil {
		return err
	}

	return nil
}

// deleteAlarm deletes the alarms associated with the health check.
func (r *HealthCheckReconciler) deleteAlarms(healthCheck *route53v1.HealthCheck) error {
	if healthCheck.Status.AlarmName != "" {
		r.Log.Info(fmt.Sprintf("Deleting alarm: %s", healthCheck.Status.AlarmName))
		var alarmNames []*string
		alarmNames = append(alarmNames, &healthCheck.Status.AlarmName)
		_, err := r.CloudwatchClient.DeleteAlarms(&cloudwatch.DeleteAlarmsInput{
			AlarmNames: alarmNames,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// deleteAlarm deletes the alarms associated with the health check.
func (r *HealthCheckReconciler) deleteHealthCheck(healthCheck *route53v1.HealthCheck) error {
	r.Log.Info(fmt.Sprintf("Deleting health check: %s", healthCheck.Status.HealthCheckId))
	_, err := r.Route53Client.DeleteHealthCheck(&route53.DeleteHealthCheckInput{
		HealthCheckId: &healthCheck.Status.HealthCheckId,
	})
	return err
}

// syncStatus syncs the health check status.
func (r *HealthCheckReconciler) syncStatus(healthCheck route53v1.HealthCheck, status route53v1.HealthCheckStatus, ctx context.Context) error {
	if diff := deep.Equal(healthCheck.Status, status); diff != nil {
		r.Log.Info(fmt.Sprintf("Status change dectected: %s", diff))
		healthCheck.Status = status
		err := r.Status().Update(ctx, &healthCheck)
		if err != nil {
			return err
		}
	}
	return nil
}

// syncHealthCheck syncs a health check.
func (r *HealthCheckReconciler) syncHealthCheck(healthCheck *route53v1.HealthCheck) (string, error) {
	callerReference, err := getToken(healthCheck.ObjectMeta.UID)
	if err != nil {
		return "", err
	}

	output, err := r.Route53Client.CreateHealthCheck(&route53.CreateHealthCheckInput{
		CallerReference: &callerReference,
		HealthCheckConfig: &route53.HealthCheckConfig{
			Type:                     &healthCheck.Spec.Type,
			FullyQualifiedDomainName: &healthCheck.Spec.Domain,
			Port:                     &healthCheck.Spec.Port,
			ResourcePath:             &healthCheck.Spec.ResourcePath,
			EnableSNI:                aws.Bool(true),
			Disabled:                 &healthCheck.Spec.Disabled,
		},
	})
	if err != nil {
		return "", err
	}

	// Health Check 'Name' is a tag.
	healthCheckId := *output.HealthCheck.Id
	_, err = r.Route53Client.ChangeTagsForResource(&route53.ChangeTagsForResourceInput{
		AddTags: []*route53.Tag{
			{Key: aws.String("Name"), Value: aws.String(getHealthCheckName(healthCheck))},
		},
		ResourceId:   &healthCheckId,
		ResourceType: aws.String(route53.TagResourceTypeHealthcheck),
	})
	if err != nil {
		return "", err
	}
	return healthCheckId, nil
}

// syncAlarm Syncs an alarm for the health check.
func (r *HealthCheckReconciler) syncAlarm(healthCheck *route53v1.HealthCheck, healthCheckId *string) (string, error) {

	var alarmActions []*string
	for _, action := range healthCheck.Spec.AlarmActions {
		alarmActions = append(alarmActions, &action)
	}
	_, err := r.CloudwatchClient.PutMetricAlarm(&cloudwatch.PutMetricAlarmInput{
		AlarmName:          aws.String(getAlarmName(healthCheck)),
		AlarmDescription:   aws.String("Route53 HealthCheck alarm for " + getHealthCheckName(healthCheck)),
		AlarmActions:       alarmActions,
		Period:             aws.Int64(60),
		EvaluationPeriods:  aws.Int64(1),
		Threshold:          aws.Float64(1.0),
		ComparisonOperator: aws.String("LessThanThreshold"),
		Namespace:          aws.String("AWS/Route53"),
		MetricName:         aws.String("HealthCheckStatus"),
		Statistic:          aws.String("Minimum"),
		Dimensions: []*cloudwatch.Dimension{
			{
				Name:  aws.String("HealthCheckId"),
				Value: healthCheckId,
			},
		},
	})
	if err != nil {
		return "", err
	}
	return getAlarmName(healthCheck), nil
}

func (r *HealthCheckReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&route53v1.HealthCheck{}).
		Complete(r)
}

// GetToken converts a Kubernetes UID into a 32 character which can be used as a token.
//   eg. AWS Certificate Requests require a 32 character idempotency token.
func getToken(uid types.UID) (string, error) {
	token := strings.ReplaceAll(string(uid), "-", "")

	if len(token) > 32 {
		return "", fmt.Errorf("token is greater than 32 characters: %s", token)
	}

	return token, nil
}

// getHealthCheckName gets the healthcheck name.
func getHealthCheckName(healthCheck *route53v1.HealthCheck) string {
	return healthCheck.Spec.NamePrefix + "-" + healthCheck.Name
}

// getHealthCheckName gets the healthcheck name.
func getAlarmName(healthCheck *route53v1.HealthCheck) string {
	return getHealthCheckName(healthCheck) + "-healthcheck"
}

// Helper functions to check and remove string from a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}
