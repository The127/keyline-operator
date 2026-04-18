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

var _ = Describe("KeylineRoleAssignment", Ordered, func() {
	const (
		testNamespace = "keyline-e2e-ra"
		instanceName  = "test-keyline-ra"
		vsName        = "test-vs-ra"
		projName      = "test-proj-ra"
		roleName      = "test-role-ra"
		userName      = "test-user-ra"
		keylineVS     = "keyline"
	)

	waitReady := func(g Gomega, kind, name string) {
		cmd := exec.Command("kubectl", "get",
			fmt.Sprintf("%s/%s", kind, name),
			"-n", testNamespace,
			"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`,
		)
		output, err := utils.Run(cmd)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(output).To(Equal("True"), "%s/%s Ready condition is not True yet", kind, name)
	}

	BeforeAll(func() {
		By("creating the e2e test namespace")
		cmd := exec.Command("kubectl", "create", "ns", testNamespace)
		_, _ = utils.Run(cmd)

		SetDefaultEventuallyTimeout(5 * time.Minute)
		SetDefaultEventuallyPollingInterval(5 * time.Second)

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

		By("waiting for the KeylineInstance to become Ready")
		Eventually(func(g Gomega) { waitReady(g, "keylineinstance", instanceName) }).Should(Succeed())

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
		Eventually(func(g Gomega) { waitReady(g, "keylinevirtualserver", vsName) }).Should(Succeed())

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
  name: test-project-ra
  slug: test-project-ra
`, projName, testNamespace, vsName)

		cmd = exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(projManifest)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineProject")

		By("waiting for the KeylineProject to become Ready")
		Eventually(func(g Gomega) { waitReady(g, "keylineproject", projName) }).Should(Succeed())

		By("creating the KeylineRole")
		roleManifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineRole
metadata:
  name: %s
  namespace: %s
spec:
  projectRef:
    name: %s
  name: e2e-test-role
`, roleName, testNamespace, projName)

		cmd = exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(roleManifest)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineRole")

		By("waiting for the KeylineRole to become Ready")
		Eventually(func(g Gomega) { waitReady(g, "keylinerole", roleName) }).Should(Succeed())

		By("creating the KeylineUser")
		userManifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineUser
metadata:
  name: %s
  namespace: %s
spec:
  virtualServerRef:
    name: %s
  username: svc-e2e-ra
`, userName, testNamespace, vsName)

		cmd = exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(userManifest)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineUser")

		By("waiting for the KeylineUser to become Ready")
		Eventually(func(g Gomega) { waitReady(g, "keylineuser", userName) }).Should(Succeed())
	})

	AfterAll(func() {
		By("removing the e2e test namespace")
		cmd := exec.Command("kubectl", "delete", "ns", testNamespace, "--ignore-not-found=true")
		_, _ = utils.Run(cmd)
	})

	It("should become Ready=True after assigning user to role", func() {
		const assignmentName = "test-ra-create"

		By("creating the KeylineRoleAssignment")
		manifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineRoleAssignment
metadata:
  name: %s
  namespace: %s
spec:
  roleRef:
    name: %s
  userRef:
    name: %s
`, assignmentName, testNamespace, roleName, userName)

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineRoleAssignment")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylineroleassignment", assignmentName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for Ready=True")
		Eventually(func(g Gomega) { waitReady(g, "keylineroleassignment", assignmentName) }).Should(Succeed())
	})

	It("should remain Ready=True on re-reconcile (idempotent assignment)", func() {
		const assignmentName = "test-ra-idempotent"

		By("creating the KeylineRoleAssignment")
		manifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineRoleAssignment
metadata:
  name: %s
  namespace: %s
spec:
  roleRef:
    name: %s
  userRef:
    name: %s
`, assignmentName, testNamespace, roleName, userName)

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineRoleAssignment")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylineroleassignment", assignmentName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for Ready=True")
		Eventually(func(g Gomega) { waitReady(g, "keylineroleassignment", assignmentName) }).Should(Succeed())

		By("annotating to trigger a re-reconcile")
		cmd = exec.Command("kubectl", "annotate", "keylineroleassignment", assignmentName,
			"-n", testNamespace, "keyline.keyline.dev/force-reconcile=true", "--overwrite")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		By("verifying Ready=True is stable")
		Consistently(func(g Gomega) { waitReady(g, "keylineroleassignment", assignmentName) },
			30*time.Second, 5*time.Second).Should(Succeed())
	})

	It("should set Ready=False with reason RoleNotFound when role does not exist", func() {
		const assignmentName = "test-ra-no-role"

		By("creating the KeylineRoleAssignment referencing a nonexistent role")
		manifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineRoleAssignment
metadata:
  name: %s
  namespace: %s
spec:
  roleRef:
    name: nonexistent-role
  userRef:
    name: %s
`, assignmentName, testNamespace, userName)

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineRoleAssignment")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylineroleassignment", assignmentName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for Ready=False with reason RoleNotFound")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylineroleassignment/%s", assignmentName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].reason}`,
			)
			reason, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(reason).To(Equal("RoleNotFound"))
		}).Should(Succeed(), "KeylineRoleAssignment did not set RoleNotFound condition")
	})

	It("should set Ready=False with reason UserNotFound when user does not exist", func() {
		const assignmentName = "test-ra-no-user"

		By("creating the KeylineRoleAssignment referencing a nonexistent user")
		manifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineRoleAssignment
metadata:
  name: %s
  namespace: %s
spec:
  roleRef:
    name: %s
  userRef:
    name: nonexistent-user
`, assignmentName, testNamespace, roleName)

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineRoleAssignment")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylineroleassignment", assignmentName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for Ready=False with reason UserNotFound")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylineroleassignment/%s", assignmentName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].reason}`,
			)
			reason, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(reason).To(Equal("UserNotFound"))
		}).Should(Succeed(), "KeylineRoleAssignment did not set UserNotFound condition")
	})
})
