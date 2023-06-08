package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pkg/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

func main() {
	args := os.Args
	if len(args) < 2 {
		fmt.Println("Please, provide a cluster name")
		return
	}
	input := args[1]
	fileName := input + ".txt"

	// Get k8s clients
	kclient, dclient, restConfig, err := k8sClients()
	if err != nil {
		fmt.Println("error creating the k8s clients:", err)
		return
	}

	// Get namespaces list
	nsList, err := getNamespaces(kclient)
	if err != nil {
		fmt.Println("error getting the list of namespaces:", err)
		return
	}

	// Create the file to save the data
	file, err := os.Create(fileName)
	if err != nil {
		fmt.Println("error creating the file:", err)
		return
	}
	defer file.Close()

	// Get resources per namespace
	err = getNsResources(kclient, dclient, nsList, file)
	if err != nil {
		fmt.Println("error getting resources per namespace:", err)
		return
	}

	// Get multicluster service entries
	getXSE(dclient, file)
	if err != nil {
		fmt.Println("error getting multicluster SE:", err)
		return
	}

	// Get edge pod name
	edgePod, err := getEdgePod(kclient)
	if err != nil {
		fmt.Println("error getting edge pod:", err)
		return
	}

	// Get XCP metrics
	err = portForward(kclient, file, restConfig, edgePod)
	if err != nil {
		fmt.Println("error doing port-forwarding:", err)
		return
	}
}

func k8sClients() (*kubernetes.Clientset, dynamic.Interface, *rest.Config, error) {
	clientcfg := kube.BuildClientCmd("", "")
	restConfig, err := clientcfg.ClientConfig()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to get k8s config file: %v", err)
	}

	k8sClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to create k8s client: %v", err)
	}

	k8sDynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to create k8s dynamic client: %v", err)
	}

	return k8sClient, k8sDynClient, restConfig, nil
}

func getNamespaces(kclient *kubernetes.Clientset) ([]string, error) {
	var nsNames []string
	nsList, err := kclient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("unable to get the list of namespaces: %v", err)
	}

	for _, ns := range nsList.Items {
		if ns.Name != "kube-system" && ns.Name != "istio-system" && ns.Name != "istio-gateway" && ns.Name != "tsb" && ns.Name != "cert-manager" {
			nsNames = append(nsNames, ns.Name)
		}
	}

	return nsNames, nil
}

func getNsResources(kclient *kubernetes.Clientset, dclient dynamic.Interface, nsList []string, file *os.File) error {
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
			return err
		}
		svcNum = len(svcList.Items)

		// Get pods per namespace
		podList, err := kclient.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		podNum = len(podList.Items)

		// Get gateways
		gwList, err := dclient.Resource(gwRes).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		gwNum = len(gwList.Items)

		// Get hostname per gateway
		for _, gw := range gwList.Items {
			var gwObj v1alpha3.Gateway
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(gw.Object, &gwObj)
			if err != nil {
				return err
			}
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
			return err
		}
		vsNum = len(vsList.Items)

		// Get destination rules
		drList, err := dclient.Resource(drRes).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		drNum = len(drList.Items)

		// Get service entries
		seList, err := dclient.Resource(seRes).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		seNum = len(seList.Items)

		// Get T1 gateways
		t1List, err := dclient.Resource(t1Res).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		t1Num = len(t1List.Items)

		// Get ingressgateways
		t2List, err := dclient.Resource(t2Res).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		t2Num = len(t2List.Items)

		info := fmt.Sprintf("Namespace %v has %d services, %d pods, %d gateways with %d hostnames, %d virtual services, %d destination rules, %d service entries, %d tier1 gateway pods and %d ingressgateway pods.\n", ns, svcNum, podNum, gwNum, gwHostname, vsNum, drNum, seNum, t1Num, t2Num)
		_, err = file.WriteString(info)
		if err != nil {
			return err
		}
	}

	return nil
}

func getXSE(dclient dynamic.Interface, file *os.File) error {
	var mcSe int
	seRes := schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  "v1alpha3",
		Resource: "serviceentries",
	}

	// Get service entries
	seList, err := dclient.Resource(seRes).Namespace("xcp-multicluster").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	mcSe = len(seList.Items)

	info := fmt.Sprintf("%d multicluster service entries\n", mcSe)
	_, err = file.WriteString(info)
	if err != nil {
		return err
	}

	return nil
}

func getEdgePod(kclient *kubernetes.Clientset) (string, error) {
	podList, err := kclient.CoreV1().Pods("istio-system").List(context.TODO(), metav1.ListOptions{LabelSelector: "app=edge"})
	if err != nil {
		return "", err
	}

	if len(podList.Items) == 0 {
		return "", fmt.Errorf("edge pod does not exist in istio-system namespace")
	}

	podName := podList.Items[0].Name

	return podName, nil
}

func portForward(kclient *kubernetes.Clientset, file *os.File, restConfig *rest.Config, podName string) error {
	// Create the request to do port-forrward
	req := kclient.CoreV1().RESTClient().Post().Resource("pods").Name(podName).Namespace("istio-system").SubResource("portforward")
	req.URL().Path = fmt.Sprintf("/api/v1/namespaces/istio-system/pods/%s/portforward", podName)

	// Upgrade the request to a stream connection
	transport, upgrader, err := spdy.RoundTripperFor(restConfig)
	if err != nil {
		return err
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())

	readyCh := make(chan struct{}, 1)
	stopCh := make(chan struct{}, 1)

	portForwarder, err := portforward.New(dialer, []string{"8080:8080"}, stopCh, readyCh, os.Stdout, os.Stderr)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		err := portForwarder.ForwardPorts()
		if err != nil {
			fmt.Printf("error during port-forward: %v", err)
			return
		}
	}()

	<-readyCh

	err = request(file)
	if err != nil {
		return err
	}

	stopCh <- struct{}{}

	wg.Wait()

	return nil
}

func request(file *os.File) error {
	resp, err := http.Get("http://localhost:8080/metrics")
	if err != nil {
		return fmt.Errorf("error generating the request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading the response: %v", err)
	}

	_, err = file.Write(body)
	if err != nil {
		return err
	}

	return nil
}
