# Changes Summary for K0smotronControlPlane Version Format E2E Test

## Overview
Extended the existing CAPI Docker MachineDeployment test to verify K0smotronControlPlane version format handling as requested in the PR review.

## Files Created/Modified

### 1. Test Configuration Files
- **Maintained**: `config/samples/capi/docker/cluster-with-machinedeployment.yaml`
  - K0smotronControlPlane with version `v1.27.2-k0s.0`
  - Tests the scenario where spec.version has the `-k0s.` suffix
  - Kept as the default configuration for backward compatibility

- **Created**: `config/samples/capi/docker/cluster-with-machinedeployment-no-suffix.yaml`
  - K0smotronControlPlane with version `v1.27.2`
  - Tests the scenario where spec.version doesn't have the `-k0s.` suffix

### 2. Test Implementation
- **Modified**: `inttest/capi-docker-machinedeployment/capi_docker_test.go`
  - Split the test into two subtests: `WithK0sSuffix` and `WithoutK0sSuffix`
  - Added `getK0smotronControlPlaneStatus()` function to retrieve K0smotronControlPlane status
  - Added `verifyK0smotronControlPlaneVersionFormat()` function to verify version format consistency
  - Added `testCAPIDockerWithVersion()` function as the main test execution function
  - Imported `cpv1beta1` package for K0smotronControlPlane types

### 3. Documentation
- **Created**: `inttest/capi-docker-machinedeployment/README.md`
  - Documented the test scenarios and expected behavior
  - Explained the background and purpose of the test
  - Provided instructions for running the test

- **Created**: `inttest/capi-docker-machinedeployment/implementation-plan.md`
  - Detailed implementation plan for the test
  - Outlined the test objectives and verification steps

- **Created**: `inttest/capi-docker-machinedeployment/CHANGES.md` (this file)
  - Summary of all changes made

## Test Behavior

The test now verifies:

1. **Version Format Consistency**:
   - When `spec.version` has `-k0s.` suffix → `status.version` keeps the suffix
   - When `spec.version` has no suffix → `status.version` has no suffix

2. **Full Version Preservation**:
   - `status.k0sVersion` always contains the complete k0s version with `-k0s.` suffix

3. **MachineDeployment Compatibility**:
   - Both version formats work correctly with MachineDeployment
   - Worker nodes join the cluster successfully

## Running the Tests

```bash
# Run all subtests
make inttest TEST_PATTERN=TestCAPIDockerSuite

# Run specific subtest
make inttest TEST_PATTERN=TestCAPIDockerSuite/WithK0sSuffix
make inttest TEST_PATTERN=TestCAPIDockerSuite/WithoutK0sSuffix
```

## Related Commits

This test implementation addresses the PR review comment:
> "I wonder if it would be possible to create or extend some existing e2e test to cover this for the MD part?"

The test extends the existing MachineDeployment e2e test to cover the version format handling introduced in the following commits:
- `afbdc3b`: Added FormatStatusVersion function for version normalization
- `e21ed31`: Added K0sVersion field to preserve full version information
