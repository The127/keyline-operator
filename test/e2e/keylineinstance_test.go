package e2e

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/keyline/keyline-operator/test/utils"
)

var _ = Describe("KeylineInstance", Ordered, func() {
	const (
		testNamespace = "keyline-e2e"
		instanceName  = "test-keyline"
	)

	BeforeAll(func() {
		By("installing CRDs")
		cmd := exec.Command("make", "install")
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the operator")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd) //nolint:ineffassign
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy operator")

		By("creating the e2e test namespace")
		cmd = exec.Command("kubectl", "create", "ns", testNamespace)
		_, _ = utils.Run(cmd)
	})

	AfterAll(func() {
		By("removing the e2e test namespace")
		cmd := exec.Command("kubectl", "delete", "ns", testNamespace, "--ignore-not-found=true")
		_, _ = utils.Run(cmd)

		By("undeploying the operator")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)
	})

	It("should deploy Keyline and reach Ready=True", func() {
		By("creating the KeylineInstance")
		manifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineInstance
metadata:
  name: %s
  namespace: %s
spec:
  image: ghcr.io/the127/keyline:v0.3.2
  externalUrl: http://keyline.test
  frontendExternalUrl: http://frontend.test
  virtualServer: keyline
  database:
    mode: memory
  keyStore:
    mode: directory
`, instanceName, testNamespace)

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineInstance")

		By("waiting for the Keyline Deployment to become available")
		cmd = exec.Command("kubectl", "wait",
			fmt.Sprintf("deployment/%s", instanceName),
			"-n", testNamespace,
			"--for=condition=Available",
			"--timeout=5m",
		)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Keyline Deployment did not become available")

		By("waiting for the KeylineInstance Ready condition to be True")
		SetDefaultEventuallyTimeout(5 * time.Minute)
		SetDefaultEventuallyPollingInterval(5 * time.Second)

		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylineinstance/%s", instanceName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("True"), "KeylineInstance Ready condition is not True yet")
		}).Should(Succeed())

		By("verifying the operator credentials Secret was created")
		cmd = exec.Command("kubectl", "get", "secret",
			fmt.Sprintf("%s-operator-credentials", instanceName),
			"-n", testNamespace,
		)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Operator credentials Secret not found")

		By("verifying status.url is set")
		cmd = exec.Command("kubectl", "get",
			fmt.Sprintf("keylineinstance/%s", instanceName),
			"-n", testNamespace,
			"-o", "jsonpath={.status.url}",
		)
		url, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(url).To(ContainSubstring(instanceName), "status.url not set correctly")
	})
})
