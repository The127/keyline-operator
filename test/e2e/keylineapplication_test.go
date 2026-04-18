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

var _ = Describe("KeylineApplication", Ordered, func() {
	const (
		testNamespace = "keyline-e2e-app"
		instanceName  = "test-keyline-app"
		vsName        = "test-vs-app"
		projName      = "test-proj-app"
		keylineVS     = "keyline"
		projectSlug   = "test-app-project"
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

		By("creating the KeylineProject")
		projManifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineProject
metadata:
  name: %s
  namespace: %s
spec:
  virtualServerRef:
    name: %s
  slug: %s
  name: Test App Project
`, projName, testNamespace, vsName, projectSlug)

		cmd = exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(projManifest)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineProject")

		By("waiting for the KeylineProject to become Ready")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylineproject/%s", projName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("True"), "KeylineProject Ready condition is not True yet")
		}).Should(Succeed(), "KeylineProject did not become Ready in time")
	})

	AfterAll(func() {
		By("removing the e2e test namespace")
		cmd := exec.Command("kubectl", "delete", "ns", testNamespace, "--ignore-not-found=true")
		_, _ = utils.Run(cmd)
	})

	It("should become Ready=True and store the applicationId", func() {
		const appName = "test-app-create"

		By("creating the KeylineApplication")
		manifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineApplication
metadata:
  name: %s
  namespace: %s
spec:
  projectRef:
    name: %s
  name: test-application
  displayName: Test Application
  type: public
  redirectUris:
    - http://localhost:8080/callback
`, appName, testNamespace, projName)

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineApplication")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylineapplication", appName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for Ready=True")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylineapplication/%s", appName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("True"), "KeylineApplication Ready condition is not True yet")
		}).Should(Succeed(), "KeylineApplication did not become Ready")

		By("verifying applicationId is set in status")
		cmd = exec.Command("kubectl", "get",
			fmt.Sprintf("keylineapplication/%s", appName),
			"-n", testNamespace,
			"-o", "jsonpath={.status.applicationId}",
		)
		appId, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(appId).NotTo(BeEmpty(), "status.applicationId should be set after sync")
	})

	It("should set Ready=False with reason ProjectNotFound when project does not exist", func() {
		const appName = "test-app-no-proj"

		By("creating the KeylineApplication referencing a nonexistent project")
		manifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineApplication
metadata:
  name: %s
  namespace: %s
spec:
  projectRef:
    name: nonexistent-project
  name: test-application-no-proj
  displayName: Test Application No Project
  type: public
  redirectUris:
    - http://localhost:8080/callback
`, appName, testNamespace)

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineApplication")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylineapplication", appName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for Ready=False with reason ProjectNotFound")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylineapplication/%s", appName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].reason}`,
			)
			reason, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(reason).To(Equal("ProjectNotFound"))
		}).Should(Succeed(), "KeylineApplication did not set ProjectNotFound condition")
	})
})
