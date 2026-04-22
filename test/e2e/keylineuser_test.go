// Copyright 2026. Licensed under the Apache License, Version 2.0.

package e2e

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/keyline/keyline-operator/test/utils"
)

// generatePublicKeyPEM returns a fresh Ed25519 PEM-encoded public key. The
// private half is discarded — the operator only ever transmits public keys, so
// the e2e tests never need to actually use them for signing.
func generatePublicKeyPEM() string {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	der, err := x509.MarshalPKIXPublicKey(pub)
	Expect(err).NotTo(HaveOccurred())
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// indentPEM prefixes every non-empty line of s with six spaces, matching the
// column required to embed a PEM block inside the YAML literal scalars used
// in the KeylineUser manifests below.
func indentPEM(s string) string {
	const prefix = "      "
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

var _ = Describe("KeylineUser", Ordered, func() {
	const (
		testNamespace = "keyline-e2e-user"
		instanceName  = "test-keyline-user"
		vsName        = "test-vs-user"
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
  image: ghcr.io/the127/keyline:v0.5.1
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

	It("should become Ready=True and store the userId", func() {
		const userName = "test-user-create"

		By("creating the KeylineUser")
		manifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineUser
metadata:
  name: %s
  namespace: %s
spec:
  virtualServerRef:
    name: %s
  username: svc-test-create
`, userName, testNamespace, vsName)

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineUser")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylineuser", userName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for Ready=True")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylineuser/%s", userName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("True"), "KeylineUser Ready condition is not True yet")
		}).Should(Succeed(), "KeylineUser did not become Ready")

		By("verifying userId is set in status")
		cmd = exec.Command("kubectl", "get",
			fmt.Sprintf("keylineuser/%s", userName),
			"-n", testNamespace,
			"-o", "jsonpath={.status.userId}",
		)
		userId, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(userId).NotTo(BeEmpty(), "status.userId should be set after sync")
	})

	It("should become Ready=True when a displayName is set", func() {
		const userName = "test-user-displayname"

		By("creating the KeylineUser with a displayName")
		manifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineUser
metadata:
  name: %s
  namespace: %s
spec:
  virtualServerRef:
    name: %s
  username: svc-test-displayname
  displayName: "Test Service User"
`, userName, testNamespace, vsName)

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineUser")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylineuser", userName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for Ready=True")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylineuser/%s", userName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("True"), "KeylineUser Ready condition is not True yet")
		}).Should(Succeed(), "KeylineUser with displayName did not become Ready")
	})

	It("should associate public keys and record them in status.managedKeyIds", func() {
		const userName = "test-user-keys-add"
		pemA := generatePublicKeyPEM()
		pemB := generatePublicKeyPEM()

		By("creating the KeylineUser with two public keys")
		manifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineUser
metadata:
  name: %s
  namespace: %s
spec:
  virtualServerRef:
    name: %s
  username: svc-keys-add
  publicKeys:
  - kid: e2e-key-a
    publicKeyPEM: |
%s
  - kid: e2e-key-b
    publicKeyPEM: |
%s
`, userName, testNamespace, vsName, indentPEM(pemA), indentPEM(pemB))

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineUser with publicKeys")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylineuser", userName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for Ready=True with managedKeyIds populated")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylineuser/%s", userName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}|{.status.managedKeyIds}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			ready, managed, ok := strings.Cut(output, "|")
			g.Expect(ok).To(BeTrue(), "expected pipe-separated ready|managed output")
			g.Expect(ready).To(Equal("True"))
			g.Expect(managed).To(ContainSubstring("e2e-key-a"))
			g.Expect(managed).To(ContainSubstring("e2e-key-b"))
		}).Should(Succeed(), "KeylineUser did not reconcile public keys")
	})

	It("should remove kids dropped from spec and preserve the rest", func() {
		const userName = "test-user-keys-remove"
		pemA := generatePublicKeyPEM()
		pemB := generatePublicKeyPEM()

		By("creating the KeylineUser with two public keys")
		twoKeys := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineUser
metadata:
  name: %s
  namespace: %s
spec:
  virtualServerRef:
    name: %s
  username: svc-keys-remove
  publicKeys:
  - kid: keep-me
    publicKeyPEM: |
%s
  - kid: drop-me
    publicKeyPEM: |
%s
`, userName, testNamespace, vsName, indentPEM(pemA), indentPEM(pemB))

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(twoKeys)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineUser")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylineuser", userName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for both kids to be managed")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylineuser/%s", userName),
				"-n", testNamespace,
				"-o", "jsonpath={.status.managedKeyIds}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(ContainSubstring("keep-me"))
			g.Expect(output).To(ContainSubstring("drop-me"))
		}).Should(Succeed())

		By("patching the spec to drop one kid")
		oneKey := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineUser
metadata:
  name: %s
  namespace: %s
spec:
  virtualServerRef:
    name: %s
  username: svc-keys-remove
  publicKeys:
  - kid: keep-me
    publicKeyPEM: |
%s
`, userName, testNamespace, vsName, indentPEM(pemA))

		cmd = exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(oneKey)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for dropped kid to disappear from managedKeyIds")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylineuser/%s", userName),
				"-n", testNamespace,
				"-o", "jsonpath={.status.managedKeyIds}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(ContainSubstring("keep-me"))
			g.Expect(output).NotTo(ContainSubstring("drop-me"))
		}).Should(Succeed(), "dropped kid was not removed from managedKeyIds")
	})

	It("should set Ready=False with reason VirtualServerNotFound when VS does not exist", func() {
		const userName = "test-user-no-vs"

		By("creating the KeylineUser referencing a nonexistent virtual server")
		manifest := fmt.Sprintf(`
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineUser
metadata:
  name: %s
  namespace: %s
spec:
  virtualServerRef:
    name: nonexistent-vs
  username: svc-test-no-vs
`, userName, testNamespace)

		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create KeylineUser")

		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "keylineuser", userName,
				"-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		By("waiting for Ready=False with reason VirtualServerNotFound")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get",
				fmt.Sprintf("keylineuser/%s", userName),
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].reason}`,
			)
			reason, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(reason).To(Equal("VirtualServerNotFound"))
		}).Should(Succeed(), "KeylineUser did not set VirtualServerNotFound condition")
	})
})
