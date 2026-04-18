// Copyright 2026. Licensed under the Apache License, Version 2.0.

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

var _ = Describe("KeylineProject", Ordered, func() {
	const (
		testNamespace = "keyline-e2e-proj"
		instanceName  = "test-keyline-proj"
		vsName        = "test-vs-proj"
		keylineVS     = "keyline"
	)

	BeforeAll(func() {
		By("creating the e2e test namespace")
		cmd := exec.Command("kubectl", "create", "ns", testNamespace)
		_, _ = utils.Run(cmd)

		By("creating the KeylineInstance")
		instanceManifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineInstance
metadata:
  name: %s
  namespace: %s
spec:
  image: ghcr.io/the127/keyline:v0.3.9
  externalUrl: http://keyline.test
  frontendExternalUrl: http://frontend.test
  virtualServer: keyline
  database:
    mode: memory
  keyStore:
    mode: directory
`, instanceName, testNamespace)

		cmd = exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(instanceManifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineInstance")

		SetDefaultEventuallyTimeout(5 * time.Minute)
		SetDefaultEventuallyPollingInterval(5 * time.Second)

		By("waiting for the KeylineInstance to become Ready")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylineinstance/%s", instanceName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("True"), "KeylineInstance Ready condition is not True yet")
		}).Should(Succeed(), "KeylineInstance did not become Ready in time")

		By("creating the KeylineVirtualServer")
		vsManifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineVirtualServer
metadata:
  name: %s
  namespace: %s
spec:
  instanceRef:
    name: %s
  name: %s
`, vsName, testNamespace, instanceName, keylineVS)

		cmd = exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(vsManifest)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineVirtualServer")

		By("waiting for the KeylineVirtualServer to become Ready")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylinevirtualserver/%s", vsName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("True"), "KeylineVirtualServer Ready condition is not True yet")
		}).Should(Succeed(), "KeylineVirtualServer did not become Ready in time")
	})

	AfterAll(func() {
		By("removing the e2e test namespace")
		cmd := exec.Command("kubectl", "delete", "ns", testNamespace, "--ignore-not-found=true")
		_, _ = utils.Run(cmd)
	})

	It("should become Ready=True when the project is created in Keyline", func() {
		const projName = "test-proj-create"

		By("creating the KeylineProject")
		manifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineProject
metadata:
  name: %s
  namespace: %s
spec:
  virtualServerRef:
    name: %s
  slug: test-project
  name: Test Project
`, projName, testNamespace, vsName)

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineProject")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylineproject", projName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for Ready=True")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylineproject/%s", projName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("True"), "KeylineProject Ready condition is not True yet")
		}).Should(Succeed(), "KeylineProject did not become Ready")
	})

	It("should set Ready=False with reason VirtualServerNotFound when VS does not exist", func() {
		const projName = "test-proj-no-vs"

		By("creating the KeylineProject referencing a nonexistent virtual server")
		manifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineProject
metadata:
  name: %s
  namespace: %s
spec:
  virtualServerRef:
    name: nonexistent-vs
  slug: test-project-no-vs
  name: Test Project No VS
`, projName, testNamespace)

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineProject")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylineproject", projName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for Ready=False with reason VirtualServerNotFound")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylineproject/%s", projName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].reason}`,
			)
			reason, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(reason).To(Equal("VirtualServerNotFound"))
		}).Should(Succeed(), "KeylineProject did not set VirtualServerNotFound condition")
	})
})
