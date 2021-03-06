/*
Copyright 2019 LitmusChaos Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bdd

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	rbacV1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	scheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/litmuschaos/chaos-operator/pkg/apis/litmuschaos/v1alpha1"
	chaosClient "github.com/litmuschaos/chaos-operator/pkg/client/clientset/versioned/typed/litmuschaos/v1alpha1"
	restclient "k8s.io/client-go/rest"
)

var (
	kubeconfig string
	config     *restclient.Config
	client     *kubernetes.Clientset
	clientSet  *chaosClient.LitmuschaosV1alpha1Client
)

func TestChaos(t *testing.T) {

	RegisterFailHandler(Fail)
	RunSpecs(t, "BDD test")
}

var _ = BeforeSuite(func() {

	var err error
	kubeconfig = os.Getenv("HOME") + "/.kube/config"
	config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	Expect(err).To(BeNil(), "failed to get config")

	client, err = kubernetes.NewForConfig(config)
	Expect(err).To(BeNil(), "failed to get client")

	clientSet, err = chaosClient.NewForConfig(config)
	Expect(err).To(BeNil(), "failed to get clientSet")

	err = v1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).To(BeNil())

	//Creating crds
	By("creating chaosengine crd")
	err = exec.Command("kubectl", "apply", "-f", "../../deploy/chaos_crds.yaml").Run()
	Expect(err).To(BeNil())

	//Creating rbacs
	err = exec.Command("kubectl", "apply", "-f", "../../deploy/rbac.yaml").Run()
	Expect(err).To(BeNil())

	//Creating Chaos-Operator
	By("creating operator")
	err = exec.Command("kubectl", "apply", "-f", "../../deploy/operator.yaml").Run()
	Expect(err).To(BeNil())

	fmt.Println("chaos-operator created successfully")

	//Wait for the creation of chaos-operator
	time.Sleep(50 * time.Second)

	//Check for the status of the chaos-operator
	operator, _ := client.CoreV1().Pods("litmus").List(metav1.ListOptions{LabelSelector: "name=chaos-operator"})
	for _, v := range operator.Items {

		Expect(string(v.Status.Phase)).To(Equal("Running"))
		break
	}
})

//BDD Tests to check secondary resources
var _ = Describe("BDD on chaos-operator", func() {

	// BDD TEST CASE 1
	Context("Check for the custom resources", func() {

		It("Should check for creation of runner pod", func() {

			//creating nginx deployment
			deployment := &appv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nginx",
					Namespace: "litmus",
					Labels: map[string]string{
						"app": "nginx",
					},
					Annotations: map[string]string{
						"litmuschaos.io/chaos": "true",
					},
				},
				Spec: appv1.DeploymentSpec{
					Replicas: func(i int32) *int32 { return &i }(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "nginx",
						},
					},
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "nginx",
							},
						},
						Spec: v1.PodSpec{
							ServiceAccountName: "litmus",
							Containers: []v1.Container{
								{
									Name:  "nginx",
									Image: "nginx:latest",
									Ports: []v1.ContainerPort{
										{

											ContainerPort: 80,
										},
									},
								},
							},
						},
					},
				},
			}

			_, err := client.AppsV1().Deployments("litmus").Create(deployment)
			if err != nil {
				fmt.Println("Deployment is not created and error is ", err)
			}

			//creating chaos-experiment for pod-delete
			By("Creating ChaosExperiments")
			ChaosExperiment := &v1alpha1.ChaosExperiment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-delete",
					Namespace: "litmus",
					Labels: map[string]string{
						"litmuschaos.io/name": "kubernetes",
					},
				},
				Spec: v1alpha1.ChaosExperimentSpec{
					Definition: v1alpha1.ExperimentDef{
						Image: "litmuschaos/ansible-runner:latest",

						Scope: "Namespaced",

						Permissions: []rbacV1.PolicyRule{},

						Args:    []string{"-c", "ansible-playbook ./experiments/generic/pod_delete/pod_delete_ansible_logic.yml -i /etc/ansible/hosts -vv; exit 0"},
						Command: []string{"/bin/bash"},

						ENVList: []v1alpha1.ENVPair{
							{
								Name:  "ANSIBLE_STDOUT_CALLBACK",
								Value: "default",
							},
							{
								Name:  "TOTAL_CHAOS_DURATION",
								Value: "15",
							},
							{
								Name:  "CHAOS_INTERVAL",
								Value: "5",
							},
							{
								Name:  "LIB",
								Value: "",
							},
						},

						Labels: map[string]string{
							"name": "pod-delete",
						},
					},
				},
			}

			_, err = clientSet.ChaosExperiments("litmus").Create(ChaosExperiment)
			Expect(err).To(BeNil())

			fmt.Println("ChaosExperiment created successfully...")

			//Creating chaosEngine
			By("Creating ChaosEngine")
			chaosEngine := &v1alpha1.ChaosEngine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "engine-nginx",
					Namespace: "litmus",
				},
				Spec: v1alpha1.ChaosEngineSpec{
					Appinfo: v1alpha1.ApplicationParams{
						Appns:    "litmus",
						Applabel: "app=nginx",
						AppKind:  "deployment",
					},
					ChaosServiceAccount: "litmus",
					Components: v1alpha1.ComponentParams{
						Runner: v1alpha1.RunnerInfo{
							Image: "litmuschaos/chaos-runner:ci",
							Type:  "go",
						},
					},
					JobCleanUpPolicy: "retain",
					Monitoring:       true,
					EngineState:      "active",
					Experiments: []v1alpha1.ExperimentList{
						{
							Name: "pod-delete",
						},
					},
				},
			}

			_, err = clientSet.ChaosEngines("litmus").Create(chaosEngine)
			Expect(err).To(BeNil())

			fmt.Println("Chaosengine created successfully...")

			//Wait till the creation of runner pod and monitor svc
			time.Sleep(50 * time.Second)

			//Fetching engine-nginx-runner pod
			runner, err := client.CoreV1().Pods("litmus").Get("engine-nginx-runner", metav1.GetOptions{})
			//Fetching engine-nginx-exporter pod
			exporter, err := client.CoreV1().Pods("litmus").Get("engine-nginx-monitor", metav1.GetOptions{})
			//Check for the Availabilty and status of the runner pod
			fmt.Println("name : ", runner.Name)

			Expect(err).To(BeNil())
			Expect(string(runner.Status.Phase)).To(Or(Equal("Running"), Equal("Succeeded")))
			Expect(string(exporter.Status.Phase)).To(Equal("Running"))

			// Check for EngineStatus
			engine, err := clientSet.ChaosEngines("litmus").Get("engine-nginx", metav1.GetOptions{})
			Expect(err).To(BeNil())

			isInit := engine.Status.EngineStatus == v1alpha1.EngineStatusInitialized
			Expect(isInit).To(BeTrue())
		})
	})

	// BDD TEST CASE 2
	Context("check for the custom resources", func() {

		It("Should check for creation of monitor service", func() {
			_, err := client.CoreV1().Services("litmus").Get("engine-nginx-monitor", metav1.GetOptions{})
			Expect(err).To(BeNil())

		})

	})

	Context("Setting the EngineState of ChaosEngine as Stop", func() {

		It("Should delete chaos-resources", func() {

			engine, err := clientSet.ChaosEngines("litmus").Get("engine-nginx", metav1.GetOptions{})
			Expect(err).To(BeNil())

			// setting the EngineState of chaosEngine to stop
			engine.Spec.EngineState = v1alpha1.EngineStateStop

			_, err = clientSet.ChaosEngines("litmus").Update(engine)
			Expect(err).To(BeNil())

			fmt.Println("Chaosengine updated successfully...")

			//Wait till the creation of runner pod and monitor svc
			time.Sleep(50 * time.Second)

		})

	})

	Context("Checking Default ChaosResources", func() {

		It("Should delete chaos-runner pod", func() {

			//Fetching engine-nginx-runner pod
			_, err := client.CoreV1().Pods("litmus").Get("engine-nginx-runner", metav1.GetOptions{})
			fmt.Printf("%v", err)
			isNotFound := errors.IsNotFound(err)
			Expect(isNotFound).To(BeTrue())
			fmt.Println("chaos-runner pod deletion verified")

		})

		It("Should delete chaos-monitor pod ", func() {

			//Fetching engine-nginx-exporter pod
			_, err := client.CoreV1().Pods("litmus").Get("engine-nginx-monitor", metav1.GetOptions{})
			fmt.Printf("%v", err)
			isNotFound := errors.IsNotFound(err)
			Expect(isNotFound).To(BeTrue())
			fmt.Println("chaos-monitor pod deletion verified")

		})

		It("Should delete chaos-monitor service ", func() {

			//Fetching engine-nginx-exporter pod
			_, err := client.CoreV1().Services("litmus").Get("engine-nginx-monitor", metav1.GetOptions{})
			fmt.Printf("%v", err)
			isNotFound := errors.IsNotFound(err)
			Expect(isNotFound).To(BeTrue())
			fmt.Println("chaos-monitor service deletion verified")

		})

		It("Should change the engineStatus ", func() {

			//Fetching engineStatus
			engine, err := clientSet.ChaosEngines("litmus").Get("engine-nginx", metav1.GetOptions{})
			Expect(err).To(BeNil())

			isStopped := engine.Status.EngineStatus == v1alpha1.EngineStatusStopped
			Expect(isStopped).To(BeTrue())
		})
	})

	Context("Deletion of ChaosEngine", func() {

		It("Should delete chaos engine", func() {

			err := clientSet.ChaosEngines("litmus").Delete("engine-nginx", &metav1.DeleteOptions{})
			Expect(err).To(BeNil())

			fmt.Println("chaos engine deleted successfully")

		})

	})

	Context("Creation of ChaosEngine with invalid experiment", func() {

		It("Should create invalid chaos engine", func() {

			//Creating chaosEngine
			By("Creating ChaosEngine")
			chaosEngine := &v1alpha1.ChaosEngine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "engine-nginx",
					Namespace: "litmus",
				},
				Spec: v1alpha1.ChaosEngineSpec{
					Appinfo: v1alpha1.ApplicationParams{
						Appns:    "litmus",
						Applabel: "app=nginx",
						AppKind:  "deployment",
					},
					ChaosServiceAccount: "litmus",
					Components: v1alpha1.ComponentParams{
						Runner: v1alpha1.RunnerInfo{
							Image: "litmuschaos/chaos-runner:ci",
							Type:  "go",
						},
					},
					JobCleanUpPolicy: "delete",
					Monitoring:       true,
					EngineState:      "active",
					Experiments: []v1alpha1.ExperimentList{
						{
							Name: "pod-delete-1",
						},
					},
				},
			}

			_, err := clientSet.ChaosEngines("litmus").Create(chaosEngine)
			Expect(err).To(BeNil())

			time.Sleep(50 * time.Second)

		})
	})

	Context("Check for Chaos Resources for invalid engine", func() {

		It("Should delete chaos-runner pod", func() {

			//Fetching engine-nginx-runner pod
			_, err := client.CoreV1().Pods("litmus").Get("engine-nginx-runner", metav1.GetOptions{})
			fmt.Printf("%v", err)
			isNotFound := errors.IsNotFound(err)
			Expect(isNotFound).To(BeTrue())
			fmt.Println("chaos-runner pod deletion verified")

		})

		It("Should delete chaos-monitor pod ", func() {

			//Fetching engine-nginx-exporter pod
			_, err := client.CoreV1().Pods("litmus").Get("engine-nginx-monitor", metav1.GetOptions{})
			fmt.Printf("%v", err)
			isNotFound := errors.IsNotFound(err)
			Expect(isNotFound).To(BeTrue())
			fmt.Println("chaos-monitor pod deletion verified")

		})

		It("Should delete chaos-monitor service ", func() {

			//Fetching engine-nginx-exporter pod
			_, err := client.CoreV1().Services("litmus").Get("engine-nginx-monitor", metav1.GetOptions{})
			fmt.Printf("%v\n", err)
			isNotFound := errors.IsNotFound(err)
			Expect(isNotFound).To(BeTrue())
			fmt.Println("chaos-monitor service deletion verified")

		})

		It("Should change EngineStatus ", func() {

			//Fetching engineStatus
			engine, err := clientSet.ChaosEngines("litmus").Get("engine-nginx", metav1.GetOptions{})
			Expect(err).To(BeNil())

			isComplete := engine.Status.EngineStatus == v1alpha1.EngineStatusCompleted
			Expect(isComplete).To(BeTrue())

		})
	})

})

//Deleting all unused resources
var _ = AfterSuite(func() {

	By("Deleting all CRDs")
	crdDeletion := exec.Command("kubectl", "delete", "-f", "../../deploy/chaos_crds.yaml").Run()
	Expect(crdDeletion).To(BeNil())
})
