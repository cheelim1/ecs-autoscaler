package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/applicationautoscaling"
	aasTypes "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwTypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

// Mock AWS clients for testing
type mockAASClient struct {
	describeScalableTargetsOutput *applicationautoscaling.DescribeScalableTargetsOutput
	describeScalableTargetsError  error
	describeScalingPoliciesOutput *applicationautoscaling.DescribeScalingPoliciesOutput
	describeScalingPoliciesError  error
}

func (m *mockAASClient) DescribeScalableTargets(ctx context.Context, params *applicationautoscaling.DescribeScalableTargetsInput, optFns ...func(*applicationautoscaling.Options)) (*applicationautoscaling.DescribeScalableTargetsOutput, error) {
	return m.describeScalableTargetsOutput, m.describeScalableTargetsError
}

func (m *mockAASClient) DescribeScalingPolicies(ctx context.Context, params *applicationautoscaling.DescribeScalingPoliciesInput, optFns ...func(*applicationautoscaling.Options)) (*applicationautoscaling.DescribeScalingPoliciesOutput, error) {
	return m.describeScalingPoliciesOutput, m.describeScalingPoliciesError
}

type mockCWClient struct {
	describeAlarmsOutput *cloudwatch.DescribeAlarmsOutput
	describeAlarmsError  error
}

func (m *mockCWClient) DescribeAlarms(ctx context.Context, params *cloudwatch.DescribeAlarmsInput, optFns ...func(*cloudwatch.Options)) (*cloudwatch.DescribeAlarmsOutput, error) {
	return m.describeAlarmsOutput, m.describeAlarmsError
}

// TestGetIntWithDefault_Valid ensures getIntWithDefault returns the correct integer for a valid string.
func TestGetIntWithDefault_Valid(t *testing.T) {
	got, err := getIntWithDefault("123", "test", 1)
	if err != nil {
		t.Errorf("getIntWithDefault valid: unexpected error: %v", err)
	}
	want := 123
	if got != want {
		t.Errorf("getIntWithDefault valid: got %d, want %d", got, want)
	}
}

// TestGetIntWithDefault_Empty ensures getIntWithDefault returns the default value for empty string.
func TestGetIntWithDefault_Empty(t *testing.T) {
	got, err := getIntWithDefault("", "test", 42)
	if err != nil {
		t.Errorf("getIntWithDefault empty: unexpected error: %v", err)
	}
	want := 42
	if got != want {
		t.Errorf("getIntWithDefault empty: got %d, want %d", got, want)
	}
}

// TestGetIntWithDefault_Invalid ensures getIntWithDefault handles invalid input correctly.
func TestGetIntWithDefault_Invalid(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	slog.SetDefault(logger)

	// Call getIntWithDefault with invalid input
	got, err := getIntWithDefault("invalid", "test", 42)
	if err == nil {
		t.Error("getIntWithDefault invalid: expected error, got nil")
	}
	if got != 0 {
		t.Errorf("getIntWithDefault invalid: expected 0, got %d", got)
	}

	// Check if the error message contains expected text
	output := buf.String()
	if !strings.Contains(output, "invalid input") || !strings.Contains(output, "invalid") {
		t.Errorf("Expected error message not found in log output: %s", output)
	}
}

// TestGetFloatWithDefault_Valid ensures getFloatWithDefault returns the correct float for a valid string.
func TestGetFloatWithDefault_Valid(t *testing.T) {
	got, err := getFloatWithDefault("123.45", "test", 1.0)
	if err != nil {
		t.Errorf("getFloatWithDefault valid: unexpected error: %v", err)
	}
	want := 123.45
	if got != want {
		t.Errorf("getFloatWithDefault valid: got %f, want %f", got, want)
	}
}

// TestGetFloatWithDefault_Empty ensures getFloatWithDefault returns the default value for empty string.
func TestGetFloatWithDefault_Empty(t *testing.T) {
	got, err := getFloatWithDefault("", "test", 42.5)
	if err != nil {
		t.Errorf("getFloatWithDefault empty: unexpected error: %v", err)
	}
	want := 42.5
	if got != want {
		t.Errorf("getFloatWithDefault empty: got %f, want %f", got, want)
	}
}

// TestGetFloatWithDefault_Invalid ensures getFloatWithDefault handles invalid input correctly.
func TestGetFloatWithDefault_Invalid(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	slog.SetDefault(logger)

	// Call getFloatWithDefault with invalid input
	got, err := getFloatWithDefault("invalid", "test", 42.5)
	if err == nil {
		t.Error("getFloatWithDefault invalid: expected error, got nil")
	}
	if got != 0 {
		t.Errorf("getFloatWithDefault invalid: expected 0, got %f", got)
	}

	// Check if the error message contains expected text
	output := buf.String()
	if !strings.Contains(output, "invalid input") || !strings.Contains(output, "invalid") {
		t.Errorf("Expected error message not found in log output: %s", output)
	}
}

// TestUnmarshalStepScalingPolicy tests JSON unmarshalling of a StepScaling policy.
func TestUnmarshalStepScalingPolicy(t *testing.T) {
	jsonStr := `[
      {
        "policy_name": "test-step",
        "policy_type": "StepScaling",
        "adjustment_type": "ChangeInCapacity",
        "cooldown": 60,
        "metric_aggregation_type": "Maximum",
        "step_adjustments": [
          {"MetricIntervalLowerBound": 0, "ScalingAdjustment": 2}
        ]
      }
    ]`

	var policies []PolicyDef
	if err := json.Unmarshal([]byte(jsonStr), &policies); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(policies))
	}
	p := policies[0]
	if p.PolicyName != "test-step" {
		t.Errorf("PolicyName: got %q, want %q", p.PolicyName, "test-step")
	}
	if p.PolicyType != "StepScaling" {
		t.Errorf("PolicyType: got %q, want %q", p.PolicyType, "StepScaling")
	}
	expectedAdj := StepAdj{MetricIntervalLowerBound: func() *float64 { f := 0.0; return &f }(), ScalingAdjustment: 2}
	if len(p.StepAdjustments) != 1 || !reflect.DeepEqual(p.StepAdjustments[0], expectedAdj) {
		t.Errorf("StepAdjustments: got %+v, want %+v", p.StepAdjustments, []StepAdj{expectedAdj})
	}
}

// TestUnmarshalStepScalingPolicyWithMetric tests JSON unmarshalling of a StepScaling policy with custom metric.
func TestUnmarshalStepScalingPolicyWithMetric(t *testing.T) {
	jsonStr := `[
      {
        "policy_name": "test-step-metric",
        "policy_type": "StepScaling",
        "metric_name": "CustomMetric",
        "metric_namespace": "CustomNamespace",
        "adjustment_type": "ChangeInCapacity",
        "cooldown": 60,
        "metric_aggregation_type": "Maximum",
        "step_adjustments": [
          {"MetricIntervalLowerBound": 0, "ScalingAdjustment": 2}
        ]
      }
    ]`

	var policies []PolicyDef
	if err := json.Unmarshal([]byte(jsonStr), &policies); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(policies))
	}
	p := policies[0]
	if p.MetricName != "CustomMetric" {
		t.Errorf("MetricName: got %q, want %q", p.MetricName, "CustomMetric")
	}
	if p.MetricNamespace != "CustomNamespace" {
		t.Errorf("MetricNamespace: got %q, want %q", p.MetricNamespace, "CustomNamespace")
	}
}

// TestUnmarshalTargetTrackingPolicy tests JSON unmarshalling of a TargetTrackingScaling policy.
func TestUnmarshalTargetTrackingPolicy(t *testing.T) {
	jsonStr := `[
      {
        "policy_name": "test-tt",
        "policy_type": "TargetTrackingScaling",
        "target_tracking_configuration": {
          "target_value": 50.5,
          "predefined_metric_specification": "ECSServiceAverageCPUUtilization",
          "scale_in_cooldown": 100,
          "scale_out_cooldown": 150
        }
      }
    ]`

	var policies []PolicyDef
	if err := json.Unmarshal([]byte(jsonStr), &policies); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(policies))
	}
	p := policies[0]
	cfg := p.TargetTrackingConfiguration
	if cfg == nil {
		t.Fatal("TargetTrackingConfiguration is nil")
	}
	if cfg.TargetValue != 50.5 {
		t.Errorf("TargetValue: got %v, want %v", cfg.TargetValue, 50.5)
	}
	if cfg.PredefinedMetricSpecification != "ECSServiceAverageCPUUtilization" {
		t.Errorf("PredefinedMetricSpecification: got %q, want %q", cfg.PredefinedMetricSpecification, "ECSServiceAverageCPUUtilization")
	}
	if cfg.ScaleInCooldown == nil || *cfg.ScaleInCooldown != 100 {
		t.Errorf("ScaleInCooldown: got %v, want %v", cfg.ScaleInCooldown, 100)
	}
	if cfg.ScaleOutCooldown == nil || *cfg.ScaleOutCooldown != 150 {
		t.Errorf("ScaleOutCooldown: got %v, want %v", cfg.ScaleOutCooldown, 150)
	}
}

// TestUnmarshalTargetTrackingPolicyWithCustomMetric tests JSON unmarshalling of a TargetTrackingScaling policy with custom metric.
func TestUnmarshalTargetTrackingPolicyWithCustomMetric(t *testing.T) {
	jsonStr := `[
      {
        "policy_name": "test-tt-custom",
        "policy_type": "TargetTrackingScaling",
        "target_tracking_configuration": {
          "target_value": 60.0,
          "custom_metric_specification": {
            "namespace": "CustomNamespace",
            "metric_name": "CustomMetric",
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
    ]`

	var policies []PolicyDef
	if err := json.Unmarshal([]byte(jsonStr), &policies); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(policies))
	}
	p := policies[0]
	cfg := p.TargetTrackingConfiguration
	if cfg == nil {
		t.Fatal("TargetTrackingConfiguration is nil")
	}
	if cfg.CustomMetricSpecification == nil {
		t.Fatal("CustomMetricSpecification is nil")
	}
	if cfg.CustomMetricSpecification.Namespace != "CustomNamespace" {
		t.Errorf("Namespace: got %q, want %q", cfg.CustomMetricSpecification.Namespace, "CustomNamespace")
	}
	if cfg.CustomMetricSpecification.MetricName != "CustomMetric" {
		t.Errorf("MetricName: got %q, want %q", cfg.CustomMetricSpecification.MetricName, "CustomMetric")
	}
	if cfg.CustomMetricSpecification.Statistic != "Average" {
		t.Errorf("Statistic: got %q, want %q", cfg.CustomMetricSpecification.Statistic, "Average")
	}
	if len(cfg.CustomMetricSpecification.Dimensions) != 2 {
		t.Errorf("Dimensions: got %d, want %d", len(cfg.CustomMetricSpecification.Dimensions), 2)
	}
	if cfg.CustomMetricSpecification.Dimensions["Dimension1"] != "Value1" {
		t.Errorf("Dimension1: got %q, want %q", cfg.CustomMetricSpecification.Dimensions["Dimension1"], "Value1")
	}
	if cfg.CustomMetricSpecification.Dimensions["Dimension2"] != "Value2" {
		t.Errorf("Dimension2: got %q, want %q", cfg.CustomMetricSpecification.Dimensions["Dimension2"], "Value2")
	}
}

// TestUnmarshalInvalidPolicy tests handling of invalid policy JSON.
func TestUnmarshalInvalidPolicy(t *testing.T) {
	invalidJSON := `[
      {
        "policy_name": "test-invalid",
        "policy_type": "InvalidType",
        "target_tracking_configuration": {
          "target_value": "not-a-number"
        }
      }
    ]`

	var policies []PolicyDef
	if err := json.Unmarshal([]byte(invalidJSON), &policies); err == nil {
		t.Error("Expected error for invalid policy JSON, got nil")
	}
}

// TestCheckScalableTarget tests the checkScalableTarget function
func TestCheckScalableTarget(t *testing.T) {
	// Create a mock context
	ctx := context.Background()

	// Test cases
	tests := []struct {
		name     string
		resource string
		minCap   int32
		maxCap   int32
		mock     *mockAASClient
		want     bool
		wantErr  bool
	}{
		{
			name:     "valid target",
			resource: "service/test-cluster/test-service",
			minCap:   1,
			maxCap:   10,
			mock: &mockAASClient{
				describeScalableTargetsOutput: &applicationautoscaling.DescribeScalableTargetsOutput{
					ScalableTargets: []aasTypes.ScalableTarget{
						{
							MinCapacity: aws.Int32(1),
							MaxCapacity: aws.Int32(10),
						},
					},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name:     "invalid target",
			resource: "service/invalid-cluster/invalid-service",
			minCap:   1,
			maxCap:   10,
			mock: &mockAASClient{
				describeScalableTargetsOutput: &applicationautoscaling.DescribeScalableTargetsOutput{
					ScalableTargets: []aasTypes.ScalableTarget{},
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name:     "error case",
			resource: "service/error-cluster/error-service",
			minCap:   1,
			maxCap:   10,
			mock: &mockAASClient{
				describeScalableTargetsError: fmt.Errorf("mock error"),
			},
			want:    false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checkScalableTarget(ctx, tt.mock, tt.resource, tt.minCap, tt.maxCap)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkScalableTarget() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("checkScalableTarget() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCheckScalingPolicy tests the checkScalingPolicy function
func TestCheckScalingPolicy(t *testing.T) {
	// Create a mock context
	ctx := context.Background()

	// Test cases
	tests := []struct {
		name       string
		resource   string
		policyName string
		mock       *mockAASClient
		want       bool
		wantErr    bool
	}{
		{
			name:       "existing policy",
			resource:   "service/test-cluster/test-service",
			policyName: "test-policy",
			mock: &mockAASClient{
				describeScalingPoliciesOutput: &applicationautoscaling.DescribeScalingPoliciesOutput{
					ScalingPolicies: []aasTypes.ScalingPolicy{
						{
							PolicyName: aws.String("test-policy"),
						},
					},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name:       "non-existent policy",
			resource:   "service/test-cluster/test-service",
			policyName: "non-existent-policy",
			mock: &mockAASClient{
				describeScalingPoliciesOutput: &applicationautoscaling.DescribeScalingPoliciesOutput{
					ScalingPolicies: []aasTypes.ScalingPolicy{},
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name:       "error case",
			resource:   "service/error-cluster/error-service",
			policyName: "error-policy",
			mock: &mockAASClient{
				describeScalingPoliciesError: fmt.Errorf("mock error"),
			},
			want:    false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checkScalingPolicy(ctx, tt.mock, tt.resource, tt.policyName)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkScalingPolicy() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("checkScalingPolicy() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCheckCloudWatchAlarm tests the checkCloudWatchAlarm function
func TestCheckCloudWatchAlarm(t *testing.T) {
	// Create a mock context
	ctx := context.Background()

	// Test cases
	tests := []struct {
		name      string
		alarmName string
		mock      *mockCWClient
		want      bool
		wantErr   bool
	}{
		{
			name:      "existing alarm",
			alarmName: "test-alarm",
			mock: &mockCWClient{
				describeAlarmsOutput: &cloudwatch.DescribeAlarmsOutput{
					MetricAlarms: []cwTypes.MetricAlarm{
						{
							AlarmName: aws.String("test-alarm"),
						},
					},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name:      "non-existent alarm",
			alarmName: "non-existent-alarm",
			mock: &mockCWClient{
				describeAlarmsOutput: &cloudwatch.DescribeAlarmsOutput{
					MetricAlarms: []cwTypes.MetricAlarm{},
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name:      "error case",
			alarmName: "error-alarm",
			mock: &mockCWClient{
				describeAlarmsError: fmt.Errorf("mock error"),
			},
			want:    false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checkCloudWatchAlarm(ctx, tt.mock, tt.alarmName)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkCloudWatchAlarm() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("checkCloudWatchAlarm() = %v, want %v", got, tt.want)
			}
		})
	}
}
