package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	oteg "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"
	configv1 "github.com/openshift/api/config/v1"
	framework "github.com/openshift/cluster-capi-operator/e2e/framework"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("[OTP][Jira:OCPCLOUD][OCPFeatureGate:ClusterAPIInstall][sig-cluster-lifecycle] Cluster_Infrastructure CAPI Machines", Ordered, func() {
	BeforeAll(func() {
		switch platform {
		case configv1.AWSPlatformType, configv1.GCPPlatformType:
		default:
			Skip("CAPI machine webhook tests only supported on AWS and GCP")
		}
		skipUnlessCAPIDeployed()
	})

	It("should enforce webhook validations for CAPI cluster resources", Label("Disruptive"), oteg.Informing(), func() {
		By("Getting the CAPI Cluster object")
		clusters := &clusterv1.ClusterList{}
		Expect(cl.List(ctx, clusters, client.InNamespace(framework.CAPINamespace))).To(Succeed())
		Expect(clusters.Items).NotTo(BeEmpty(), "expected at least one Cluster in %s", framework.CAPINamespace)

		cluster := &clusters.Items[0]

		By("Attempting to patch cluster with invalid infrastructureRef kind")
		patch := client.MergeFrom(cluster.DeepCopy())
		cluster.Spec.InfrastructureRef.Kind = "invalid"
		err := cl.Patch(ctx, cluster, patch)
		Expect(err).To(HaveOccurred(), "patching with invalid kind should be rejected")
		Expect(err.Error()).To(ContainSubstring("invalid"), "error should mention the invalid kind")

		By("Attempting to delete the cluster")
		freshCluster := &clusterv1.Cluster{}
		Expect(cl.Get(ctx, client.ObjectKeyFromObject(cluster), freshCluster)).To(Succeed())
		err = cl.Delete(ctx, freshCluster)
		Expect(err).To(HaveOccurred(), "cluster deletion should be denied")
		Expect(err.Error()).Should(MatchRegexp(`(?i)(denied|not allowed)`), "error should indicate deletion was denied")
	})
})
