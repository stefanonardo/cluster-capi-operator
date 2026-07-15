package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	oteg "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"
	configv1 "github.com/openshift/api/config/v1"
	framework "github.com/openshift/cluster-capi-operator/e2e/framework"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func skipUnlessCAPIDeployed() {
	deploys := &appsv1.DeploymentList{}
	Expect(cl.List(ctx, deploys, client.InNamespace(framework.CAPINamespace))).To(Succeed())
	available := 0
	for i := range deploys.Items {
		if deploys.Items[i].Status.AvailableReplicas > 0 {
			available++
		}
	}
	if available == 0 {
		Skip("CAPI not deployed")
	}
}

var _ = Describe("[OTP][Jira:OCPCLOUD][OCPFeatureGate:ClusterAPIInstall][sig-cluster-lifecycle] Cluster_Infrastructure CAPI", Ordered, func() {
	BeforeAll(func() {
		skipUnlessCAPIDeployed()
	})

	It("should have workload management annotations on all deployments", oteg.Informing(), func() {
		By("Listing deployments in the CAPI namespace")
		deploys := &appsv1.DeploymentList{}
		Expect(cl.List(ctx, deploys, client.InNamespace(framework.CAPINamespace))).To(Succeed())
		Expect(deploys.Items).NotTo(BeEmpty(), "expected at least one deployment in %s", framework.CAPINamespace)

		By("Checking workload annotation on each deployment")
		for _, deploy := range deploys.Items {
			annotations := deploy.Spec.Template.Annotations
			Expect(annotations).To(HaveKeyWithValue(
				"target.workload.openshift.io/management",
				`{"effect": "PreferredDuringScheduling"}`,
			), "deployment %s is missing the workload management annotation", deploy.Name)
		}
	})

	It("should have IPAM CRDs installed", oteg.Informing(), func() {
		ipamCRDs := []string{
			"ipaddressclaims.ipam.cluster.x-k8s.io",
			"ipaddresses.ipam.cluster.x-k8s.io",
		}

		for _, crdName := range ipamCRDs {
			By(fmt.Sprintf("Checking CRD %s exists", crdName))
			crd := &apiextensionsv1.CustomResourceDefinition{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: crdName}, crd)).To(Succeed(),
				"CRD %s should exist", crdName)
		}
	})

	It("should have FallbackToLogsOnError as terminationMessagePolicy", oteg.Informing(), func() {
		By("Listing pods in the CAPI namespace")
		pods := &corev1.PodList{}
		Expect(cl.List(ctx, pods, client.InNamespace(framework.CAPINamespace))).To(Succeed())
		Expect(pods.Items).NotTo(BeEmpty(), "expected at least one pod in %s", framework.CAPINamespace)

		By("Checking terminationMessagePolicy on each pod's containers")
		for _, pod := range pods.Items {
			for _, container := range pod.Spec.Containers {
				Expect(string(container.TerminationMessagePolicy)).To(
					Equal(string(corev1.TerminationMessageFallbackToLogsOnError)),
					"pod %s container %s should have FallbackToLogsOnError", pod.Name, container.Name)
			}
		}
	})

	It("should re-sync worker-user-data secret after deletion", Label("Disruptive"), oteg.Informing(), func() {
		switch platform {
		case configv1.AWSPlatformType, configv1.GCPPlatformType, configv1.VSpherePlatformType:
		default:
			Skip(fmt.Sprintf("Secret sync test not supported on %s", platform))
		}

		secretKey := client.ObjectKey{
			Namespace: framework.CAPINamespace,
			Name:      "worker-user-data",
		}

		By("Verifying worker-user-data secret exists")
		secret := &corev1.Secret{}
		Expect(cl.Get(ctx, secretKey, secret)).To(Succeed())

		By("Deleting worker-user-data secret")
		Expect(cl.Delete(ctx, secret)).To(Succeed())

		By("Waiting for secret to be re-synced")
		Eventually(func() error {
			return cl.Get(ctx, secretKey, &corev1.Secret{})
		}, framework.WaitMedium, framework.RetryMedium).Should(Succeed(),
			"worker-user-data secret should be re-synced from %s", framework.MAPINamespace)
	})

	It("should deny deletion of infrastructure cluster resources", Label("Disruptive"), oteg.Informing(), func() {
		switch platform {
		case configv1.AWSPlatformType, configv1.GCPPlatformType, configv1.VSpherePlatformType:
		default:
			Skip(fmt.Sprintf("Infra cluster deletion test not supported on %s", platform))
		}

		infraClusterKind := platformToInfraClusterKind(platform)
		if infraClusterKind == "" {
			Skip(fmt.Sprintf("Unknown infra cluster kind for platform %s", platform))
		}

		By(fmt.Sprintf("Listing %s resources in CAPI namespace", infraClusterKind))
		infraList := &unstructured.UnstructuredList{}
		infraList.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "infrastructure.cluster.x-k8s.io",
			Version: "v1beta2",
			Kind:    infraClusterKind + "List",
		})
		Expect(cl.List(ctx, infraList, client.InNamespace(framework.CAPINamespace))).To(Succeed())

		var target *unstructured.Unstructured
		for i := range infraList.Items {
			if infraList.Items[i].GetName() == clusterName {
				target = &infraList.Items[i]
				break
			}
		}

		if target == nil {
			Skip("Infrastructure cluster resource not found matching cluster name")
		}

		By(fmt.Sprintf("Attempting to delete %s/%s", infraClusterKind, target.GetName()))
		err := cl.Delete(ctx, target)
		Expect(err).To(HaveOccurred(), "deletion should be denied")
		Expect(err.Error()).To(ContainSubstring("denied"), "error should mention denial")
	})
})

func platformToInfraClusterKind(p configv1.PlatformType) string {
	switch p {
	case configv1.AWSPlatformType:
		return "AWSCluster"
	case configv1.GCPPlatformType:
		return "GCPCluster"
	case configv1.VSpherePlatformType:
		return "VSphereCluster"
	case configv1.AzurePlatformType:
		return "AzureCluster"
	default:
		return ""
	}
}
