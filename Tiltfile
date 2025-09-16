# -*- mode: Python -*-

config.define_bool("debug")
cfg = config.parse()
debug = cfg.get('debug', False)

print("Debug mode is", debug)

# Deploy cert-manager if not already present.
# This is required for the k0smotron webhook to function correctly.
local('kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.18.2/cert-manager.yaml')
# wait for the service to become available
local('kubectl wait --for=condition=available deployment/cert-manager deployment/cert-manager-cainjector deployment/cert-manager-webhook -n cert-manager --timeout=300s')


# including 'all=N -l' in gcflags disables optimizations and inlining, making it easier to debug the code.
compile_cmd = 'CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -gcflags "all=-N -l" -o .tiltbuild/bin/manager cmd/main.go'

k0smotron_deployment_name = 'k0smotron-controller-manager'

local_resource(
    "k0smotron_binary",
    compile_cmd,
    deps=[
        'cmd',
        'api/k0smotron.io',
        'internal/controller/k0smotron.io',
        'internal/controller/util',
        'internal/exec',
        'internal/util',
        'go.mod',
        'go.sum',
    ],
    ignore=['**/*_test.go'])

dockerfile_contents = """
# Tilt image
FROM golang:1.24.6 as tilt-helper
# Install delve. Note this should be kept in step with the Go release minor version.
RUN go install github.com/go-delve/delve/cmd/dlv@v1.24

FROM golang:1.24.6 as tilt
WORKDIR /
COPY --from=tilt-helper /go/bin/dlv .
COPY manager .
"""

# We build development image with delve installed and the binary compiled without optimizations and inlining.
# The controller image is only built once the '.tiltbuild/bin/manager' binary is built by the local_resource 
# above.
docker_build(
    ref = k0smotron_deployment_name,
    context = ".tiltbuild/bin/",
    dockerfile_contents = dockerfile_contents,
    only = "manager")

standalone_install_path = './install-standalone.yaml'
dlv_command = ["/dlv", "exec", "./manager", "--headless", "--listen=:30000", "--api-version=2", "--accept-multiclient"]

# Modify the install-standalone.yaml to use the development image with delve and remove the securityContext
# that prevents the container from running as root, which is required for a good integration with Tilt.
objects = read_yaml_stream(standalone_install_path)
for o in objects:
    if o['kind'] == 'Deployment' and o['metadata']['name'] == k0smotron_deployment_name:
        # Use the development image with delve installed.
        o['spec']['template']['spec']['securityContext'] = None

        # If debug mode is enabled, wrap the controller manager command with dlv.
        if debug:
            # If container manager is 'manager', change its command to wrap with dlv.
            for c in o['spec']['template']['spec']['containers']:
                if c['name'] == 'manager':
                    if len(c['args']) > 0:
                        # Append the original args after a '--' to the dlv command.
                        dlv_command.append('--')
                        for arg in c['args']:
                            dlv_command.append(arg)
                        c['args'] = []
                        
                    c['command'] = dlv_command

                    # Increase container memory limit to 512Mi.
                    if 'resources' not in c:
                        c['resources'] = {}
                    if 'limits' not in c['resources']:
                        c['resources']['limits'] = {}
                    c['resources']['limits']['memory'] = '512Mi'

                    # Remove liveness and readiness probes to avoid interfering with the debugger.
                    c['livenessProbe'] = None
                    c['readinessProbe'] = None

k8s_yaml(encode_yaml_stream(objects))

# workload name is the name of the k0smotron controller manager deployment.
k8s_resource(k0smotron_deployment_name, port_forwards='30000:30000', resource_deps=['k0smotron_binary'])