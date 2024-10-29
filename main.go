package main

import (
	"fmt"
	"time"

	"github.com/spf13/pflag"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var (
	namespace   string
	kubecontext string
)

func main() {
	pflag.StringVarP(&kubecontext, "context", "c", "apps-sandbox1-us-ce1-lg8", "the kubernetes context the deployment will be on")
	pflag.StringVarP(&namespace, "namespace", "n", apiv1.NamespaceDefault, "the namespace the deployment will be on")
	pflag.Parse()

	cfg, err := config.GetConfigWithContext(kubecontext)
	if err != nil {
		fmt.Printf("Config Error: %v\n", err)
		return
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		fmt.Printf("ClientSet Error: %v\n", err)
		return
	}

	// Create deployment listwatcher
	deploymentListWatcher := cache.NewListWatchFromClient(
		clientset.AppsV1().RESTClient(),
		"deployments",
		namespace,
		fields.Everything(),
	)

	informerOptions := cache.InformerOptions{
		ListerWatcher: deploymentListWatcher,
		ObjectType:    &appsv1.Deployment{},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    addFunc,
			UpdateFunc: updateFunc,
			DeleteFunc: deleteFunc,
		},
	}

	store, controller := cache.NewInformerWithOptions(informerOptions)

	store.Add(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-deployment",
			Namespace: namespace,
		},
	})

	// Create a channel with empty struct (0 bytes of memory)
	// Only care about signalling, not sending data
	stopCh := make(chan struct{})

	// Defer, so closes channel when main() ends/exits
	defer close(stopCh)

	// From Claude:
	// But main() won't end until this ends
	// This only ends when something externally triggers program termination
	// E.g. SIGTERM signal or Ctrl+C (which sends SIGINT signal)
	// Then the controller goes through graceful shutdown
	// See this in the `select` blocks when clicking through
	go controller.Run(stopCh)

	// https://web.archive.org/web/20240317164624/https://docs.bitnami.com/tutorials/a-deep-dive-into-kubernetes-controllers
	if !cache.WaitForCacheSync(stopCh, controller.HasSynced) {
		fmt.Println("Timed out waiting for caches to sync")
		return
	}

	// This blocks until something closes the channel
	// This is a channel receiving without a variable to receive it
	<-stopCh
}

func addFunc(obj interface{}) {
	if deployment, ok := obj.(*appsv1.Deployment); ok {
		fmt.Printf("\nNew Deployment Added: %s\n", deployment.Name)
	}
}

func updateFunc(old, new interface{}) {
	oldDeployment, ok := old.(*appsv1.Deployment)
	if !ok {
		return
	}

	newDeployment, ok := new.(*appsv1.Deployment)
	if !ok {
		return
	}

	fmt.Printf("\nDeployment Updated: %s -> %s\n", oldDeployment.Name, newDeployment.Name)

	fmt.Println("\n\t[OLD]")
	printDeployment(oldDeployment)

	fmt.Println("\n\t[NEW]")
	printDeployment(newDeployment)
}

func deleteFunc(obj interface{}) {
	if deployment, ok := obj.(*appsv1.Deployment); ok {
		fmt.Printf("\nDeployment Deleted: %s\n", deployment.Name)
	}
}

func printDeployment(deployment *appsv1.Deployment) {
	fmt.Printf("\tDeployment: %s\n", deployment.Name)
	fmt.Printf("\tNamespace: %s\n", deployment.Namespace)
	fmt.Printf("\tCreated: %s\n", deployment.CreationTimestamp.Format(time.RFC3339))
	if deployment.Spec.Replicas != nil {
		fmt.Printf("\tReplicas: %d\n", *deployment.Spec.Replicas)
	}

	if deployment.Annotations != nil {
		fmt.Print("\tAnnotations:\n")
		for k, v := range deployment.Annotations {
			fmt.Printf("\t  %s: %s\n", k, v)
		}
	}

	if deployment.Spec.Template.Spec.Containers != nil {
		fmt.Print("\tContainers:\n")
		for _, container := range deployment.Spec.Template.Spec.Containers {
			fmt.Printf("\t  - Name: %s\n", container.Name)
			fmt.Printf("\t    Image: %s\n", container.Image)
		}
	}

	fmt.Print("\tStatus:\n")
	fmt.Printf("\t  Available Replicas: %d\n", deployment.Status.AvailableReplicas)
	fmt.Printf("\t  Ready Replicas: %d\n", deployment.Status.ReadyReplicas)
	fmt.Printf("\t  Updated Replicas: %d\n", deployment.Status.UpdatedReplicas)
}
