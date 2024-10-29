package main

import (
	"fmt"
	"time"

	"github.com/spf13/pflag"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

/*
How the workqueue works:
- Makes use of watcher in the Informer to watch for add/update/delete events
- Client sends one of these events by creating/updating/deleting a deployment
- The key for this deployment gets added to queue in the Informer cache

- Program starts and controller starts a running worker as the Informer watches for events to add to queue
- In processItem(), the worker is always trying to Get() items from the queue (every 1 second with wait.Until)
- When a key gets added, it'll call doWork() to do work, which prints
- doWork() returns an err or nil
- if nil, then handleError() will Forget() (delete) the key from the queue
- if err, then handleError() will requeue 5 times (rate limited), and if all fail, then we will also Forget() (delete)

- Program ends with ctrl+c or by selecting 3 from displayMenu() which unblocks exitCh and ends main()
- which then also closes stopCh, and everything else ends as well

Refs:
- https://web.archive.org/web/20240317164624/https://docs.bitnami.com/tutorials/a-deep-dive-into-kubernetes-controllers
- https://github.com/kubernetes/client-go/blob/master/examples/workqueue/main.go
*/

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
	numWorkers := 1

	// From workqueue/queue.go:
	// ShutDown will cause q to ignore all new items added to it and
	// immediately instruct the worker goroutines to exit
	defer c.queue.ShutDown()

	fmt.Println("\nStarting Deployment controller")
	go c.controller.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.controller.HasSynced) {
		fmt.Println("Timed out waiting for caches to sync")
		return
	}

	for i := 0; i < numWorkers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
		fmt.Println("\nEnding shift")
	}

	// Need this blocker to give time for the workers to work
	<-stopCh
}

func (c *CustomController) runWorker() {
	// loops while processItem returns true
	for c.processItem() {
	}
}

func (c *CustomController) processItem() bool {
	// From workqueue/queue.go:
	// If shutdown = true, the caller should end their goroutine.
	// You must call Done with item when you have finished processing it.
	key, shutdown := c.queue.Get()
	if shutdown {
		// ends loop in runWorker()
		return false
	}

	// From workqueue/queue.go:
	// Done marks item as done processing
	// Since deferring, this happens when doWork() finishes
	defer c.queue.Done(key)

	err := c.doWork(key)
	c.handleErr(err, key)

	return true
}

// This is the method with all the business logic
// Here it just prints
// syncToStdout() from https://github.com/kubernetes/client-go/blob/master/examples/workqueue/main.go
func (c *CustomController) doWork(key string) error {
	obj, exists, err := c.store.GetByKey(key)
	if err != nil {
		fmt.Printf("\nError retrieving deployment for %s: %v\n", key, err)
		return err
	}
	if !exists {
		fmt.Printf("\nDeployment \"%s\" not found\n", key)
	} else {
		fmt.Printf("\nSync/Add/Update for Deployment: %s\n", obj.(*appsv1.Deployment).GetName())
	}
	return nil
}

func (c *CustomController) handleErr(err error, key string) {
	maxRetries := 5

	if err == nil {
		// If successful, want to Forget() to remove from queue and stop retrying
		// Forget() happens before calling Done() since Done() is called via defer
		// From workqueue/rate_limiting_queue.go:
		// Forget indicates that an item is finished being retried.
		// This only clears the `rateLimiter`, you still have to call `Done` on the queue.
		fmt.Printf("Successfully processed: %s\n", key)
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		fmt.Printf("\nError syncing deployment \"%v\": %v\n", key, err)

		c.queue.AddRateLimited(key)
		return
	}

	c.queue.Forget(key)

	fmt.Println("Failed to process deployment.")
	fmt.Printf("Dropping deployment \"%q\" out of the queue: %v\n", key, err)
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
			// Add items (keys) to queue when
			AddFunc: func(obj interface{}) {
				key, err := cache.MetaNamespaceKeyFunc(obj) // just returns key as "namespace/name"
				if err == nil {
					queue.Add(key)
					fmt.Println("\nAddFunc: Added to queue")
				}
			},
			UpdateFunc: func(old, new interface{}) {
				key, err := cache.MetaNamespaceKeyFunc(new)
				if err == nil {
					queue.Add(key)
					fmt.Println("\nUpdateFunc: Added to queue")
				}
			},
			DeleteFunc: func(obj interface{}) {
				// first checks if object is DeletedFinalStateUnknown
				// otherwise, just returns MetaNamespaceKeyFunc as well; key as "namespace/name"
				key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
				if err == nil {
					queue.Add(key)
					fmt.Println("\nDeleteFunc: Added to queue")
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
