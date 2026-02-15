package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

	// Get our namespace
	namespaceBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		log.Fatal(err)
	}
	namespace := string(namespaceBytes)
	log.Printf("Starting SimpleApp controller in namespace %v", namespace)

	oac := clientset.OpenAPIV3()
	if oac == nil {
		log.Fatal("OpenAPI V3 is not available")
	}
	paths, err := oac.Paths()
	if err != nil {
		log.Fatal(err)
	}

	rp := paths["apis/"+resourcePath]
	if rp == nil {
		log.Fatalf("Resource Path for %v not found", resourcePath)
	}

	simpleApps := make(map[string]SimpleApp, 0)

	for {
		result := clientset.RESTClient().Get().AbsPath("/apis/" + resourcePath).Namespace(namespace).Resource(plural).Do(context.TODO())
		if result.Error() != nil {
			log.Fatal(result.Error())
		}
		content, err := result.Raw()
		if err != nil {
			log.Fatal(err)
		}

		var simpleAppList SimpleAppList
		err = json.Unmarshal(content, &simpleAppList)
		if err != nil {
			log.Fatal(err)
		}

		labelSelector := labels.Set(map[string]string{managedByLabel: managedByValue}).String()
		// Fetch managed Deployments
		deployments, err := clientset.AppsV1().Deployments(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			log.Fatalf("Got %v listing Deployments", err)
		}

		// Fetch managed services
		services, err := clientset.CoreV1().Services(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			log.Fatalf("Got %v listing Services", err)
		}

		// Store the list of previously existing apps
		oldAppSet := make(map[string]struct{}, len(simpleApps))
		for name := range simpleApps {
			oldAppSet[name] = struct{}{}
		}

		for _, simpleApp := range simpleAppList.Items {
			// Does App already exist?
			_, ok := simpleApps[simpleApp.Metadata.Name]
			if ok {
				// Remove from oldAppList
				delete(oldAppSet, simpleApp.Metadata.Name)
			} else {
				log.Printf("SimpleApp %v.%v appeared", simpleApp.Metadata.Namespace, simpleApp.Metadata.Name)
			}
			err := simpleApp.createOrUpdate(clientset)
			if err != nil {
				log.Printf("Got %v creating or updating %v", err, simpleApp.Metadata.Name)
			}
			// Store updated SimpleApp
			simpleApps[simpleApp.Metadata.Name] = simpleApp
		}

		// Delete no longer existing apps
		for name := range oldAppSet {
			log.Printf("SimpleApp %v.%v disappeared", simpleApps[name].Metadata.Namespace, simpleApps[name].Metadata.Name)
			err := simpleApps[name].delete(clientset)
			if err != nil {
				log.Printf("Got %v deleting %v", err, simpleApps[name].Metadata.Name)
			}
			delete(simpleApps, name)
		}

		// Reap orphan Deployments
	deployments:
		for _, deployment := range deployments.Items {
			for simpleAppName := range simpleApps {
				if deployment.Name == simpleAppName {
					continue deployments
				}
			}
			log.Printf("Reaping orphan Deployment %v.%v", deployment.Namespace, deployment.Name)
			err := clientset.AppsV1().Deployments(namespace).Delete(context.TODO(), deployment.Name, metav1.DeleteOptions{})
			if err != nil {
				log.Printf("Got %v deleting Deployment %v.%v", deployment.Namespace, deployment.Name)
			}
		}

		// Reap orphan Services
	services:
		for _, service := range services.Items {
			for simpleAppName := range simpleApps {
				if service.Name == simpleAppName {
					continue services
				}
			}
			log.Printf("Reaping orphan Service %v.%v", service.Namespace, service.Name)
			err := clientset.CoreV1().Services(namespace).Delete(context.TODO(), service.Name, metav1.DeleteOptions{})
			if err != nil {
				log.Printf("Got %v deleting Service %v.%v", service.Namespace, service.Name)
			}
		}

		time.Sleep(10 * time.Second)
	}
}
