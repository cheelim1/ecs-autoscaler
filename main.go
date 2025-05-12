package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	aas "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling"
	aasTypes "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"
	cw "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwTypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

// Define interfaces for AWS clients
type AASClient interface {
	DescribeScalableTargets(ctx context.Context, params *aas.DescribeScalableTargetsInput, optFns ...func(*aas.Options)) (*aas.DescribeScalableTargetsOutput, error)
	DescribeScalingPolicies(ctx context.Context, params *aas.DescribeScalingPoliciesInput, optFns ...func(*aas.Options)) (*aas.DescribeScalingPoliciesOutput, error)
}

type CWClient interface {
	DescribeAlarms(ctx context.Context, params *cw.DescribeAlarmsInput, optFns ...func(*cw.Options)) (*cw.DescribeAlarmsOutput, error)
}

// Set up structured logging with slog
func init() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)
}

type StepAdj struct {
	MetricIntervalLowerBound *float64 `json:"MetricIntervalLowerBound,omitempty"`
	MetricIntervalUpperBound *float64 `json:"MetricIntervalUpperBound,omitempty"`
	ScalingAdjustment        int32    `json:"ScalingAdjustment"`
}

type CustomMetricSpec struct {
	Namespace  string            `json:"namespace"`
	MetricName string            `json:"metric_name"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
	Statistic  string            `json:"statistic"`
}

type TargetTrackingConfig struct {
	TargetValue                   float64           `json:"target_value"`
	PredefinedMetricSpecification string            `json:"predefined_metric_specification,omitempty"`
	CustomMetricSpecification     *CustomMetricSpec `json:"custom_metric_specification,omitempty"`
	ScaleInCooldown               *int32            `json:"scale_in_cooldown,omitempty"`
	ScaleOutCooldown              *int32            `json:"scale_out_cooldown,omitempty"`
}

type PolicyDef struct {
	PolicyName                  string                `json:"policy_name"`
	PolicyType                  string                `json:"policy_type"` // StepScaling or TargetTrackingScaling
	MetricName                  string                `json:"metric_name,omitempty"`
	MetricNamespace             string                `json:"metric_namespace,omitempty"`
	AdjustmentType              string                `json:"adjustment_type,omitempty"`
	Cooldown                    *int32                `json:"cooldown,omitempty"`
	MetricAggregationType       string                `json:"metric_aggregation_type,omitempty"`
	StepAdjustments             []StepAdj             `json:"step_adjustments,omitempty"`
	TargetTrackingConfiguration *TargetTrackingConfig `json:"target_tracking_configuration,omitempty"`
}

func getIntWithDefault(arg, name string, defaultValue int) (int, error) {
	if arg == "" {
		return defaultValue, nil
	}
	i, err := strconv.Atoi(arg)
	if err != nil {
		slog.Error("invalid input", "name", name, "value", arg, "error", err)
		return 0, fmt.Errorf("invalid %s: %v", name, err)
	}
	return i, nil
}

func getFloatWithDefault(arg, name string, defaultValue float64) (float64, error) {
	if arg == "" {
		return defaultValue, nil
	}
	f, err := strconv.ParseFloat(arg, 64)
	if err != nil {
		slog.Error("invalid input", "name", name, "value", arg, "error", err)
		return 0, fmt.Errorf("invalid %s: %v", name, err)
	}
	return f, nil
}

// Check if scalable target exists and matches desired configuration
func checkScalableTarget(ctx context.Context, client AASClient, resourceID string, minCap, maxCap int32) (bool, error) {
	resp, err := client.DescribeScalableTargets(ctx, &aas.DescribeScalableTargetsInput{
		ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
		ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
		ResourceIds:       []string{resourceID},
	})
	if err != nil {
		return false, fmt.Errorf("failed to describe scalable target: %v", err)
	}

	if len(resp.ScalableTargets) == 0 {
		return false, nil
	}

	target := resp.ScalableTargets[0]
	return *target.MinCapacity == minCap && *target.MaxCapacity == maxCap, nil
}

// Check if scaling policy exists and matches desired configuration
func checkScalingPolicy(ctx context.Context, client AASClient, resourceID, policyName string) (bool, error) {
	resp, err := client.DescribeScalingPolicies(ctx, &aas.DescribeScalingPoliciesInput{
		ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
		ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
		ResourceId:        aws.String(resourceID),
		PolicyNames:       []string{policyName},
	})
	if err != nil {
		return false, fmt.Errorf("failed to describe scaling policy: %v", err)
	}

	return len(resp.ScalingPolicies) > 0, nil
}

// Check if CloudWatch alarm exists
func checkCloudWatchAlarm(ctx context.Context, client CWClient, alarmName string) (bool, error) {
	resp, err := client.DescribeAlarms(ctx, &cw.DescribeAlarmsInput{
		AlarmNames: []string{alarmName},
	})
	if err != nil {
		return false, fmt.Errorf("failed to describe alarm: %v", err)
	}

	return len(resp.MetricAlarms) > 0, nil
}

func main() {
	// we expect 14 args after program name
	if len(os.Args) != 17 {
		slog.Error("invalid number of arguments", "expected", 16, "got", len(os.Args)-1)
		os.Exit(1)
	}

	keyID := os.Args[1]
	keySecret := os.Args[2]
	region := os.Args[3]
	cluster := os.Args[4]
	service := os.Args[5]
	enabled := os.Args[6] == "true"

	minCap, err := getIntWithDefault(os.Args[7], "min-capacity", 1)
	if err != nil {
		os.Exit(1)
	}
	maxCap, err := getIntWithDefault(os.Args[8], "max-capacity", 10)
	if err != nil {
		os.Exit(1)
	}
	outCd, err := getIntWithDefault(os.Args[9], "scale-out-cooldown", 300)
	if err != nil {
		os.Exit(1)
	}
	inCd, err := getIntWithDefault(os.Args[10], "scale-in-cooldown", 300)
	if err != nil {
		os.Exit(1)
	}

	// Convert to int32 for AWS SDK
	minCap32 := int32(minCap)
	maxCap32 := int32(maxCap)
	outCd32 := int32(outCd)
	inCd32 := int32(inCd)

	targetCPUUp, err := getFloatWithDefault(os.Args[11], "target-cpu-utilization-up", 75.0)
	if err != nil {
		os.Exit(1)
	}
	targetCPUDown, err := getFloatWithDefault(os.Args[12], "target-cpu-utilization-down", 65.0)
	if err != nil {
		os.Exit(1)
	}
	targetMemUp, err := getFloatWithDefault(os.Args[13], "target-memory-utilization-up", 80.0)
	if err != nil {
		os.Exit(1)
	}
	targetMemDown, err := getFloatWithDefault(os.Args[14], "target-memory-utilization-down", 70.0)
	if err != nil {
		os.Exit(1)
	}
	defaultPoliciesRaw := os.Args[15]
	policiesRaw := os.Args[16]

	// AWS config
	var cfg aws.Config
	if keyID != "" && keySecret != "" {
		cfg, err = config.LoadDefaultConfig(context.TODO(),
			config.WithRegion(region),
			config.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(keyID, keySecret, ""),
			),
		)
	} else {
		cfg, err = config.LoadDefaultConfig(context.TODO(),
			config.WithRegion(region),
		)
	}
	if err != nil {
		slog.Error("loading AWS config", "error", err)
		os.Exit(1)
	}

	aasClient := aas.NewFromConfig(cfg)
	cwClient := cw.NewFromConfig(cfg)
	resourceID := fmt.Sprintf("service/%s/%s", cluster, service)

	// Check if scalable target exists and matches desired configuration
	if enabled {
		exists, err := checkScalableTarget(context.TODO(), aasClient, resourceID, minCap32, maxCap32)
		if err != nil {
			slog.Error("failed to check scalable target", "error", err)
			os.Exit(1)
		}

		if !exists {
			slog.Info("registering scalable target", "resource", resourceID)
			if _, err := aasClient.RegisterScalableTarget(context.TODO(), &aas.RegisterScalableTargetInput{
				ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
				ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
				ResourceId:        aws.String(resourceID),
				MinCapacity:       aws.Int32(minCap32),
				MaxCapacity:       aws.Int32(maxCap32),
			}); err != nil {
				slog.Error("failed to register scalable target", "error", err)
				os.Exit(1)
			}
		} else {
			slog.Info("scalable target already exists with desired configuration", "resource", resourceID)
		}
	} else {
		// cleanup: delete alarms, policies, then deregister
		slog.Info("disabling auto-scaling", "resource", resourceID)
		cwClient.DeleteAlarms(context.TODO(), &cw.DeleteAlarmsInput{
			AlarmNames: []string{
				fmt.Sprintf("%s-%s-cpu-high", cluster, service),
				fmt.Sprintf("%s-%s-cpu-low", cluster, service),
				fmt.Sprintf("%s-%s-mem-high", cluster, service),
				fmt.Sprintf("%s-%s-mem-low", cluster, service),
			},
		})
		for _, name := range []string{
			fmt.Sprintf("%s-%s-scale-up", cluster, service),
			fmt.Sprintf("%s-%s-scale-down", cluster, service),
		} {
			aasClient.DeleteScalingPolicy(context.TODO(), &aas.DeleteScalingPolicyInput{
				ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
				ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
				ResourceId:        aws.String(resourceID),
				PolicyName:        aws.String(name),
			})
		}
		if _, err := aasClient.DeregisterScalableTarget(context.TODO(), &aas.DeregisterScalableTargetInput{
			ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
			ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
			ResourceId:        aws.String(resourceID),
		}); err != nil {
			slog.Error("failed to deregister scalable target", "error", err)
			os.Exit(1)
		}
		slog.Info("auto-scaling disabled and cleaned up")
		return
	}

	// (2) parse custom policies if provided
	var policies []PolicyDef
	if policiesRaw != "" {
		slog.Info("parsing custom scaling policies")
		if err := json.Unmarshal([]byte(policiesRaw), &policies); err != nil {
			slog.Error("invalid scaling-policies JSON", "error", err)
			os.Exit(1)
		}
	} else if defaultPoliciesRaw != "" {
		slog.Info("parsing default scaling policies")
		if err := json.Unmarshal([]byte(defaultPoliciesRaw), &policies); err != nil {
			slog.Error("invalid default-policies JSON", "error", err)
			os.Exit(1)
		}
	}

	// Check and apply policies only if they don't exist or need updating
	if len(policies) > 0 {
		for _, p := range policies {
			exists, err := checkScalingPolicy(context.TODO(), aasClient, resourceID, p.PolicyName)
			if err != nil {
				slog.Error("failed to check scaling policy", "policy_name", p.PolicyName, "error", err)
				os.Exit(1)
			}

			if !exists {
				slog.Info("applying policy", "policy_name", p.PolicyName)
				switch p.PolicyType {
				case "StepScaling":
					// build step adjustments
					var sa []aasTypes.StepAdjustment
					for _, adj := range p.StepAdjustments {
						sa = append(sa, aasTypes.StepAdjustment{
							MetricIntervalLowerBound: adj.MetricIntervalLowerBound,
							MetricIntervalUpperBound: adj.MetricIntervalUpperBound,
							ScalingAdjustment:        aws.Int32(adj.ScalingAdjustment),
						})
					}
					_, err := aasClient.PutScalingPolicy(context.TODO(), &aas.PutScalingPolicyInput{
						ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
						ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
						ResourceId:        aws.String(resourceID),
						PolicyName:        aws.String(p.PolicyName),
						PolicyType:        aasTypes.PolicyTypeStepScaling,
						StepScalingPolicyConfiguration: &aasTypes.StepScalingPolicyConfiguration{
							AdjustmentType:        aasTypes.AdjustmentType(p.AdjustmentType),
							Cooldown:              p.Cooldown,
							MetricAggregationType: aasTypes.MetricAggregationType(p.MetricAggregationType),
							StepAdjustments:       sa,
						},
					})
					if err != nil {
						slog.Error("failed to put scaling policy", "policy_name", p.PolicyName, "error", err)
						os.Exit(1)
					}

					// Only create CloudWatch alarm if both metric_name and metric_namespace are provided
					if p.MetricName != "" && p.MetricNamespace != "" {
						// Fetch policy ARN
						polDesc, err := aasClient.DescribeScalingPolicies(context.TODO(), &aas.DescribeScalingPoliciesInput{
							ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
							ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
							ResourceId:        aws.String(resourceID),
							PolicyNames:       []string{p.PolicyName},
						})
						if err != nil || len(polDesc.ScalingPolicies) == 0 {
							slog.Error("failed to describe scaling policy for alarm", "policy_name", p.PolicyName, "metric_namespace", p.MetricNamespace, "error", err)
							os.Exit(1)
						}
						policyARN := *polDesc.ScalingPolicies[0].PolicyARN
						alarmName := fmt.Sprintf("%s-%s-%s", cluster, service, p.PolicyName)
						_, err = cwClient.PutMetricAlarm(context.TODO(), &cw.PutMetricAlarmInput{
							AlarmName:          aws.String(alarmName),
							AlarmDescription:   aws.String(fmt.Sprintf("Scale based on %s", p.MetricName)),
							Namespace:          aws.String(p.MetricNamespace),
							MetricName:         aws.String(p.MetricName),
							Statistic:          cwTypes.StatisticAverage,
							Period:             aws.Int32(*p.Cooldown),
							EvaluationPeriods:  aws.Int32(2),
							Threshold:          aws.Float64(targetCPUUp),
							ComparisonOperator: cwTypes.ComparisonOperatorGreaterThanOrEqualToThreshold,
							Dimensions: []cwTypes.Dimension{
								{Name: aws.String("ClusterName"), Value: aws.String(cluster)},
								{Name: aws.String("ServiceName"), Value: aws.String(service)},
							},
							AlarmActions: []string{policyARN},
						})
						if err != nil {
							slog.Error("failed to put metric alarm", "alarm_name", alarmName, "error", err)
							os.Exit(1)
						}
						slog.Info("created CloudWatch alarm for custom policy", "alarm_name", alarmName)
					} else {
						slog.Info("no metric_name/metric_namespace specified; no alarm created for this custom policy", "policy_name", p.PolicyName)
					}

				case "TargetTrackingScaling":
					cfgTT := &aasTypes.TargetTrackingScalingPolicyConfiguration{
						TargetValue: aws.Float64(p.TargetTrackingConfiguration.TargetValue),
					}
					if pre := p.TargetTrackingConfiguration.PredefinedMetricSpecification; pre != "" {
						cfgTT.PredefinedMetricSpecification = &aasTypes.PredefinedMetricSpecification{
							PredefinedMetricType: aasTypes.MetricType(pre),
						}
					} else if cm := p.TargetTrackingConfiguration.CustomMetricSpecification; cm != nil {
						var dims []aasTypes.MetricDimension
						for k, v := range cm.Dimensions {
							dims = append(dims, aasTypes.MetricDimension{Name: aws.String(k), Value: aws.String(v)})
						}
						cfgTT.CustomizedMetricSpecification = &aasTypes.CustomizedMetricSpecification{
							MetricName: aws.String(cm.MetricName),
							Namespace:  aws.String(cm.Namespace),
							Dimensions: dims,
							Statistic:  aasTypes.MetricStatistic(cm.Statistic),
						}
					}
					cfgTT.ScaleInCooldown = p.TargetTrackingConfiguration.ScaleInCooldown
					cfgTT.ScaleOutCooldown = p.TargetTrackingConfiguration.ScaleOutCooldown

					_, err := aasClient.PutScalingPolicy(context.TODO(), &aas.PutScalingPolicyInput{
						ServiceNamespace:                         aasTypes.ServiceNamespaceEcs,
						ScalableDimension:                        aasTypes.ScalableDimension("ecs:service:DesiredCount"),
						ResourceId:                               aws.String(resourceID),
						PolicyName:                               aws.String(p.PolicyName),
						PolicyType:                               aasTypes.PolicyTypeTargetTrackingScaling,
						TargetTrackingScalingPolicyConfiguration: cfgTT,
					})
					if err != nil {
						slog.Error("failed to put scaling policy", "policy_name", p.PolicyName, "error", err)
						os.Exit(1)
					}

				default:
					slog.Error("unknown policy_type", "policy_type", p.PolicyType)
					os.Exit(1)
				}
			} else {
				slog.Info("policy already exists", "policy_name", p.PolicyName)
			}
		}
		slog.Info("custom scaling policies applied")
		return
	}

	// (3b) default CPU step-scaling + alarms
	slog.Info("applying default CPU step-scaling policies")
	// a) step policies
	for _, info := range []struct {
		name   string
		adjust int32
		cd     int32
	}{
		{fmt.Sprintf("%s-%s-scale-up", cluster, service), 1, outCd32},
		{fmt.Sprintf("%s-%s-scale-down", cluster, service), -1, inCd32},
	} {
		if _, err := aasClient.PutScalingPolicy(context.TODO(), &aas.PutScalingPolicyInput{
			ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
			ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
			ResourceId:        aws.String(resourceID),
			PolicyName:        aws.String(info.name),
			PolicyType:        aasTypes.PolicyTypeStepScaling,
			StepScalingPolicyConfiguration: &aasTypes.StepScalingPolicyConfiguration{
				AdjustmentType:        aasTypes.AdjustmentTypeChangeInCapacity,
				Cooldown:              aws.Int32(info.cd),
				MetricAggregationType: aasTypes.MetricAggregationTypeMaximum,
				StepAdjustments:       []aasTypes.StepAdjustment{{MetricIntervalLowerBound: aws.Float64(0), ScalingAdjustment: aws.Int32(info.adjust)}},
			},
		}); err != nil {
			slog.Error("failed to put scaling policy", "policy_name", info.name, "error", err)
			os.Exit(1)
		}
	}

	// b) describe to fetch ARNs
	upPol, err := aasClient.DescribeScalingPolicies(context.TODO(), &aas.DescribeScalingPoliciesInput{
		ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
		ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
		ResourceId:        aws.String(resourceID),
		PolicyNames:       []string{fmt.Sprintf("%s-%s-scale-up", cluster, service)},
	})
	if err != nil || len(upPol.ScalingPolicies) == 0 {
		slog.Error("failed to describe up-policy", "error", err)
		os.Exit(1)
	}
	downPol, err := aasClient.DescribeScalingPolicies(context.TODO(), &aas.DescribeScalingPoliciesInput{
		ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
		ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
		ResourceId:        aws.String(resourceID),
		PolicyNames:       []string{fmt.Sprintf("%s-%s-scale-down", cluster, service)},
	})
	if err != nil || len(downPol.ScalingPolicies) == 0 {
		slog.Error("failed to describe down-policy", "error", err)
		os.Exit(1)
	}

	// c) CloudWatch alarms
	alarms := []struct {
		name, desc string
		comp       cwTypes.ComparisonOperator
		period     int32
		arn        string
		metric     string
		threshold  float64
	}{
		{
			name:      fmt.Sprintf("%s-%s-cpu-high", cluster, service),
			desc:      "Scale up on high CPU",
			comp:      cwTypes.ComparisonOperatorGreaterThanOrEqualToThreshold,
			period:    outCd32,
			arn:       *upPol.ScalingPolicies[0].PolicyARN,
			metric:    "CPUUtilization",
			threshold: targetCPUUp,
		},
		{
			name:      fmt.Sprintf("%s-%s-cpu-low", cluster, service),
			desc:      "Scale down on low CPU",
			comp:      cwTypes.ComparisonOperatorLessThanOrEqualToThreshold,
			period:    inCd32,
			arn:       *downPol.ScalingPolicies[0].PolicyARN,
			metric:    "CPUUtilization",
			threshold: targetCPUDown,
		},
		{
			name:      fmt.Sprintf("%s-%s-mem-high", cluster, service),
			desc:      "Scale up on high memory",
			comp:      cwTypes.ComparisonOperatorGreaterThanOrEqualToThreshold,
			period:    outCd32,
			arn:       *upPol.ScalingPolicies[0].PolicyARN,
			metric:    "MemoryUtilization",
			threshold: targetMemUp,
		},
		{
			name:      fmt.Sprintf("%s-%s-mem-low", cluster, service),
			desc:      "Scale down on low memory",
			comp:      cwTypes.ComparisonOperatorLessThanOrEqualToThreshold,
			period:    inCd32,
			arn:       *downPol.ScalingPolicies[0].PolicyARN,
			metric:    "MemoryUtilization",
			threshold: targetMemDown,
		},
	}

	// Check and create CloudWatch alarms only if they don't exist
	for _, a := range alarms {
		exists, err := checkCloudWatchAlarm(context.TODO(), cwClient, a.name)
		if err != nil {
			slog.Error("failed to check CloudWatch alarm", "alarm_name", a.name, "error", err)
			os.Exit(1)
		}

		if !exists {
			slog.Info("creating CloudWatch alarm", "alarm_name", a.name)
			if _, err := cwClient.PutMetricAlarm(context.TODO(), &cw.PutMetricAlarmInput{
				AlarmName:          aws.String(a.name),
				AlarmDescription:   aws.String(a.desc),
				Namespace:          aws.String("AWS/ECS"),
				MetricName:         aws.String(a.metric),
				Statistic:          cwTypes.StatisticAverage,
				Period:             aws.Int32(a.period),
				EvaluationPeriods:  aws.Int32(2),
				Threshold:          aws.Float64(a.threshold),
				ComparisonOperator: a.comp,
				Dimensions: []cwTypes.Dimension{
					{Name: aws.String("ClusterName"), Value: aws.String(cluster)},
					{Name: aws.String("ServiceName"), Value: aws.String(service)},
				},
				AlarmActions: []string{a.arn},
			}); err != nil {
				slog.Error("failed to put metric alarm", "alarm_name", a.name, "error", err)
				os.Exit(1)
			}
		} else {
			slog.Info("CloudWatch alarm already exists", "alarm_name", a.name)
		}
	}

	slog.Info("default CPU and memory auto-scaling & alarms configured")
}
