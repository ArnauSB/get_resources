package main

import (
	"context"
	"fmt"

	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pkg/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

func main() {
	// Get k8s clients
	kclient, dclient := k8sClients()

	// Get namespaces
	fmt.Println("Looking for namespaces...")
	nsList := getNamespaces(kclient)

	// Get resources per namespace
	getNsResources(kclient, dclient, nsList)

	// Get multicluster service entries
	getXSE(dclient)
}

func k8sClients() (kubernetes.Interface, dynamic.Interface) {
	clientcfg := kube.BuildClientCmd("", "")
	restConfig, errConfig := clientcfg.ClientConfig()
	if errConfig != nil {
		panic(fmt.Sprintf("unable to get k8s config file: %v", errConfig))
	}

	k8sClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		panic(err)
	}
	k8sDynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		panic(err)
	}

	return k8sClient, k8sDynClient
}

func getNamespaces(kclient kubernetes.Interface) []string {
	var nsNames []string
	nsList, err := kclient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	for _, ns := range nsList.Items {
		if ns.Name != "kube-system" && ns.Name != "istio-system" && ns.Name != "istio-gateway" && ns.Name != "tsb" && ns.Name != "cert-manager" {
			nsNames = append(nsNames, ns.Name)
		}
	}

	return nsNames
}

func getNsResources(kclient kubernetes.Interface, dclient dynamic.Interface, nsList []string) {
	var (
		svcNum     int
		podNum     int
		gwNum      int
		gwHostname int
		vsNum      int
		drNum      int
		seNum      int
		t1Num      int
		t2Num      int
	)

	gwRes := schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  "v1alpha3",
		Resource: "gateways",
	}
	vsRes := schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  "v1alpha3",
		Resource: "virtualservices",
	}
	drRes := schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  "v1alpha3",
		Resource: "destinationrules",
	}
	seRes := schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  "v1alpha3",
		Resource: "serviceentries",
	}
	t1Res := schema.GroupVersionResource{
		Group:    "install.tetrate.io",
		Version:  "v1alpha1",
		Resource: "tier1gateways",
	}
	t2Res := schema.GroupVersionResource{
		Group:    "install.tetrate.io",
		Version:  "v1alpha1",
		Resource: "ingressgateways",
	}

	for _, ns := range nsList {
		// Get services per namespace
		svcList, err := kclient.CoreV1().Services(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		svcNum = len(svcList.Items)

		// Get pods per namespace
		podList, err := kclient.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		podNum = len(podList.Items)

		// Get gateways
		gwList, err := dclient.Resource(gwRes).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		gwNum = len(gwList.Items)

		// Get hostname per gateway
		for _, gw := range gwList.Items {
			var gwObj v1alpha3.Gateway
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(gw.Object, &gwObj)
			if err != nil {
				panic(err.Error())
			}

			// Esto no va
			for _, gwCon := range gwObj.Spec.Servers {
				if gwCon.Port.Number == 15443 {
					continue
				}
				gwHostname = len(gwCon.Hosts)
			}
		}

		// Get virtual services
		vsList, err := dclient.Resource(vsRes).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		vsNum = len(vsList.Items)

		// Get destination rules
		drList, err := dclient.Resource(drRes).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		drNum = len(drList.Items)

		// Get service entries
		seList, err := dclient.Resource(seRes).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		seNum = len(seList.Items)

		// Get T1 gateways
		t1List, err := dclient.Resource(t1Res).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		t1Num = len(t1List.Items)

		// Get ingressgateways
		t2List, err := dclient.Resource(t2Res).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		t2Num = len(t2List.Items)

		fmt.Printf("Namespace %v has %d services, %d pods, %d gateways with %d hostnames, %d virtual services, %d destination rules, %d service entries, %d tier1 gateway pods and %d ingressgateway pods.\n", ns, svcNum, podNum, gwNum, gwHostname, vsNum, drNum, seNum, t1Num, t2Num)
	}
}

func getXSE(dclient dynamic.Interface) {
	var mcSe int
	seRes := schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  "v1alpha3",
		Resource: "serviceentries",
	}

	// Get service entries
	seList, err := dclient.Resource(seRes).Namespace("xcp-multicluster").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	mcSe = len(seList.Items)

	fmt.Printf("%d multicluster service entries", mcSe)
}
