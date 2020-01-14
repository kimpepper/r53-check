package mock

import (
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/cloudwatch/cloudwatchiface"
)

type CloudwatchClient struct {
	cloudwatchiface.CloudWatchAPI
}

func NewMockCloudwatchClient() *CloudwatchClient {
	return &CloudwatchClient{}
}

func (c *CloudwatchClient) DescribeAlarms(*cloudwatch.DescribeAlarmsInput) (*cloudwatch.DescribeAlarmsOutput, error) {
	return &cloudwatch.DescribeAlarmsOutput{}, nil
}

func (c *CloudwatchClient) PutMetricAlarm(*cloudwatch.PutMetricAlarmInput) (*cloudwatch.PutMetricAlarmOutput, error) {
	return &cloudwatch.PutMetricAlarmOutput{}, nil
}

func (c *CloudwatchClient) DeleteAlarms(*cloudwatch.DeleteAlarmsInput) (*cloudwatch.DeleteAlarmsOutput, error) {
	return &cloudwatch.DeleteAlarmsOutput{}, nil
}
