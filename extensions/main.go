//go:build extension

package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	inplaceversionupdate "github.com/k0sproject/k0smotron/extensions/inplaceversionupdate"
	"github.com/spf13/pflag"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	runtimecatalog "sigs.k8s.io/cluster-api/exp/runtime/catalog"
	"sigs.k8s.io/cluster-api/exp/runtime/server"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// catalog contains all information about RuntimeHooks.
	catalog = runtimecatalog.New()

	// Flags.
	profilerAddress string
	webhookPort     int
	webhookCertDir  string
	logOptions      = logs.NewOptions()

	// Creates a logger to be used during the main func using controller runtime utilities
	// NOTE: it is not mandatory to use controller runtime utilities in custom RuntimeExtension, but it is recommended
	// because it makes log from those components similar to log from controllers.
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	// Adds to the catalog all the RuntimeHooks defined in cluster API.
	_ = runtimehooksv1.AddToCatalog(catalog)
}

// InitFlags initializes the flags.
func InitFlags(fs *pflag.FlagSet) {
	// Initialize logs flags using Kubernetes component-base machinery.
	logsv1.AddFlags(logOptions, fs)

	fs.StringVar(&profilerAddress, "profiler-address", "",
		"Bind address to expose the pprof profiler (e.g. localhost:6060)")

	fs.IntVar(&webhookPort, "webhook-port", 9443,
		"Webhook Server port")

	fs.StringVar(&webhookCertDir, "webhook-cert-dir", "/tmp/k8s-webhook-server/serving-certs/",
		"Webhook cert dir.")
}

func main() {
	// Initialize and parse command line flags.
	InitFlags(pflag.CommandLine)
	pflag.CommandLine.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	// Set log level 2 as default.
	if err := pflag.CommandLine.Set("v", "2"); err != nil {
		setupLog.Error(err, "Failed to set default log level")
		os.Exit(1)
	}
	pflag.Parse()

	// Validates logs flags using Kubernetes component-base machinery and applies them
	if err := logsv1.ValidateAndApply(logOptions, nil); err != nil {
		setupLog.Error(err, "Unable to start extension")
		os.Exit(1)
	}

	// Add the klog logger in the context.
	ctrl.SetLogger(klog.Background())

	// Initialize the golang profiler server, if required.
	if profilerAddress != "" {
		klog.Infof("Profiler listening for requests at %s", profilerAddress)
		go func() {
			klog.Info(http.ListenAndServe(profilerAddress, nil))
		}()
	}

	// Create a http server for serving runtime extensions
	webhookServer, err := server.New(server.Options{
		Catalog: catalog,
		Port:    webhookPort,
		CertDir: webhookCertDir,
	})
	if err != nil {
		setupLog.Error(err, "Error creating webhook server")
		os.Exit(1)
	}

	client, err := ctrlclient.New(ctrl.GetConfigOrDie(), ctrlclient.Options{})
	if err != nil {
		setupLog.Error(err, "Error creating controller-runtime client")
		os.Exit(1)
	}

	// Register extension handlers.
	if err := setupInPlaceVersionUpdateHookHandlers(webhookServer, client); err != nil {
		setupLog.Error(err, "Error setting up in-place update hook handlers")
		os.Exit(1)
	}

	// Setup a context listening for SIGINT.
	ctx := ctrl.SetupSignalHandler()

	// Start the https server.
	setupLog.Info("Starting Runtime Extension server")
	if err := webhookServer.Start(ctx); err != nil {
		setupLog.Error(err, "Error running webhook server")
		os.Exit(1)
	}
}

// setupInPlaceVersionUpdateHookHandlers sets up In-Place Version Update Hooks.
func setupInPlaceVersionUpdateHookHandlers(runtimeExtensionWebhookServer *server.Server, client ctrlclient.Client) error {

	ipvu := inplaceversionupdate.NewInPlaceVersionUpdateHandler(client)

	if err := runtimeExtensionWebhookServer.AddExtensionHandler(server.ExtensionHandler{
		Hook:        runtimehooksv1.CanUpdateMachine,
		Name:        "can-update-machine",
		HandlerFunc: inplaceversionupdate.DoCanUpdateMachine,
	}); err != nil {
		return fmt.Errorf("error adding CanUpdateMachine handler: %w", err)
	}

	if err := runtimeExtensionWebhookServer.AddExtensionHandler(server.ExtensionHandler{
		Hook:        runtimehooksv1.CanUpdateMachineSet,
		Name:        "can-update-machineset",
		HandlerFunc: inplaceversionupdate.DoCanUpdateMachineSet,
	}); err != nil {
		return fmt.Errorf("error adding CanUpdateMachineSet handler: %w", err)
	}

	if err := runtimeExtensionWebhookServer.AddExtensionHandler(server.ExtensionHandler{
		Hook:        runtimehooksv1.UpdateMachine,
		Name:        "update-machine",
		HandlerFunc: ipvu.DoUpdateMachine,
	}); err != nil {
		return fmt.Errorf("error adding UpdateMachine handler: %w", err)
	}

	return nil
}
