/*
Copyright 2017 The OpenEBS Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package app

import (
	"flag"
	"os"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"k8s.io/klog"

	clientset "github.com/openebs/maya/pkg/client/generated/clientset/versioned"
	informers "github.com/openebs/maya/pkg/client/generated/informers/externalversions"
	ndmclientset "github.com/openebs/maya/pkg/client/generated/openebs.io/ndm/v1alpha1/clientset/internalclientset"
	"github.com/openebs/maya/pkg/signals"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeconfig = flag.String("kubeconfig", "", "Path for kube config")
)

const (
	// ResyncInterval is sync interval of the watcher
	ResyncInterval = 30 * time.Second
)

// Start starts the cstor-operator.
func Start() error {
	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()
	klog.InitFlags(nil)
	err := flag.Set("logtostderr", "true")
	if err != nil {
		return errors.Wrap(err, "failed to set logtostderr flag")
	}
	flag.Parse()

	cfg, err := getClusterConfig(*kubeconfig)
	if err != nil {
		return errors.Wrap(err, "error building kubeconfig")
	}

	// Building Kubernetes Clientset
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return errors.Wrap(err, "error building kubernetes clientset")
	}

	// Building OpenEBS Clientset
	openebsClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		return errors.Wrap(err, "error building openebs clientset")
	}

	// Building NDM Clientset
	ndmClient, err := ndmclientset.NewForConfig(cfg)
	if err != nil {
		return errors.Wrap(err, "error building ndm clientset")
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, getSyncInterval())
	spcInformerFactory := informers.NewSharedInformerFactory(openebsClient, getSyncInterval())
	// Build() fn of all controllers calls AddToScheme to adds all types of this
	// clientset into the given scheme.
	// If multiple controllers happen to call this AddToScheme same time,
	// it causes panic with error saying concurrent map access.
	// This lock is used to serialize the AddToScheme call of all controllers.
	//controllerMtx.Lock()

	controller, err := NewControllerBuilder().
		withKubeClient(kubeClient).
		withOpenEBSClient(openebsClient).
		withNDMClient(ndmClient).
		withCSPCSynced(spcInformerFactory).
		withCSPCLister(spcInformerFactory).
		withRecorder(kubeClient).
		withEventHandler(spcInformerFactory).
		withWorkqueueRateLimiting().Build()

	// blocking call, can't use defer to release the lock
	//controllerMtx.Unlock()

	if err != nil {
		return errors.Wrapf(err, "error building controller instance")
	}

	go kubeInformerFactory.Start(stopCh)
	go spcInformerFactory.Start(stopCh)

	// Threadiness defines the number of workers to be launched in Run function
	return controller.Run(2, stopCh)
}

// GetClusterConfig return the config for k8s.
func getClusterConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	klog.V(2).Info("Kubeconfig flag is empty")
	return rest.InClusterConfig()
}

// getSyncInterval gets the resync interval from environment variable.
// If missing or zero then default to 30 seconds
// otherwise return the obtained value
func getSyncInterval() time.Duration {
	resyncInterval, err := strconv.Atoi(os.Getenv("RESYNC_INTERVAL"))
	if err != nil || resyncInterval == 0 {
		klog.Warningf("Incorrect resync interval %q obtained from env, defaulting to %q seconds", resyncInterval, ResyncInterval)
		return ResyncInterval
	}
	return time.Duration(resyncInterval) * time.Second
}
