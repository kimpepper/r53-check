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
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/go-logr/logr"
	"github.com/go-test/deep"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
	"time"

	route53v1 "github.com/skpr/r53-check/api/v1"
)

// HealthCheckReconciler reconciles a HealthCheck object
type HealthCheckReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
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

	sess, err := session.NewSession(&aws.Config{})
	if err != nil {
		return ctrl.Result{}, err
	}

	r53Client := route53.New(sess)

	callerReference, err := getToken(healthCheck.ObjectMeta.UID)
	if err != nil {
		return ctrl.Result{}, err
	}

	output, err := r53Client.CreateHealthCheck(&route53.CreateHealthCheckInput{
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
		return ctrl.Result{}, err
	}

	name := healthCheck.Spec.NamePrefix + "-" + healthCheck.Name
	_, err = r53Client.ChangeTagsForResource(&route53.ChangeTagsForResourceInput{
		AddTags: []*route53.Tag{
			{Key: aws.String("Name"), Value: &name},
		},
		ResourceId:   output.HealthCheck.Id,
		ResourceType: aws.String(route53.TagResourceTypeHealthcheck),
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	status := route53v1.HealthCheckStatus{
		Id: *output.HealthCheck.Id,
	}

	if diff := deep.Equal(healthCheck.Status, status); diff != nil {
		log.Info(fmt.Sprintf("Status change dectected: %s", diff))
		healthCheck.Status = status
		err := r.Status().Update(ctx, &healthCheck)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	result := ctrl.Result{
		Requeue:      false,
		RequeueAfter: time.Second * 30,
	}

	return result, nil
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
