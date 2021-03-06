package controllers

import (
	"k8s.io/client-go/kubernetes/scheme"
	"testing"

	healthcheckv1 "github.com/skpr/r53-check/api/v1"
	"github.com/skpr/r53-check/controllers/mock"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestReconcile(t *testing.T) {
	err := healthcheckv1.AddToScheme(scheme.Scheme)
	assert.Nil(t, err)

	logger := zap.New()
	route53Client := mock.NewMockRoute53Client()
	cloudwatchClient := mock.NewMockCloudwatchClient()

	var actions []string
	actions = append(actions, "example.action.arn")

	healthcheck := &healthcheckv1.HealthCheck{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: corev1.NamespaceDefault,
			UID:       types.UID("xxxxxxxxxxxxxxxxxxxxxxxxxxx"),
		},
		Spec: healthcheckv1.HealthCheckSpec{
			NamePrefix:   "example-site.prod",
			Domain:       "test.example.skpr.io",
			Type:         "HTTPS",
			Port:         443,
			ResourcePath: "/healthz",
			AlarmActions: actions,
			OKActions:    actions,
		},
	}

	client := fake.NewFakeClientWithScheme(scheme.Scheme, healthcheck)

	reconciler := HealthCheckReconciler{
		Client:           client,
		Log:              logger,
		Scheme:           scheme.Scheme,
		Route53Client:    route53Client,
		CloudwatchClient: cloudwatchClient,
	}

	query := types.NamespacedName{
		Name:      healthcheck.ObjectMeta.Name,
		Namespace: healthcheck.ObjectMeta.Namespace,
	}

	_, err = reconciler.Reconcile(ctrl.Request{
		NamespacedName: query,
	})

	assert.Nil(t, err)

}
