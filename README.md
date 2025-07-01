# ECS Service AutoScaler

A GitHub Action to register/deregister AWS ECS Service auto-scaling and CloudWatch alarms.
Written in Go and published as a Docker-based action.

## Features

- Built-in CPU and Memory **step-scaling** + CloudWatch alarms (default)  
- Custom JSON-defined **StepScaling** and **TargetTrackingScaling** policies  
- Support for **predefined** or **custom** metrics, cooldowns, and thresholds  
- Default policies support for common scaling patterns

## Quick Start

Here's the simplest way to enable auto-scaling for your ECS service:

```yaml
jobs:
  scale:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: aws-actions/configure-aws-credentials@v2
        with:
          aws-region: us-east-1
          role-to-assume: ${{ secrets.AWS_ROLE_ARN }}

      - name: Enable Auto-Scaling
        uses: cheelim1/ecs-autoscaler@v0.1.14
        with:
          aws-region: us-east-1
          cluster-name: my-cluster
          service-name: my-service
          enabled: true
```

This will enable auto-scaling with default settings:
- Min capacity: 1
- Max capacity: 10
- CPU threshold: 75%
- Memory threshold: 80%
- Cooldown periods: 300 seconds

## Usage Examples

### 1. Basic Auto-Scaling with Custom Thresholds

```yaml
      - name: Configure Auto-Scaling
        uses: cheelim1/ecs-autoscaler@v0.1.14
        with:
          aws-region: us-east-1
          cluster-name: my-cluster
          service-name: my-service
          enabled: true
          min-capacity: 2
          max-capacity: 10
          target-cpu-utilization-out: 80
          target-cpu-utilization-in: 60
          target-memory-utilization-out: 85
          target-memory-utilization-in: 65
```

### 2. Using Target Tracking Scaling

```yaml
      - name: Configure Target Tracking
        uses: cheelim1/ecs-autoscaler@v0.1.14
        with:
          aws-region: us-east-1
          cluster-name: my-cluster
          service-name: my-service
          enabled: true
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
```

### 3. Using Custom Metrics

```yaml
      - name: Configure Custom Metric Scaling
        uses: cheelim1/ecs-autoscaler@v0.1.14
        with:
          aws-region: us-east-1
          cluster-name: my-cluster
          service-name: my-service
          enabled: true
          scaling-policies: >
            [
              {
                "policy_name": "custom-metric",
                "policy_type": "TargetTrackingScaling",
                "target_tracking_configuration": {
                  "target_value": 60.0,
                  "custom_metric_specification": {
                    "namespace": "MyNamespace",
                    "metric_name": "MyMetric",
                    "dimensions": {
                      "Dimension1": "Value1"
                    },
                    "statistic": "Average"
                  },
                  "scale_in_cooldown": 200,
                  "scale_out_cooldown": 200
                }
              }
            ]
```

## Input Parameters

### Required Parameters

| Parameter | Description |
|-----------|-------------|
| `aws-region` | AWS region (e.g., us-east-1) |
| `cluster-name` | ECS cluster name |
| `service-name` | ECS service name |
| `enabled` | Set to `true` to enable auto-scaling, `false` to disable |

### Optional Parameters

#### Basic Configuration
| Parameter | Description | Default |
|-----------|-------------|---------|
| `min-capacity` | Minimum desired count | 1 |
| `max-capacity` | Maximum desired count | 10 |
| `scale-out-cooldown` | Scale-out cooldown in seconds | 300 |
| `scale-in-cooldown` | Scale-in cooldown in seconds | 300 |
| `target-cpu-utilization-out` | CPU% threshold for scale-out | 75 |
| `target-cpu-utilization-in` | CPU% threshold for scale-in | 65 |
| `target-memory-utilization-out` | Memory% threshold for scale-out | 80 |
| `target-memory-utilization-in` | Memory% threshold for scale-in | 70 |

#### Example: Different thresholds for up and down (CPU and Memory)

```yaml
      - name: Configure Auto-Scaling
        uses: cheelim1/ecs-autoscaler@v0.1.14
        with:
          aws-region: us-east-1
          cluster-name: my-cluster
          service-name: my-service
          enabled: true
          min-capacity: 2
          max-capacity: 10
          target-cpu-utilization-out: 80
          target-cpu-utilization-in: 60
          target-memory-utilization-out: 85
          target-memory-utilization-in: 65
```

- `target-cpu-utilization-out` is used for the scale-out alarm (e.g., CPU >= 80 triggers scale out)
- `target-cpu-utilization-in` is used for the scale-in alarm (e.g., CPU <= 60 triggers scale in)
- `target-memory-utilization-out` is used for the scale-out alarm (e.g., Memory >= 85 triggers scale out)
- `target-memory-utilization-in` is used for the scale-in alarm (e.g., Memory <= 65 triggers scale in)

#### Advanced Configuration
| Parameter | Description | Default |
|-----------|-------------|---------|
| `default-policies` | JSON array of default policies | "" |
| `scaling-policies` | JSON array of custom policies | "" |

### AWS Credentials
You can provide AWS credentials in two ways:

1. Using IAM Role (Recommended):
```yaml
      - uses: aws-actions/configure-aws-credentials@v2
        with:
          aws-region: us-east-1
          role-to-assume: ${{ secrets.AWS_ROLE_ARN }}
```

2. Using Access Keys:
```yaml
      - uses: aws-actions/configure-aws-credentials@v2
        with:
          aws-access-key-id: ${{ secrets.AWS_ACCESS_KEY_ID }}
          aws-secret-access-key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          aws-region: us-east-1
```

## Policy Types

### 1. Step Scaling
Use this when you want to scale based on specific thresholds:

```json
{
  "policy_name": "cpu-step",
  "policy_type": "StepScaling",
  "adjustment_type": "ChangeInCapacity",
  "cooldown": 300,
  "metric_aggregation_type": "Maximum",
  "step_adjustments": [
    {"MetricIntervalLowerBound": 0, "ScalingAdjustment": 1}
  ]
}
```

### 2. Target Tracking
Use this when you want to maintain a specific metric value:

```json
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
```

## Alarm Creation Logic

- **Default step scaling (no custom policies):**
  - The action will always create and attach CloudWatch alarms for CPU and memory utilization (high/low) to the scaling policies.
- **Custom scaling policies:**
  - The action will only create and attach a CloudWatch alarm if both `metric_name` and `metric_namespace` are provided in the policy JSON.
  - If these fields are not provided, no alarm will be created for that policy (you are responsible for alarm management).

### Example: Custom Policy With Alarm Creation

```yaml
      - name: Configure Custom Policy With Alarm
        uses: cheelim1/ecs-autoscaler@v0.1.14
        with:
          aws-region: us-east-1
          cluster-name: my-cluster
          service-name: my-service
          enabled: true
          target-cpu-utilization-in: 20
          target-cpu-utilization-out: 80
          scaling-policies: >
            [
              {
                "policy_name": "custom-cpu-scale-in",
                "scale_direction": "in",
                "policy_type": "StepScaling",
                "adjustment_type": "ChangeInCapacity",
                "cooldown": 300,
                "metric_aggregation_type": "Maximum",
                "metric_name": "CPUUtilization",
                "metric_namespace": "AWS/ECS",
                "step_adjustments": [
                  {"MetricIntervalUpperBound": 0, "ScalingAdjustment": -1}
                ]
              },
              {
                "policy_name": "custom-cpu-scale-out",
                "scale_direction": "out",
                "policy_type": "StepScaling",
                "adjustment_type": "ChangeInCapacity",
                "cooldown": 300,
                "metric_aggregation_type": "Maximum",
                "metric_name": "CPUUtilization",
                "metric_namespace": "AWS/ECS",
                "step_adjustments": [
                  {"MetricIntervalLowerBound": 0, "ScalingAdjustment": 1}
                ]
              }
            ]
```

**Note:**
- The `scale_direction` field ("in" or "out") explicitly controls which threshold is used for the alarm:
  - `scale_direction: "in"` uses `target-cpu-utilization-in` as the alarm threshold.
  - `scale_direction: "out"` uses `target-cpu-utilization-out` as the alarm threshold.
- This is the recommended, explicit, and robust way to control alarm thresholds for custom policies.

### Example: Custom Policy Without Alarm Creation

```yaml
      - name: Configure Custom Policy Without Alarm
        uses: cheelim1/ecs-autoscaler@v0.1.14
        with:
          aws-region: us-east-1
          cluster-name: my-cluster
          service-name: my-service
          enabled: true
          scaling-policies: >
            [
              {
                "policy_name": "custom-step-no-alarm",
                "policy_type": "StepScaling",
                "adjustment_type": "ChangeInCapacity",
                "cooldown": 300,
                "metric_aggregation_type": "Maximum",
                "step_adjustments": [
                  {"MetricIntervalLowerBound": 0, "ScalingAdjustment": 1}
                ]
              }
            ]
```

In the second example, **no alarm will be created or attached** for the custom policy.
