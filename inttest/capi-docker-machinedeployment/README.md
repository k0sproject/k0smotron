# CAPI Docker MachineDeployment Version Format Test

This test verifies the K0smotronControlPlane version format handling with MachineDeployment.

## Test Scenarios

The test includes two scenarios to verify version format consistency:

### 1. With K0s Suffix (`WithK0sSuffix`)
- K0smotronControlPlane spec.version: `v1.27.2-k0s.0`
- Expected status.version: `v1.27.2-k0s.0` (keeps the suffix)
- Expected status.k0sVersion: `v1.27.2-k0s.0` (full version)

### 2. Without K0s Suffix (`WithoutK0sSuffix`)
- K0smotronControlPlane spec.version: `v1.27.2`
- Expected status.version: `v1.27.2` (no suffix)
- Expected status.k0sVersion: `v1.27.2-k0s.0` (full version with suffix)

## Test Files

- `capi_docker_test.go`: Main test implementation
- `../../config/samples/capi/docker/cluster-with-machinedeployment.yaml`: Cluster configuration with k0s suffix (default/backward compatible)
- `../../config/samples/capi/docker/cluster-with-machinedeployment-no-suffix.yaml`: Cluster configuration without k0s suffix

## What the Test Verifies

1. **Version Format Consistency**: The test ensures that `status.version` format matches `spec.version` format:
   - If `spec.version` has `-k0s.` suffix, `status.version` should also have it
   - If `spec.version` doesn't have `-k0s.` suffix, `status.version` should not have it

2. **Full Version Preservation**: The test verifies that `status.k0sVersion` always contains the complete k0s version string with the `-k0s.` suffix.

3. **MachineDeployment Compatibility**: The test ensures that MachineDeployment works correctly with both version formats and that worker nodes join the cluster successfully.

## Running the Test

The test is part of the integration test suite and can be run with:

```bash
make inttest TEST_PATTERN=TestCAPIDockerSuite
```

Or to run a specific subtest:

```bash
make inttest TEST_PATTERN=TestCAPIDockerSuite/WithK0sSuffix
make inttest TEST_PATTERN=TestCAPIDockerSuite/WithoutK0sSuffix
```

## Background

This test was added to verify the fix for version format mismatch issues between K0smotronControlPlane and Cluster API. The fix includes:

1. `FormatStatusVersion` function that normalizes `status.version` based on `spec.version` format
2. `K0sVersion` field in K0smotronControlPlaneStatus to preserve the full k0s version information

This ensures compatibility with Cluster API while maintaining complete version information for k0s operations.
