package main

import (
	"context"
	"log"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatal(err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	// Use the clientset to interact with the Kubernetes API
	for {
		// List all pods in all namespaces
		pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("There are %d pods in the cluster", len(pods.Items))
		for _, pod := range pods.Items {
			log.Printf("%s\t%s", pod.Namespace, pod.Name)
		}

		time.Sleep(10 * time.Second)
	}
}
