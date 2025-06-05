# E2E Test Implementation Plan for K0smotronControlPlane Version Format with MachineDeployment

## Overview
This document outlines the implementation plan for extending the existing e2e test to verify the version format handling in K0smotronControlPlane with MachineDeployment.

## Background
Two commits were made to fix version format mismatch issues:
1. Added `FormatStatusVersion` function to normalize status.version based on spec.version format
2. Added `K0sVersion` field to K0smotronControlPlaneStatus to preserve full version information

## Test Objectives
- Verify that when K0smotronControlPlane spec.version doesn't have `-k0s.` suffix, status.version also doesn't have it
- Verify that when K0smotronControlPlane spec.version has `-k0s.` suffix, status.version keeps it
- Ensure the full k0s version is preserved in status.k0sVersion field
- Confirm MachineDeployment works correctly with both version formats

## Implementation Details

### 1. Test Structure
Extend `inttest/capi-docker-machinedeployment/capi_docker_test.go` with two subtests:
- `WithK0sSuffix`: Tests with version `v1.27.2-k0s.0`
- `WithoutK0sSuffix`: Tests with version `v1.27.2`

### 2. Test Configuration Files
- Keep `cluster-with-machinedeployment.yaml`: K0smotronControlPlane with version `v1.27.2-k0s.0` (for backward compatibility)
- Create `cluster-with-machinedeployment-no-suffix.yaml`: K0smotronControlPlane with version `v1.27.2`

### 3. Key Functions to Implement
- `getK0smotronControlPlaneStatus()`: Retrieve K0smotronControlPlane status via REST API
- `verifyVersionFormat()`: Validate version format consistency
- `testCAPIDockerWithVersion()`: Main test execution function

### 4. Verification Steps
1. Apply cluster configuration
2. Wait for K0smotronControlPlane to be ready
3. Verify status.version format matches spec.version format
4. Verify status.k0sVersion contains complete version string
5. Wait for MachineDeployment to be ready
6. Confirm worker nodes join successfully

### 5. Expected Behavior
- When spec.version = `v1.27.2-k0s.0`:
  - status.version = `v1.27.2-k0s.0`
  - status.k0sVersion = `v1.27.2-k0s.0`

- When spec.version = `v1.27.2`:
  - status.version = `v1.27.2`
  - status.k0sVersion = `v1.27.2-k0s.0` (full version)

## Files to Modify/Create
1. `inttest/capi-docker-machinedeployment/capi_docker_test.go` - Extend with version format tests
2. `config/samples/capi/docker/cluster-with-machinedeployment.yaml` - Keep existing (with suffix)
3. `config/samples/capi/docker/cluster-with-machinedeployment-no-suffix.yaml` - Create new (without suffix)
