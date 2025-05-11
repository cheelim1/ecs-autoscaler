# ECS AutoScaler

A GitHub Action to register/deregister AWS ECS Service auto-scaling and CloudWatch alarms.
Written in Go and published as a Docker-based action.

## Features

- Built-in CPU and Memory **step-scaling** + CloudWatch alarms (default)  
- Custom JSON-defined **StepScaling** and **TargetTrackingScaling** policies  
- Support for **predefined** or **custom** metrics, cooldowns, and thresholds  
- Default policies support for common scaling patterns

## Usage

```yaml
jobs:
  scale:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - uses: aws-actions/configure-aws-credentials@v2
        with:
          aws-region: us-east-1
          role-to-assume: ${{ secrets.AWS_ROLE_ARN }}

      - name: ECS Auto-Scale
        uses: your-org/ecs-autoscaler@v1
        with:
          aws-region: us-east-1
          cluster-name: my-cluster
          service-name: my-service
          enabled: true

          # ⬇️ Optional built-in step-scaling parameters:
          min-capacity: 2
          max-capacity: 10
          scale-out-cooldown: 300
          scale-in-cooldown: 300
          target-cpu-utilization: 75
          target-memory-utilization: 80

          # ⬇️ Optional default policies (used if no custom policies):
          default-policies: >
            [
              {
                "policy_name": "cpu-target",
                "policy_type": "TargetTrackingScaling",
                "target_tracking_configuration": {
                  "target_value": 75.0,
                  "predefined_metric_specification": "ECSServiceAverageCPUUtilization",
                  "scale_in_cooldown": 300,
                  "scale_out_cooldown": 300
                }
              }
            ]

          # ⬇️ Optional custom policies (overrides built-in):
          scaling-policies: >
            [
              {
                "policy_name":"cpu-step",
                "policy_type":"StepScaling",
                "adjustment_type":"ChangeInCapacity",
                "cooldown":300,
                "metric_aggregation_type":"Maximum",
                "step_adjustments":[
                  {"MetricIntervalLowerBound":0,"ScalingAdjustment":1}
                ]
              },
              {
                "policy_name":"mem-target",
                "policy_type":"TargetTrackingScaling",
                "target_tracking_configuration":{
                  "target_value":60,
                  "predefined_metric_specification":"ECSServiceAverageMemoryUtilization",
                  "scale_in_cooldown":200,
                  "scale_out_cooldown":200
                }
              }
            ]
```          

## Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `aws-access-key-id` | AWS_ACCESS_KEY_ID (omit to use IAM role) | No | - |
| `aws-secret-access-key` | AWS_SECRET_ACCESS_KEY (omit to use IAM role) | No | - |
| `aws-region` | AWS region, e.g. us-east-1 | Yes | - |
| `cluster-name` | ECS cluster name | Yes | - |
| `service-name` | ECS service name | Yes | - |
| `enabled` | Enable auto-scaling? (`true` or `false`) | Yes | - |
| `min-capacity` | Minimum desired count (used only when no custom policies) | No | 1 |
| `max-capacity` | Maximum desired count (used only when no custom policies) | No | 10 |
| `scale-out-cooldown` | Scale-out cooldown in seconds (only default step-scaling) | No | 300 |
| `scale-in-cooldown` | Scale-in cooldown in seconds (only default step-scaling) | No | 300 |
| `target-cpu-utilization` | CPU% threshold (only default step-scaling) | No | 75 |
| `target-memory-utilization` | Memory% threshold (only default step-scaling) | No | 80 |
| `default-policies` | Optional JSON array of default policies to use if no custom policies | No | "" |
| `scaling-policies` | Optional JSON array of custom policies (overrides built-in) | No | "" |

### Example Custom Scaling Policies

```json
[
  {
    "policy_name": "cpu-step",
    "policy_type": "StepScaling",
    "adjustment_type": "ChangeInCapacity",
    "cooldown": 300,
    "metric_aggregation_type": "Maximum",
    "step_adjustments": [
      {"MetricIntervalLowerBound": 0, "ScalingAdjustment": 1}
    ]
  },
  {
    "policy_name": "mem-target",
    "policy_type": "TargetTrackingScaling",
    "target_tracking_configuration": {
      "target_value": 60.0,
      "predefined_metric_specification": "ECSServiceAverageMemoryUtilization",
      "scale_in_cooldown": 200,
      "scale_out_cooldown": 200
    }
  }
]
```

### Custom Metric Support

For TargetTrackingScaling policies, you can also use custom metrics:

```json
{
  "policy_name": "custom-metric",
  "policy_type": "TargetTrackingScaling",
  "target_tracking_configuration": {
    "target_value": 60.0,
    "custom_metric_specification": {
      "namespace": "MyNamespace",
      "metric_name": "MyMetric",
      "dimensions": {
        "Dimension1": "Value1",
        "Dimension2": "Value2"
      },
      "statistic": "Average"
    },
    "scale_in_cooldown": 200,
    "scale_out_cooldown": 200
  }
}
```

### Local Testing

```bash
go fmt ./...
go clean -cache -testcache
go test ./... -v
```