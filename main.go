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
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type CustomController struct {
	controller cache.Controller
	store      cache.Store
	queue      workqueue.TypedRateLimitingInterface[string]
}

func NewCustomController(controller cache.Controller, store cache.Store, queue workqueue.TypedRateLimitingInterface[string]) *CustomController {
	return &CustomController{
		controller: controller,
		store:      store,
		queue:      queue,
	}
}

func (c *CustomController) Run(stopCh <-chan struct{}) {
	go c.controller.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.controller.HasSynced) {
		fmt.Println("Timed out waiting for caches to sync")
		return
	}
}

func main() {
	var (
		namespace   string
		kubecontext string
	)

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

	// [string] because processing resource (Deployment) keys, which are just strings -- i.e. "namespace/name"
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())

	informerOptions := cache.InformerOptions{
		ListerWatcher: deploymentListWatcher,
		ObjectType:    &appsv1.Deployment{},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if deployment, ok := obj.(*appsv1.Deployment); ok {
					fmt.Printf("\nNew Deployment Added: %s\n", deployment.Name)
				}
			},
			UpdateFunc: func(old, new interface{}) {
				oldDeployment, ok := old.(*appsv1.Deployment)
				if !ok {
					return
				}
				if newDeployment, ok := new.(*appsv1.Deployment); ok {
					fmt.Printf("\nDeployment Updated: %s -> %s\n", oldDeployment.Name, newDeployment.Name)
				}
			},
			DeleteFunc: func(obj interface{}) {
				if deployment, ok := obj.(*appsv1.Deployment); ok {
					fmt.Printf("\nDeployment Deleted: %s\n", deployment.Name)
				}
			},
		},
		Indexers: cache.Indexers{},
	}

	store, controller := cache.NewInformerWithOptions(informerOptions)

	customController := NewCustomController(controller, store, queue)

	store.Add(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-deployment",
			Namespace: namespace,
		},
	})

	stopCh := make(chan struct{}) // for controller
	exitCh := make(chan struct{}) // for displayMenu
	defer close(stopCh)

	go customController.Run(stopCh)

	go displayMenu(store, exitCh)

	// blocks until receive signal to exit from displayMenu
	<-exitCh
	fmt.Println("Exiting...")
}

// send-only channel as parameter
func displayMenu(store cache.Store, exitCh chan<- struct{}) {
	for {
		fmt.Println("\nOptions:")
		fmt.Println("1. List Deployments")
		fmt.Println("2. Read specific Deployment")
		fmt.Println("3. Exit")
		fmt.Print("> ")

		var choice int
		fmt.Scanln(&choice)

		switch choice {
		case 1:
			listDeployments(store)
		case 2:
			fmt.Println("\nEnter deployment name: ")
			fmt.Print("> ")
			var name string
			fmt.Scanln(&name)
			getDeployment(store, name)
		case 3:
			// Send exit signal
			exitCh <- struct{}{}
			return
		default:
			fmt.Println("Invalid choice")
		}
	}
}

func listDeployments(store cache.Store) {
	deployments := store.List()
	fmt.Printf("\nFound %d deployments:\n", len(deployments))
	for _, d := range deployments {
		deployment := d.(*appsv1.Deployment)
		fmt.Printf("- %s (Namespace: %s, Replicas: %d)\n", deployment.Name, deployment.Namespace, *deployment.Spec.Replicas)
	}
}

func getDeployment(store cache.Store, name string) {
	obj, exists, err := store.GetByKey(name)
	if err != nil {
		fmt.Printf("Error retrieving deployment: %v\n", err)
		return
	}
	if !exists {
		fmt.Printf("Deployment %s not found\n", name)
		return
	}
	deployment := obj.(*appsv1.Deployment)
	fmt.Println()
	printDeployment(deployment)
}

func printDeployment(deployment *appsv1.Deployment) {
	fmt.Printf("Deployment: %s\n", deployment.Name)
	fmt.Printf("Namespace: %s\n", deployment.Namespace)
	fmt.Printf("Created: %s\n", deployment.CreationTimestamp.Format(time.RFC3339))
	if deployment.Spec.Replicas != nil {
		fmt.Printf("Replicas: %d\n", *deployment.Spec.Replicas)
	}

	if deployment.Annotations != nil {
		fmt.Print("Annotations:\n")
		for k, v := range deployment.Annotations {
			fmt.Printf("  %s: %s\n", k, v)
		}
	}

	if deployment.Spec.Template.Spec.Containers != nil {
		fmt.Print("Containers:\n")
		for _, container := range deployment.Spec.Template.Spec.Containers {
			fmt.Printf("  - Name: %s\n", container.Name)
			fmt.Printf("    Image: %s\n", container.Image)
		}
	}

	fmt.Print("Status:\n")
	fmt.Printf("  Available Replicas: %d\n", deployment.Status.AvailableReplicas)
	fmt.Printf("  Ready Replicas: %d\n", deployment.Status.ReadyReplicas)
	fmt.Printf("  Updated Replicas: %d\n", deployment.Status.UpdatedReplicas)
}
