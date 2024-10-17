package main

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"github.com/spf13/pflag"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var (
	namespace string
	kubecontext string
)

func main() {
	pflag.StringVarP(&namespace, "namespace", "n", apiv1.NamespaceDefault, "the namespace the deployment will be on")
	pflag.StringVarP(&kubecontext, "context", "c", "", "the kubernetes context the deployment will be on")
	pflag.Parse()

	cfg, err := config.GetConfigWithContext(kubecontext)
	if err != nil {
		fmt.Printf("Config Error: %s\n", err)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		fmt.Printf("ClientSet Error: %s\n", err)
	}

	deploymentsClient := clientset.AppsV1().Deployments(namespace)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "demo-deployment",
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
		fmt.Printf("Deployment Creation Error: %s\n", err)
	}
	fmt.Println(result)

	prompt()
	fmt.Printf("Listing deployments in namespace %q:\n", apiv1.NamespaceDefault)
	list, err := deploymentsClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	for _, d := range list.Items {
		fmt.Printf(" * %s (%d replicas)\n", d.Name, *d.Spec.Replicas)
	}

	fmt.Println("Updating deployment...")
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, getErr := deploymentsClient.Get(context.TODO(), "demo-deployment", metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("failed to get latest version of Deployment: %v", getErr)
		}

		result.Spec.Replicas = int32Ptr(1)
		result.Spec.Template.Spec.Containers[0].Image = "nginx:1.13"
		_, updateErr := deploymentsClient.Update(context.TODO(), result, metav1.UpdateOptions{})
		return updateErr
	})
	if err != nil {
		fmt.Printf("Update Error: %s\n", err)
	}

	prompt()
	fmt.Println("Watching deployment...")
	watcher, err := deploymentsClient.Watch(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Printf("Deployment Watch Error: %s\n", err)
	}
	for event := range watcher.ResultChan() {
		if event.Type == watch.Deleted { watcher.Stop() }
		fmt.Printf("Event Type: %s\n", event.Type)
	}
}

func prompt() {
	fmt.Printf("-> Press Return key to continue.")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		break
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	fmt.Println()
}

func int32Ptr(i int32) *int32 { return &i }
