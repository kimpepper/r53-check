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
	"github.com/aws/aws-sdk-go/service/cloudwatch/cloudwatchiface"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/aws/aws-sdk-go/service/route53/route53iface"
	"github.com/go-logr/logr"
	"github.com/go-test/deep"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	healthcheckv1 "github.com/skpr/r53-check/api/v1"
)

const finalizerName = "healthcheck.route53.finalizers.skpr.io"

// HealthCheckReconciler reconciles a HealthCheck object
type HealthCheckReconciler struct {
	client.Client
	Log              logr.Logger
	Scheme           *runtime.Scheme
	Route53Client    route53iface.Route53API
	CloudwatchClient cloudwatchiface.CloudWatchAPI
}

// +kubebuilder:rbac:groups=route53.skpr.io,resources=healthchecks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route53.skpr.io,resources=healthchecks/status,verbs=get;update;patch

func (r *HealthCheckReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()

	healthCheck := &healthcheckv1.HealthCheck{}

	if err := r.Get(ctx, req.NamespacedName, healthCheck); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if healthCheck.ObjectMeta.DeletionTimestamp.IsZero() {
		// The health check is not being deleted. Register the finalizer.
		if !containsString(healthCheck.ObjectMeta.Finalizers, finalizerName) {
			healthCheck.ObjectMeta.Finalizers = append(healthCheck.ObjectMeta.Finalizers, finalizerName)
			if err := r.Update(context.Background(), healthCheck); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to add finalizer %w", err)
			}
		}
	} else {
		// The health check is being deleted. Handled external resources.
		if containsString(healthCheck.ObjectMeta.Finalizers, finalizerName) {
			// our finalizer is present, so lets handle any external dependency
			if err := r.deleteExternalResources(healthCheck); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return ctrl.Result{}, fmt.Errorf("failed to delete exeternal resources %w", err)
			}

			// remove our finalizer from the list and update it.
			healthCheck.ObjectMeta.Finalizers = removeString(healthCheck.ObjectMeta.Finalizers, finalizerName)
			if err := r.Update(context.Background(), healthCheck); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to removed finalizer %w", err)
			}
		}

		return ctrl.Result{}, nil
	}

	healthCheckId, err := r.syncHealthCheck(healthCheck)
	if err != nil {
		return ctrl.Result{}, err
	}

	alarmName, err := r.syncAlarm(healthCheck, healthCheckId)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Get alarm state.
	alarmState, err := r.getAlarmState(alarmName)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.syncStatus(healthCheck, healthcheckv1.HealthCheckStatus{
		HealthCheckId: healthCheckId,
		AlarmName:     alarmName,
		AlarmState:    alarmState,
	}, ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to sync status %v %w", healthCheck, err)
	}

	result := ctrl.Result{
		Requeue:      false,
		RequeueAfter: time.Second * 30,
	}

	return result, nil
}

// getAlarmState gets the current alarm state.
func (r *HealthCheckReconciler) getAlarmState(alarmName string) (string, error) {
	var alarmNames []*string
	alarmNames = append(alarmNames, &alarmName)
	output, err := r.CloudwatchClient.DescribeAlarms(&cloudwatch.DescribeAlarmsInput{
		AlarmNames: alarmNames,
		MaxRecords: aws.Int64(1),
	})
	if err != nil {
		return "", err
	}
	for _, alarm := range output.MetricAlarms {
		return *alarm.StateValue, nil
	}
	return "", nil
}

// deleteExternalResources deletes external resources on health check deletion.
func (r *HealthCheckReconciler) deleteExternalResources(healthCheck *healthcheckv1.HealthCheck) error {
	err := r.deleteAlarm(healthCheck)
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
func (r *HealthCheckReconciler) deleteHealthCheck(healthCheck *healthcheckv1.HealthCheck) error {
	r.Log.Info(fmt.Sprintf("Deleting health check: %s", healthCheck.Status.HealthCheckId))
	_, err := r.Route53Client.DeleteHealthCheck(&route53.DeleteHealthCheckInput{
		HealthCheckId: &healthCheck.Status.HealthCheckId,
	})
	return err
}

// syncStatus syncs the health check status.
func (r *HealthCheckReconciler) syncStatus(healthCheck *healthcheckv1.HealthCheck, status healthcheckv1.HealthCheckStatus, ctx context.Context) error {
	if diff := deep.Equal(healthCheck.Status, status); diff != nil {
		r.Log.Info(fmt.Sprintf("Status change dectected: %s", diff))
		healthCheck.Status = status
		err := r.Status().Update(ctx, healthCheck)
		if err != nil {
			return err
		}
	}
	return nil
}

// syncHealthCheck syncs a health check.
func (r *HealthCheckReconciler) syncHealthCheck(healthCheck *healthcheckv1.HealthCheck) (string, error) {
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

// syncAlarm syncs the health check alarm.
func (r *HealthCheckReconciler) syncAlarm(healthCheck *healthcheckv1.HealthCheck, healthCheckId string) (string, error) {
	var (
		alarmName string
		err       error
	)
	if healthCheck.Spec.AlarmDisabled {
		err = r.deleteAlarm(healthCheck)
	} else {
		alarmName, err = r.createAlarm(healthCheck, healthCheckId)
	}
	if err != nil {
		return "", err
	}
	return alarmName, nil
}

// createAlarm creates an alarm for the health check.
func (r *HealthCheckReconciler) createAlarm(healthCheck *healthcheckv1.HealthCheck, healthCheckId string) (string, error) {

	var alarmActions, okActions []*string
	for _, action := range healthCheck.Spec.AlarmActions {
		alarmActions = append(alarmActions, &action)
	}
	for _, action := range healthCheck.Spec.OKActions {
		okActions = append(okActions, &action)
	}
	_, err := r.CloudwatchClient.PutMetricAlarm(&cloudwatch.PutMetricAlarmInput{
		AlarmName:          aws.String(getAlarmName(healthCheck)),
		AlarmDescription:   aws.String("Route53 HealthCheck alarm for " + getHealthCheckName(healthCheck)),
		AlarmActions:       alarmActions,
		OKActions:          okActions,
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
				Value: aws.String(healthCheckId),
			},
		},
	})
	if err != nil {
		return "", err
	}
	return getAlarmName(healthCheck), nil
}

// deleteAlarm deletes the alarms associated with the health check.
func (r *HealthCheckReconciler) deleteAlarm(healthCheck *healthcheckv1.HealthCheck) error {
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

func (r *HealthCheckReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&healthcheckv1.HealthCheck{}).
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
func getHealthCheckName(healthCheck *healthcheckv1.HealthCheck) string {
	return healthCheck.Spec.NamePrefix + "-" + healthCheck.Name
}

// getHealthCheckName gets the healthcheck name.
func getAlarmName(healthCheck *healthcheckv1.HealthCheck) string {
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
