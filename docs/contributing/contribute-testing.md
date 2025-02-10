# Testing guidelines

K0smotron follows a comprehensive testing strategy that includes unit, integration, and e2e testing to ensure the reliability and robustness of the system.

All unit, integration, and e2e tests are executed automatically on pull requests that are intended to be merged, ensuring that changes are thoroughly validated before integration into the main codebase. These tests are executed using [GitHub Actions](https://docs.github.com/en/actions), which provides a seamless and automated way to validate proposed changes. This layered approach ensures that proposed changes are both correct and meet the expected quality standards.

For details on the contribution process, including how to follow the GitHub workflow and ensure your changes pass all necessary validations, please refer to the [Contributing Workflow](contribute-workflow.md).

## Unit and Integration Testing

In k0smotron project, tests are prioritized to execute quickly in order to maintain a fast feedback loop, ensuring efficient and agile development.

K0smotron uses [go test](https://pkg.go.dev/testing) as the foundation for all our tests, combined with the [testify](https://pkg.go.dev/github.com/stretchr/testify) library to simplify assertions and improve test readability and reliability. Both unit and integration tests rely on this approach to ensure consistency and ease of development.

- **Unit Testing**: Focused on isolated components, unit tests validate the correctness of individual functions or methods in a controlled environment, ensuring they behave as expected.

- **Integration Testing**: For integration testing, k0smotron uses [envtest](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest), which provides a lightweight Kubernetes control plane with the essential components needed to test interactions between the code and Kubernetes API. This approach allows us to validate the behavior of our controllers and other Kubernetes integrations in a reproducible and efficient manner without requiring a full cluster. 

  Following the best practices suggested in the [Cluster API documentation](https://cluster-api.sigs.k8s.io/developer/core/testing), integration tests are written using [**generic infrastructure providers**](https://cluster-api.sigs.k8s.io/developer/core/testing#generic-providers) rather than a specific provider. This ensures that tests remain agnostic and reusable across different infrastructures, fostering better maintainability and adaptability.

## E2E testing

K0smotron's end-to-end (E2E) testing leverages the [CAPI E2E framework](https://pkg.go.dev/sigs.k8s.io/cluster-api/test/framework) to provide configurability and utilities that support various phases of E2E testing, including the creation and configuration of the management cluster, waiting for specific resources, log dumping, and more.

To fully utilize CAPI's E2E framework, it is necessary to integrate [Ginkgo](https://onsi.github.io/ginkgo/) into the project. However, in K0smotron, we intentionally avoid using this testing framework for several reasons, primarily to maintain a unified approach to writing tests using standard Go testing conventions. As a result, certain methods from CAPI's E2E framework have been reimplemented within K0smotron to remove their direct dependency on Ginkgo.

### Run E2E

You can run the tests using the command:

``` cmd
make e2e
```

This will perform the following actions:

1. Deploy a local cluster using [Kind](https://github.com/kubernetes-sigs/kind) as the management cluster.
2. Install the desired providers. Basically the same achieved by executing the command `clusterctl init ...`, by including:
  - Cluster API for core components.
  - k0smotron as controlplane and bootstrap provider.
  - Configurable infrastructure provider (currently only docker supported). 
  
1. Execute the E2E test suite.

> NOTE: This command will run the tests using docker as infrastructure provider but it is intended to make use of the configurability offered by the CAPI E2E framework to add other infrastructure providers that can be used in e2e testing.