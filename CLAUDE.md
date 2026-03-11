# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

ECS AutoScaler is a **GitHub Action** (Docker-based) written in Go that manages AWS ECS Service auto-scaling policies and CloudWatch alarms. It registers/deregisters Application Auto Scaling targets, creates step-scaling and target-tracking policies, and manages associated CloudWatch metric alarms.

## Build and Development Commands

```bash
# Build
go build -o ecs-autoscaler

# Run tests
go test -v ./...

# Run tests with race detection and coverage
go test -race -covermode atomic -coverprofile=covprofile ./...

# Run a single test
go test -v -run TestFunctionName ./...

# Vet
go vet ./...

# Static analysis (install first: go install honnef.co/go/tools/cmd/staticcheck@latest)
staticcheck ./...
```

## Architecture

This is a single-file Go application (`main.go`) with tests in `main_test.go`. There are no subpackages.

### How it runs

The binary is invoked via Docker (`Dockerfile`) as a GitHub Action (`action.yml`). It receives **16 positional CLI arguments** (os.Args[1..16]) passed from the action inputs in `action.yml`. The argument order is fixed and must match between `action.yml` args and `main()` parsing.

### Core flow in `main()`

1. **Parse args** - 16 positional args: AWS creds, region, cluster, service, enabled flag, capacity bounds, cooldowns, CPU/memory thresholds, default-policies JSON, scaling-policies JSON
2. **If `enabled=false`** - Cleanup path: check existence of scalable target, delete alarms, delete policies, deregister target
3. **If `enabled=true`** - Register scalable target, then either:
   - Apply **custom policies** (`scaling-policies` or `default-policies` JSON) with idempotent create/update logic
   - Apply **built-in default** CPU+Memory step-scaling policies with CloudWatch alarms

### Key design decisions

- **Idempotent**: Compares existing AWS state before making changes (`compareScalingPolicy`, `checkScalableTarget`)
- **Alarm safety**: Only creates CloudWatch alarms for **new** policies; never overwrites existing alarms to avoid "Multiple alarms attached" warnings
- **Custom alarm creation**: Only triggers when both `metric_name` and `metric_namespace` are set in the policy JSON
- **Scale direction**: `scale_direction` field ("in"/"out") on `PolicyDef` controls which threshold (in vs out) is used for alarm creation

### AWS SDK interfaces

`AASClient` and `CWClient` interfaces wrap the AWS SDK clients for Application Auto Scaling and CloudWatch respectively. Tests use mock implementations (`mockAASClient`, `mockCWClient`) of these interfaces.

### Naming conventions for AWS resources

- Scaling policies: `{cluster}-{service}-scale-out`, `{cluster}-{service}-scale-in`
- CloudWatch alarms: `{cluster}-{service}-cpu-high`, `{cluster}-{service}-cpu-low`, `{cluster}-{service}-mem-high`, `{cluster}-{service}-mem-low`
- Custom policy alarms: `{cluster}-{service}-{policy_name}`

## CI/CD

- **code-check.yml**: Runs `go test` with coverage + `go vet` + `staticcheck` on PRs and pushes to main
- **codeql.yml**: Runs GitHub CodeQL analysis for Go on pushes/PRs to main and weekly schedule
- **release.yml**: Builds and pushes Docker image to GHCR on version tags (`v*`)
- **dependabot.yml**: Automated dependency updates for Go modules, Docker, and GitHub Actions (weekly)
