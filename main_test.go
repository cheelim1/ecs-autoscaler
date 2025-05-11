package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

// TestParseInt_Valid ensures parseInt returns the correct integer for a valid string.
func TestParseInt_Valid(t *testing.T) {
	got, err := parseInt("123", "test")
	if err != nil {
		t.Errorf("parseInt valid: unexpected error: %v", err)
	}
	want := 123
	if got != want {
		t.Errorf("parseInt valid: got %d, want %d", got, want)
	}
}

// TestParseInt_Invalid ensures parseInt handles invalid input correctly.
func TestParseInt_Invalid(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	slog.SetDefault(logger)

	// Call parseInt with invalid input
	got, err := parseInt("invalid", "test")
	if err == nil {
		t.Error("parseInt invalid: expected error, got nil")
	}
	if got != 0 {
		t.Errorf("parseInt invalid: expected 0, got %d", got)
	}

	// Check if the error message contains expected text
	output := buf.String()
	if !strings.Contains(output, "invalid input") || !strings.Contains(output, "invalid") {
		t.Errorf("Expected error message not found in log output: %s", output)
	}
}

// testWriter is a custom io.Writer for testing
type testWriter struct {
	output *string
}

func (w *testWriter) Write(p []byte) (n int, err error) {
	*w.output = string(p)
	return len(p), nil
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
