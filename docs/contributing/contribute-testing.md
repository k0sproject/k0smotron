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
TBD
