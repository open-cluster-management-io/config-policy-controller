// Copyright (c) 2020 Red Hat, Inc.

package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/open-cluster-management/config-policy-controller/test/utils"
)

const etcdEncryptionEnforceName string = "etcd-encryption-enforce"
const etcdEncryptionInformName string = "etcd-encryption-inform"
const etcdEncryptionEnforceYaml string = "../resources/case11_apiserver_config/etcd_encryption_enforce.yaml"
const etcdEncryptionEnforceInvalidYaml string = "../resources/case11_apiserver_config/etcd_encryption_enforce_invalid.yaml"
const etcdEncryptionInformYaml string = "../resources/case11_apiserver_config/etcd_encryption_inform.yaml"
const tlsProfileEnforceName string = "tls-profile-enforce"
const tlsProfileInformName string = "tls-profile-inform"
const tlsProfileEnforceYaml string = "../resources/case11_apiserver_config/tls_profile_enforce.yaml"
const tlsProfileEnforceInvalidYaml string = "../resources/case11_apiserver_config/tls_profile_enforce_invalid.yaml"
const tlsProfileInformYaml string = "../resources/case11_apiserver_config/tls_profile_inform.yaml"

var _ = Describe("Test APIServer Config policy", func() {
	Describe("Test etcd encryption and tls profile", func() {
		It("should be noncompliant for no encryption", func() {
			By("Creating " + etcdEncryptionInformYaml + " on managed")
			utils.Kubectl("apply", "-f", etcdEncryptionInformYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, etcdEncryptionInformName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				informPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, etcdEncryptionInformName, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(informPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
		It("should be noncompliant for invalid encryption", func() {
			By("Creating " + etcdEncryptionEnforceInvalidYaml + " on managed")
			utils.Kubectl("apply", "-f", etcdEncryptionEnforceInvalidYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, etcdEncryptionEnforceName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, etcdEncryptionEnforceName, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
		It("should be compliant for aescbc encryption", func() {
			By("Creating " + etcdEncryptionEnforceYaml + " on managed")
			utils.Kubectl("apply", "-f", etcdEncryptionEnforceYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, etcdEncryptionEnforceName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, etcdEncryptionEnforceName, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				informPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, etcdEncryptionInformName, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(informPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should be noncompliant for no tls profile", func() {
			By("Creating " + tlsProfileInformYaml + " on managed")
			utils.Kubectl("apply", "-f", tlsProfileInformYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, tlsProfileInformName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				informPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, tlsProfileInformName, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(informPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
		It("should be noncompliant for invalid tls profile", func() {
			By("Creating " + tlsProfileEnforceInvalidYaml + " on managed")
			utils.Kubectl("apply", "-f", tlsProfileEnforceInvalidYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, tlsProfileEnforceName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, tlsProfileEnforceName, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
		It("should be compliant for intermediate tls profile", func() {
			By("Creating " + tlsProfileEnforceYaml + " on managed")
			utils.Kubectl("apply", "-f", tlsProfileEnforceYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, tlsProfileEnforceName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, tlsProfileEnforceName, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				informPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, tlsProfileInformName, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(informPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				informPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, etcdEncryptionInformName, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(informPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
	})
})
