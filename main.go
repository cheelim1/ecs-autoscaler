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
	RegisterScalableTarget(ctx context.Context, params *aas.RegisterScalableTargetInput, optFns ...func(*aas.Options)) (*aas.RegisterScalableTargetOutput, error)
	PutScalingPolicy(ctx context.Context, params *aas.PutScalingPolicyInput, optFns ...func(*aas.Options)) (*aas.PutScalingPolicyOutput, error)
	DeleteScalingPolicy(ctx context.Context, params *aas.DeleteScalingPolicyInput, optFns ...func(*aas.Options)) (*aas.DeleteScalingPolicyOutput, error)
	DeregisterScalableTarget(ctx context.Context, params *aas.DeregisterScalableTargetInput, optFns ...func(*aas.Options)) (*aas.DeregisterScalableTargetOutput, error)
}

type CWClient interface {
	DescribeAlarms(ctx context.Context, params *cw.DescribeAlarmsInput, optFns ...func(*cw.Options)) (*cw.DescribeAlarmsOutput, error)
	DeleteAlarms(ctx context.Context, params *cw.DeleteAlarmsInput, optFns ...func(*cw.Options)) (*cw.DeleteAlarmsOutput, error)
	PutMetricAlarm(ctx context.Context, params *cw.PutMetricAlarmInput, optFns ...func(*cw.Options)) (*cw.PutMetricAlarmOutput, error)
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
	ScaleDirection              string                `json:"scale_direction,omitempty"` // "in" or "out" (optional, explicit)
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

// Check if scalable target exists (without checking capacity values)
func scalableTargetExists(ctx context.Context, client AASClient, resourceID string) (bool, error) {
	resp, err := client.DescribeScalableTargets(ctx, &aas.DescribeScalableTargetsInput{
		ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
		ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
		ResourceIds:       []string{resourceID},
	})
	if err != nil {
		return false, fmt.Errorf("failed to describe scalable target: %v", err)
	}

	return len(resp.ScalableTargets) > 0, nil
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

// Compare existing scaling policy with desired configuration
func compareScalingPolicy(ctx context.Context, client AASClient, resourceID, policyName string, desired *aas.PutScalingPolicyInput) (bool, error) {
	resp, err := client.DescribeScalingPolicies(ctx, &aas.DescribeScalingPoliciesInput{
		ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
		ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
		ResourceId:        aws.String(resourceID),
		PolicyNames:       []string{policyName},
	})
	if err != nil {
		return false, fmt.Errorf("failed to describe scaling policy: %v", err)
	}

	if len(resp.ScalingPolicies) == 0 {
		return false, nil // Policy doesn't exist
	}

	existing := resp.ScalingPolicies[0]

	// Compare policy type
	if existing.PolicyType != desired.PolicyType {
		return false, nil
	}

	// Compare based on policy type
	switch desired.PolicyType {
	case aasTypes.PolicyTypeStepScaling:
		if existing.StepScalingPolicyConfiguration == nil || desired.StepScalingPolicyConfiguration == nil {
			return false, nil
		}

		existingStep := existing.StepScalingPolicyConfiguration
		desiredStep := desired.StepScalingPolicyConfiguration

		if existingStep.AdjustmentType != desiredStep.AdjustmentType ||
			existingStep.MetricAggregationType != desiredStep.MetricAggregationType {
			return false, nil
		}

		// Compare cooldown (handle nil cases)
		if (existingStep.Cooldown == nil) != (desiredStep.Cooldown == nil) {
			return false, nil
		}
		if existingStep.Cooldown != nil && desiredStep.Cooldown != nil && *existingStep.Cooldown != *desiredStep.Cooldown {
			return false, nil
		}

		// Compare step adjustments
		if len(existingStep.StepAdjustments) != len(desiredStep.StepAdjustments) {
			return false, nil
		}

		for i, existingAdj := range existingStep.StepAdjustments {
			desiredAdj := desiredStep.StepAdjustments[i]

			// Compare bounds (handle nil cases)
			if (existingAdj.MetricIntervalLowerBound == nil) != (desiredAdj.MetricIntervalLowerBound == nil) ||
				(existingAdj.MetricIntervalUpperBound == nil) != (desiredAdj.MetricIntervalUpperBound == nil) {
				return false, nil
			}

			if existingAdj.MetricIntervalLowerBound != nil && desiredAdj.MetricIntervalLowerBound != nil &&
				*existingAdj.MetricIntervalLowerBound != *desiredAdj.MetricIntervalLowerBound {
				return false, nil
			}

			if existingAdj.MetricIntervalUpperBound != nil && desiredAdj.MetricIntervalUpperBound != nil &&
				*existingAdj.MetricIntervalUpperBound != *desiredAdj.MetricIntervalUpperBound {
				return false, nil
			}

			if *existingAdj.ScalingAdjustment != *desiredAdj.ScalingAdjustment {
				return false, nil
			}
		}

	case aasTypes.PolicyTypeTargetTrackingScaling:
		if existing.TargetTrackingScalingPolicyConfiguration == nil || desired.TargetTrackingScalingPolicyConfiguration == nil {
			return false, nil
		}

		existingTT := existing.TargetTrackingScalingPolicyConfiguration
		desiredTT := desired.TargetTrackingScalingPolicyConfiguration

		if *existingTT.TargetValue != *desiredTT.TargetValue {
			return false, nil
		}

		// Compare cooldowns (handle nil cases)
		if (existingTT.ScaleInCooldown == nil) != (desiredTT.ScaleInCooldown == nil) ||
			(existingTT.ScaleOutCooldown == nil) != (desiredTT.ScaleOutCooldown == nil) {
			return false, nil
		}

		if existingTT.ScaleInCooldown != nil && desiredTT.ScaleInCooldown != nil &&
			*existingTT.ScaleInCooldown != *desiredTT.ScaleInCooldown {
			return false, nil
		}

		if existingTT.ScaleOutCooldown != nil && desiredTT.ScaleOutCooldown != nil &&
			*existingTT.ScaleOutCooldown != *desiredTT.ScaleOutCooldown {
			return false, nil
		}

		// Compare metric specifications
		if (existingTT.PredefinedMetricSpecification == nil) != (desiredTT.PredefinedMetricSpecification == nil) {
			return false, nil
		}

		if existingTT.PredefinedMetricSpecification != nil && desiredTT.PredefinedMetricSpecification != nil {
			if existingTT.PredefinedMetricSpecification.PredefinedMetricType != desiredTT.PredefinedMetricSpecification.PredefinedMetricType {
				return false, nil
			}
		}

		if (existingTT.CustomizedMetricSpecification == nil) != (desiredTT.CustomizedMetricSpecification == nil) {
			return false, nil
		}

		if existingTT.CustomizedMetricSpecification != nil && desiredTT.CustomizedMetricSpecification != nil {
			existingCustom := existingTT.CustomizedMetricSpecification
			desiredCustom := desiredTT.CustomizedMetricSpecification

			if *existingCustom.MetricName != *desiredCustom.MetricName ||
				*existingCustom.Namespace != *desiredCustom.Namespace ||
				existingCustom.Statistic != desiredCustom.Statistic {
				return false, nil
			}

			// Compare dimensions
			if len(existingCustom.Dimensions) != len(desiredCustom.Dimensions) {
				return false, nil
			}

			existingDims := make(map[string]string)
			for _, dim := range existingCustom.Dimensions {
				existingDims[*dim.Name] = *dim.Value
			}

			for _, dim := range desiredCustom.Dimensions {
				if existingDims[*dim.Name] != *dim.Value {
					return false, nil
				}
			}
		}
	}

	return true, nil // Configuration matches
}

// Helper function to deduplicate string slices
func deduplicate(slice []string) []string {
	seen := make(map[string]bool)
	result := []string{}
	for _, item := range slice {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
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

	targetCPUOut, err := getFloatWithDefault(os.Args[11], "target-cpu-utilization-out", 75.0)
	if err != nil {
		os.Exit(1)
	}
	targetCPUIn, err := getFloatWithDefault(os.Args[12], "target-cpu-utilization-in", 65.0)
	if err != nil {
		os.Exit(1)
	}
	targetMemOut, err := getFloatWithDefault(os.Args[13], "target-memory-utilization-out", 80.0)
	if err != nil {
		os.Exit(1)
	}
	targetMemIn, err := getFloatWithDefault(os.Args[14], "target-memory-utilization-in", 70.0)
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
		slog.Info("disabling auto-scaling", "resource", resourceID, "cluster", cluster, "service", service)

		// First check if scalable target exists to determine if auto-scaling was ever enabled
		exists, err := scalableTargetExists(context.TODO(), aasClient, resourceID)
		if err != nil {
			slog.Error("failed to check scalable target", "error", err)
			os.Exit(1)
		}
		if !exists {
			slog.Info("auto-scaling was not enabled for this service", "cluster", cluster, "service", service)
			return
		}

		// Parse custom policies to get all policy names
		var policies []PolicyDef
		if policiesRaw != "" {
			slog.Info("parsing custom scaling policies for cleanup")
			if err := json.Unmarshal([]byte(policiesRaw), &policies); err != nil {
				slog.Error("invalid scaling-policies JSON during cleanup", "error", err)
				os.Exit(1)
			}
		} else if defaultPoliciesRaw != "" {
			slog.Info("parsing default scaling policies for cleanup")
			if err := json.Unmarshal([]byte(defaultPoliciesRaw), &policies); err != nil {
				slog.Error("invalid default-policies JSON during cleanup", "error", err)
				os.Exit(1)
			}
		}

		// Collect all alarm names to delete
		alarmNames := []string{
			// Default alarms
			fmt.Sprintf("%s-%s-cpu-high", cluster, service),
			fmt.Sprintf("%s-%s-cpu-low", cluster, service),
			fmt.Sprintf("%s-%s-mem-high", cluster, service),
			fmt.Sprintf("%s-%s-mem-low", cluster, service),
		}

		// Add custom policy alarms
		for _, p := range policies {
			if p.MetricName != "" && p.MetricNamespace != "" {
				alarmName := fmt.Sprintf("%s-%s-%s", cluster, service, p.PolicyName)
				alarmNames = append(alarmNames, alarmName)
			}
		}

		// Check which alarms actually exist before deleting
		existingAlarms := []string{}
		for _, alarmName := range alarmNames {
			exists, err := checkCloudWatchAlarm(context.TODO(), cwClient, alarmName)
			if err != nil {
				slog.Error("failed to check CloudWatch alarm", "alarm_name", alarmName, "error", err)
				continue
			}
			if exists {
				existingAlarms = append(existingAlarms, alarmName)
			}
		}

		// Delete only existing alarms
		if len(existingAlarms) > 0 {
			slog.Info("deleting CloudWatch alarms", "alarms", existingAlarms)
			if _, err := cwClient.DeleteAlarms(context.TODO(), &cw.DeleteAlarmsInput{
				AlarmNames: existingAlarms,
			}); err != nil {
				slog.Error("failed to delete alarms", "error", err)
				os.Exit(1)
			}
		}

		// Collect all policy names to delete
		policyNames := []string{
			// Default policies
			fmt.Sprintf("%s-%s-scale-out", cluster, service),
			fmt.Sprintf("%s-%s-scale-in", cluster, service),
		}

		// Add custom policy names
		for _, p := range policies {
			policyNames = append(policyNames, p.PolicyName)
		}

		// Deduplicate policy names to avoid attempting to delete the same policy twice
		policyNames = deduplicate(policyNames)

		// Check and delete only existing scaling policies
		existingPolicies := []string{}
		for _, name := range policyNames {
			exists, err := checkScalingPolicy(context.TODO(), aasClient, resourceID, name)
			if err != nil {
				slog.Error("failed to check scaling policy", "policy_name", name, "error", err)
				continue
			}
			if exists {
				existingPolicies = append(existingPolicies, name)
			}
		}

		// Delete existing policies
		for _, name := range existingPolicies {
			slog.Info("deleting scaling policy", "policy_name", name)
			if _, err := aasClient.DeleteScalingPolicy(context.TODO(), &aas.DeleteScalingPolicyInput{
				ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
				ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
				ResourceId:        aws.String(resourceID),
				PolicyName:        aws.String(name),
			}); err != nil {
				slog.Error("failed to delete scaling policy", "policy_name", name, "error", err)
				os.Exit(1)
			}
		}

		// Deregister the scalable target
		slog.Info("deregistering scalable target", "resource", resourceID)
		if _, err := aasClient.DeregisterScalableTarget(context.TODO(), &aas.DeregisterScalableTargetInput{
			ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
			ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
			ResourceId:        aws.String(resourceID),
		}); err != nil {
			slog.Error("failed to deregister scalable target", "error", err)
			os.Exit(1)
		}

		slog.Info("auto-scaling disabled and cleaned up", "cluster", cluster, "service", service)
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

	// For each policy, compare with existing configuration and update only if needed
	for _, p := range policies {
		slog.Info("processing policy", "policy_name", p.PolicyName)

		var policyInput *aas.PutScalingPolicyInput

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
			policyInput = &aas.PutScalingPolicyInput{
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

			policyInput = &aas.PutScalingPolicyInput{
				ServiceNamespace:                         aasTypes.ServiceNamespaceEcs,
				ScalableDimension:                        aasTypes.ScalableDimension("ecs:service:DesiredCount"),
				ResourceId:                               aws.String(resourceID),
				PolicyName:                               aws.String(p.PolicyName),
				PolicyType:                               aasTypes.PolicyTypeTargetTrackingScaling,
				TargetTrackingScalingPolicyConfiguration: cfgTT,
			}

		default:
			slog.Error("unknown policy_type", "policy_type", p.PolicyType)
			os.Exit(1)
		}

		// Check if policy needs to be updated
		policyMatches, err := compareScalingPolicy(context.TODO(), aasClient, resourceID, p.PolicyName, policyInput)
		if err != nil {
			slog.Error("failed to compare scaling policy", "policy_name", p.PolicyName, "error", err)
			os.Exit(1)
		}

		policyExists := true
		if !policyMatches {
			// Check if policy exists at all
			exists, err := checkScalingPolicy(context.TODO(), aasClient, resourceID, p.PolicyName)
			if err != nil {
				slog.Error("failed to check scaling policy existence", "policy_name", p.PolicyName, "error", err)
				os.Exit(1)
			}
			policyExists = exists

			if policyExists {
				slog.Info("updating scaling policy configuration", "policy_name", p.PolicyName)
			} else {
				slog.Info("creating new scaling policy", "policy_name", p.PolicyName)
			}
			_, err = aasClient.PutScalingPolicy(context.TODO(), policyInput)
			if err != nil {
				slog.Error("failed to put scaling policy", "policy_name", p.PolicyName, "error", err)
				os.Exit(1)
			}
		} else {
			slog.Info("scaling policy is up to date", "policy_name", p.PolicyName)
		}

		// Only create alarms for NEW policies to prevent "Multiple alarms attached" warnings
		// If policy already existed, we leave existing alarms completely alone
		if p.PolicyType == "StepScaling" && p.MetricName != "" && p.MetricNamespace != "" && !policyExists {
			slog.Info("creating CloudWatch alarm for new scaling policy", "policy_name", p.PolicyName)

			// Fetch policy ARN (needed for alarm configuration)
			polDesc, err := aasClient.DescribeScalingPolicies(context.TODO(), &aas.DescribeScalingPoliciesInput{
				ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
				ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
				ResourceId:        aws.String(resourceID),
				PolicyNames:       []string{p.PolicyName},
			})
			if err != nil || len(polDesc.ScalingPolicies) == 0 {
				slog.Error("failed to describe scaling policy for alarm", "policy_name", p.PolicyName, "error", err)
				os.Exit(1)
			}
			policyARN := *polDesc.ScalingPolicies[0].PolicyARN
			alarmName := fmt.Sprintf("%s-%s-%s", cluster, service, p.PolicyName)

			// Determine threshold and comparison operator based on scaling direction
			var threshold float64
			var compOp cwTypes.ComparisonOperator
			if p.ScaleDirection == "in" {
				threshold = targetCPUIn
				compOp = cwTypes.ComparisonOperatorLessThanOrEqualToThreshold
			} else if p.ScaleDirection == "out" {
				threshold = targetCPUOut
				compOp = cwTypes.ComparisonOperatorGreaterThanOrEqualToThreshold
			} else {
				threshold = targetCPUOut
				compOp = cwTypes.ComparisonOperatorGreaterThanOrEqualToThreshold
			}

			alarmInput := &cw.PutMetricAlarmInput{
				AlarmName:          aws.String(alarmName),
				AlarmDescription:   aws.String(fmt.Sprintf("Scale based on %s", p.MetricName)),
				Namespace:          aws.String(p.MetricNamespace),
				MetricName:         aws.String(p.MetricName),
				Statistic:          cwTypes.StatisticAverage,
				Period:             aws.Int32(*p.Cooldown),
				EvaluationPeriods:  aws.Int32(2),
				Threshold:          aws.Float64(threshold),
				ComparisonOperator: compOp,
				Dimensions: []cwTypes.Dimension{
					{Name: aws.String("ClusterName"), Value: aws.String(cluster)},
					{Name: aws.String("ServiceName"), Value: aws.String(service)},
				},
				AlarmActions: []string{policyARN},
			}

			// Check if alarm already exists - if it does, leave it alone
			var alarmExists bool
			alarmExists, err = checkCloudWatchAlarm(context.TODO(), cwClient, alarmName)
			if err != nil {
				slog.Error("failed to check CloudWatch alarm existence", "alarm_name", alarmName, "error", err)
				os.Exit(1)
			}

			if !alarmExists {
				slog.Info("creating CloudWatch alarm for new policy", "alarm_name", alarmName)
				_, err = cwClient.PutMetricAlarm(context.TODO(), alarmInput)
				if err != nil {
					slog.Error("failed to put metric alarm", "alarm_name", alarmName, "error", err)
					os.Exit(1)
				}
			} else {
				slog.Info("CloudWatch alarm already exists, leaving unchanged", "alarm_name", alarmName)
			}
		} else if p.PolicyType == "StepScaling" && p.MetricName != "" && p.MetricNamespace != "" {
			slog.Info("scaling policy already exists, leaving existing alarms unchanged", "policy_name", p.PolicyName)
		}
	}
	if len(policies) > 0 {
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
		{fmt.Sprintf("%s-%s-scale-out", cluster, service), 1, outCd32},
		{fmt.Sprintf("%s-%s-scale-in", cluster, service), -1, inCd32},
	} {
		policyInput := &aas.PutScalingPolicyInput{
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
		}

		// Check if policy needs to be updated
		policyMatches, err := compareScalingPolicy(context.TODO(), aasClient, resourceID, info.name, policyInput)
		if err != nil {
			slog.Error("failed to compare scaling policy", "policy_name", info.name, "error", err)
			os.Exit(1)
		}

		if !policyMatches {
			slog.Info("updating default scaling policy", "policy_name", info.name)
			if _, err := aasClient.PutScalingPolicy(context.TODO(), policyInput); err != nil {
				slog.Error("failed to put scaling policy", "policy_name", info.name, "error", err)
				os.Exit(1)
			}
		} else {
			slog.Info("default scaling policy is up to date", "policy_name", info.name)
		}
	}

	// b) describe to fetch ARNs
	upPol, err := aasClient.DescribeScalingPolicies(context.TODO(), &aas.DescribeScalingPoliciesInput{
		ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
		ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
		ResourceId:        aws.String(resourceID),
		PolicyNames:       []string{fmt.Sprintf("%s-%s-scale-out", cluster, service)},
	})
	if err != nil || len(upPol.ScalingPolicies) == 0 {
		slog.Error("failed to describe up-policy", "error", err)
		os.Exit(1)
	}
	downPol, err := aasClient.DescribeScalingPolicies(context.TODO(), &aas.DescribeScalingPoliciesInput{
		ServiceNamespace:  aasTypes.ServiceNamespaceEcs,
		ScalableDimension: aasTypes.ScalableDimension("ecs:service:DesiredCount"),
		ResourceId:        aws.String(resourceID),
		PolicyNames:       []string{fmt.Sprintf("%s-%s-scale-in", cluster, service)},
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
			desc:      "Scale out on high CPU",
			comp:      cwTypes.ComparisonOperatorGreaterThanOrEqualToThreshold,
			period:    outCd32,
			arn:       *upPol.ScalingPolicies[0].PolicyARN,
			metric:    "CPUUtilization",
			threshold: targetCPUOut,
		},
		{
			name:      fmt.Sprintf("%s-%s-cpu-low", cluster, service),
			desc:      "Scale in on low CPU",
			comp:      cwTypes.ComparisonOperatorLessThanOrEqualToThreshold,
			period:    inCd32,
			arn:       *downPol.ScalingPolicies[0].PolicyARN,
			metric:    "CPUUtilization",
			threshold: targetCPUIn,
		},
		{
			name:      fmt.Sprintf("%s-%s-mem-high", cluster, service),
			desc:      "Scale out on high memory",
			comp:      cwTypes.ComparisonOperatorGreaterThanOrEqualToThreshold,
			period:    outCd32,
			arn:       *upPol.ScalingPolicies[0].PolicyARN,
			metric:    "MemoryUtilization",
			threshold: targetMemOut,
		},
		{
			name:      fmt.Sprintf("%s-%s-mem-low", cluster, service),
			desc:      "Scale in on low memory",
			comp:      cwTypes.ComparisonOperatorLessThanOrEqualToThreshold,
			period:    inCd32,
			arn:       *downPol.ScalingPolicies[0].PolicyARN,
			metric:    "MemoryUtilization",
			threshold: targetMemIn,
		},
	}

	// Only create alarms if they don't already exist
	slog.Info("configuring CloudWatch alarms for default policies")
	for _, a := range alarms {
		alarmInput := &cw.PutMetricAlarmInput{
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
		}

		// Check if alarm already exists - if it does, leave it alone
		var alarmExists bool
		alarmExists, err = checkCloudWatchAlarm(context.TODO(), cwClient, a.name)
		if err != nil {
			slog.Error("failed to check CloudWatch alarm existence", "alarm_name", a.name, "error", err)
			os.Exit(1)
		}

		if !alarmExists {
			slog.Info("creating CloudWatch alarm for default policy", "alarm_name", a.name)
			_, err = cwClient.PutMetricAlarm(context.TODO(), alarmInput)
			if err != nil {
				slog.Error("failed to put metric alarm", "alarm_name", a.name, "error", err)
				os.Exit(1)
			}
		} else {
			slog.Info("CloudWatch alarm already exists, leaving unchanged", "alarm_name", a.name)
		}
	}

	slog.Info("default CPU and memory auto-scaling & alarms configured")
}
