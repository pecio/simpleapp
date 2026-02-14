package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

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
	namespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Starting SimpleApp controller in namespace %v", string(namespace))

	oac := clientset.OpenAPIV3()
	if oac == nil {
		log.Fatal("OpenAPI V3 is not available")
	}
	paths, err := oac.Paths()
	if err != nil {
		log.Fatal(err)
	}

	rp := paths["apis/apps.raulpedroche.es/v1alpha1"]
	if rp == nil {
		log.Fatal("Resource Path for apps.raulpedroche.es/v1alpha1 not found")
	}

	simpleApps := make(map[string]SimpleApp, 0)

	for {
		result := clientset.RESTClient().Get().AbsPath(`/apis/apps.raulpedroche.es/v1alpha1`).Namespace(string(namespace)).Resource("SimpleApps").Do(context.TODO())
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

		time.Sleep(10 * time.Second)
	}
}
