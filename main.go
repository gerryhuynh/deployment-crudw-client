package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/pflag"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var (
	namespace      string
	kubecontext    string
	action         string
	deploymentName string
)

func main() {
	pflag.StringVarP(&kubecontext, "context", "c", "", "the kubernetes context the deployment will be on")
	pflag.StringVarP(&namespace, "namespace", "n", apiv1.NamespaceDefault, "the namespace the deployment will be on")
	pflag.StringVarP(&action, "action", "a", "list", "CRUDW actions: create, get, list, update, delete, watch. Default: list")
	pflag.StringVarP(&deploymentName, "deployment", "d", "demo-deployment", "the deployment name. Use for create, get, update, delete actions. Default: 'demo-deployment'")
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

	deploymentsClient := clientset.AppsV1().Deployments(namespace)

	switch action {
	case "list":
		if err := listDeployments(deploymentsClient, namespace); err != nil {
			fmt.Println(err)
		}
	case "create":
		if err := createDeployment(deploymentsClient, deploymentName); err != nil {
			fmt.Println(err)
		}
	case "get":
		if err := getDeployment(deploymentsClient, deploymentName); err != nil {
			fmt.Println(err)
		}
	case "update":
		if err := updateDeployment(deploymentsClient, deploymentName); err != nil {
			fmt.Println(err)
		}
	case "delete":
		if err := deleteDeployment(deploymentsClient, deploymentName); err != nil {
			fmt.Println(err)
		}
	case "watch":
		if err := watchDeployment(deploymentsClient); err != nil {
			fmt.Println(err)
		}
	default:
		fmt.Println("Invalid action")
	}
}

func listDeployments(deploymentsClient v1.DeploymentInterface, namespace string) error {
	fmt.Printf("Listing deployments in namespace %q:\n", namespace)
	list, err := deploymentsClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list deployments: %v", err)
	}
	if len(list.Items) == 0 {
		fmt.Println("No deployments found")
		return nil
	}
	for _, d := range list.Items {
		fmt.Printf(" * %s (%d replicas)\n", d.Name, *d.Spec.Replicas)
	}
	return nil
}

func createDeployment(deploymentsClient v1.DeploymentInterface, deploymentName string) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: deploymentName,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "demo",
				},
			},
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "demo",
					},
				},
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{
							Name:  "web",
							Image: "nginx:1.12",
							Ports: []apiv1.ContainerPort{
								{
									Name:          "http",
									Protocol:      apiv1.ProtocolTCP,
									ContainerPort: 80,
								},
							},
						},
					},
				},
			},
		},
	}

	fmt.Println("Creating deployment...")
	result, err := deploymentsClient.Create(context.TODO(), deployment, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create deployment: %v", err)
	}
	printDeployment(result)
	return nil
}

func getDeployment(deploymentsClient v1.DeploymentInterface, deploymentName string) error {
	fmt.Println("Getting deployment...")
	result, getErr := deploymentsClient.Get(context.TODO(), deploymentName, metav1.GetOptions{})
	if getErr != nil {
		return fmt.Errorf("failed to get latest version of Deployment: %v", getErr)
	}
	printDeployment(result)
	return nil
}

func updateDeployment(deploymentsClient v1.DeploymentInterface, deploymentName string) error {
	fmt.Println("Updating deployment...")
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, getErr := deploymentsClient.Get(context.TODO(), deploymentName, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("failed to get latest version of Deployment: %v", getErr)
		}

		result.Spec.Replicas = int32Ptr(1)
		result.Spec.Template.Spec.Containers[0].Image = "nginx:1.13"
		_, updateErr := deploymentsClient.Update(context.TODO(), result, metav1.UpdateOptions{})
		return updateErr
	})
	if err != nil {
		return fmt.Errorf("failed to update deployment: %v", err)
	}
	return nil
}

func deleteDeployment(deploymentsClient v1.DeploymentInterface, deploymentName string) error {
	fmt.Println("Deleting deployment...")
	deletePolicy := metav1.DeletePropagationForeground
	if err := deploymentsClient.Delete(context.TODO(), deploymentName, metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}); err != nil {
		return fmt.Errorf("failed to delete deployment: %v", err)
	}
	fmt.Println("Deleted deployment.")
	return nil
}

func watchDeployment(deploymentsClient v1.DeploymentInterface) error {
	fmt.Println("Watching deployment client...")
	watcher, err := deploymentsClient.Watch(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to watch deployment client: %v", err)
	}
	for event := range watcher.ResultChan() {
		if event.Type == watch.Deleted {
			watcher.Stop()
		}
		fmt.Printf("Event Type: %s\n", event.Type)
	}
	return nil
}

func printDeployment(deployment *appsv1.Deployment) {
	fmt.Printf("Deployment: %s\n", deployment.Name)
	fmt.Printf("Namespace: %s\n", deployment.Namespace)
	fmt.Printf("Created: %s\n", deployment.CreationTimestamp.Format(time.RFC3339))
	fmt.Printf("Replicas: %d\n", *deployment.Spec.Replicas)

	fmt.Print("Labels:\n")
	for k, v := range deployment.Labels {
		fmt.Printf("  %s: %s\n", k, v)
	}

	fmt.Print("Annotations:\n")
	for k, v := range deployment.Annotations {
		fmt.Printf("  %s: %s\n", k, v)
	}

	fmt.Print("Containers:\n")
	for _, container := range deployment.Spec.Template.Spec.Containers {
		fmt.Printf("  - Name: %s\n", container.Name)
		fmt.Printf("    Image: %s\n", container.Image)
		fmt.Print("    Ports:\n")
		for _, port := range container.Ports {
			fmt.Printf("      - %s: %d\n", port.Name, port.ContainerPort)
		}
	}

	fmt.Print("Status:\n")
	fmt.Printf("  Available Replicas: %d\n", deployment.Status.AvailableReplicas)
	fmt.Printf("  Ready Replicas: %d\n", deployment.Status.ReadyReplicas)
	fmt.Printf("  Updated Replicas: %d\n", deployment.Status.UpdatedReplicas)

	fmt.Print("Conditions:\n")
	for _, condition := range deployment.Status.Conditions {
		fmt.Printf("  - Type: %s\n", condition.Type)
		fmt.Printf("    Status: %s\n", condition.Status)
		fmt.Printf("    Reason: %s\n", condition.Reason)
		fmt.Printf("    Message: %s\n", condition.Message)
		fmt.Printf("    Last Update: %s\n", condition.LastUpdateTime.Format(time.RFC3339))
	}
}

func int32Ptr(i int32) *int32 { return &i }
