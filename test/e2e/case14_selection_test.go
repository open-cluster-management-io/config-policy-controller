// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case14PolicyNamed       string = "../resources/case14_namespaces/case14_limitrange_named.yaml"
	case14PolicyUnnamed     string = "../resources/case14_namespaces/case14_limitrange_unnamed.yaml"
	case14PolicyNamedName   string = "policy-named-limitrange"
	case14PolicyUnnamedName string = "policy-unnamed-limitrange"
	case14LimitRangeFile    string = "../resources/case14_namespaces/case14_limitrange.yaml"
	case14LimitRangeName    string = "container-mem-limit-range"
)

var _ = Describe("Test policy compliance with namespace selection", func() {
	Describe("Create a named limitrange policy on managed cluster in ns: "+testNamespace, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case14PolicyNamed + " on managed")
			utils.Kubectl("apply", "-f", case14PolicyNamed, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case14PolicyNamedName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case14PolicyNamedName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
		It("should stay noncompliant when limitrange is in one matching namespace", func() {
			By("Creating limitrange " + case14LimitRangeName + " on range1")
			utils.Kubectl("apply", "-f", case14LimitRangeFile, "-n", "range1")
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case14PolicyNamedName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case14PolicyNamedName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case14PolicyNamedName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, time.Second*20, 1).Should(Equal("NonCompliant"))
		})
		It("should be compliant with limitrange in all matching namespaces", func() {
			By("Creating " + case14LimitRangeName + " on range2")
			utils.Kubectl("apply", "-f", case14LimitRangeFile, "-n", "range2")
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case14PolicyNamedName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case14PolicyNamedName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("Clean up named policy and limitrange", func() {
			By("Deleting " + case14LimitRangeName + " on range1")
			utils.Kubectl("delete", "-f", case14LimitRangeFile, "-n", "range1")
			By("Deleting " + case14LimitRangeName + " on range2")
			utils.Kubectl("delete", "-f", case14LimitRangeFile, "-n", "range2")
			By("Deleting " + case14PolicyNamed + " on managed")
			utils.Kubectl("delete", "-f", case14PolicyNamed, "-n", testNamespace)
		})
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case14PolicyUnnamed + " on managed")
			utils.Kubectl("apply", "-f", case14PolicyUnnamed, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case14PolicyUnnamedName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case14PolicyUnnamedName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
		It("should stay noncompliant when limitrange is in one matching namespace", func() {
			By("Creating limitrange " + case14LimitRangeName + " on range1")
			utils.Kubectl("apply", "-f", case14LimitRangeFile, "-n", "range1")
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case14PolicyUnnamedName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case14PolicyUnnamedName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case14PolicyUnnamedName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, time.Second*20, 1).Should(Equal("NonCompliant"))
		})
		It("should be compliant with limitrange in all matching namespaces", func() {
			By("Creating " + case14LimitRangeName + " on range2")
			utils.Kubectl("apply", "-f", case14LimitRangeFile, "-n", "range2")
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case14PolicyUnnamedName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case14PolicyUnnamedName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("Clean up unnamed policy and limitrange", func() {
			By("Deleting " + case14LimitRangeName + " on range1")
			utils.Kubectl("delete", "-f", case14LimitRangeFile, "-n", "range1")
			By("Deleting " + case14LimitRangeName + " on range2")
			utils.Kubectl("delete", "-f", case14LimitRangeFile, "-n", "range2")
			By("Deleting " + case14PolicyUnnamed + " on managed")
			utils.Kubectl("delete", "-f", case14PolicyUnnamed, "-n", testNamespace)
		})
	})
})
