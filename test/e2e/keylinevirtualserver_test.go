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

var _ = Describe("KeylineVirtualServer", Ordered, func() {
	const (
		testNamespace = "keyline-e2e-vs"
		instanceName  = "test-keyline-vs"
		// keylineVS is the initial virtual server name created by the instance bootstrap.
		keylineVS = "keyline"
	)

	BeforeAll(func() {
		By("creating the e2e test namespace")
		cmd := exec.Command("kubectl", "create", "ns", testNamespace)
		_, _ = utils.Run(cmd)

		By("creating the KeylineInstance")
		manifest := fmt.Sprintf(`
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
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineInstance")

		By("waiting for the KeylineInstance to become Ready")
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
		}).Should(Succeed(), "KeylineInstance did not become Ready in time")
	})

	AfterAll(func() {
		By("removing the e2e test namespace")
		cmd := exec.Command("kubectl", "delete", "ns", testNamespace, "--ignore-not-found=true")
		_, _ = utils.Run(cmd)
	})

	It("should become Ready=True when synced against a ready instance", func() {
		const vsName = "test-vs-sync"

		By("creating the KeylineVirtualServer")
		manifest := fmt.Sprintf(`
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

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineVirtualServer")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylinevirtualserver", vsName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for Ready=True")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylinevirtualserver/%s", vsName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("True"), "KeylineVirtualServer Ready condition is not True yet")
		}).Should(Succeed(), "KeylineVirtualServer did not become Ready")
	})

	It("should become Ready=True after patching virtual server fields", func() {
		const vsName = "test-vs-patch"

		By("creating the KeylineVirtualServer with a displayName")
		manifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineVirtualServer
metadata:
  name: %s
  namespace: %s
spec:
  instanceRef:
    name: %s
  name: %s
  displayName: "Test Virtual Server"
  registrationEnabled: false
`, vsName, testNamespace, instanceName, keylineVS)

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineVirtualServer")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylinevirtualserver", vsName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for Ready=True after the patch")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylinevirtualserver/%s", vsName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("True"), "KeylineVirtualServer Ready condition is not True yet")
		}).Should(Succeed(), "KeylineVirtualServer with displayName did not become Ready")
	})

	It("should set Ready=False with reason InstanceNotFound when the instance does not exist", func() {
		const vsName = "test-vs-no-instance"

		By("creating the KeylineVirtualServer referencing a nonexistent instance")
		manifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineVirtualServer
metadata:
  name: %s
  namespace: %s
spec:
  instanceRef:
    name: nonexistent-instance
  name: %s
`, vsName, testNamespace, keylineVS)

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineVirtualServer")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylinevirtualserver", vsName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for Ready=False with reason InstanceNotFound")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylinevirtualserver/%s", vsName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`,
			)
			status, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(status).To(Equal("False"))

			cmd = exec.Command("kubectl", "get",
				fmt.Sprintf("keylinevirtualserver/%s", vsName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].reason}`,
			)
			reason, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(reason).To(Equal("InstanceNotFound"))
		}).Should(Succeed(), "KeylineVirtualServer did not set InstanceNotFound condition")
	})
})
