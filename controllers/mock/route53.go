package mock

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/aws/aws-sdk-go/service/route53/route53iface"
)

type Route53Client struct {
	route53iface.Route53API
}

func NewMockRoute53Client() *Route53Client {
	return &Route53Client{}
}

func (r *Route53Client) CreateHealthCheck(*route53.CreateHealthCheckInput) (*route53.CreateHealthCheckOutput, error) {
	return &route53.CreateHealthCheckOutput{
		HealthCheck: &route53.HealthCheck{
			Id: aws.String("abcdefg"),
		},
	}, nil
}

func (r *Route53Client) ChangeTagsForResource(*route53.ChangeTagsForResourceInput) (*route53.ChangeTagsForResourceOutput, error) {
	return &route53.ChangeTagsForResourceOutput{}, nil
}

func (r *Route53Client) DeleteHealthCheck(*route53.DeleteHealthCheckInput) (*route53.DeleteHealthCheckOutput, error) {
	return &route53.DeleteHealthCheckOutput{}, nil
}
