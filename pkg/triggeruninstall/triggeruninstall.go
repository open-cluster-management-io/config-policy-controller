// Copyright Contributors to the Open Cluster Management project

package triggeruninstall

import (
	"context"
	"fmt"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	"open-cluster-management.io/config-policy-controller/pkg/common"
)

// TriggerUninstall will add an annotation to the controller's Deployment indicating that the controller needs to
// prepare to be uninstalled. This function will run until all ConfigurationPolicy objects have no finalizers.
func TriggerUninstall(
	ctx context.Context,
	config *rest.Config,
	deploymentName string,
	deploymentNamespace string,
	policyNamespaces []string,
) error {
	client := kubernetes.NewForConfigOrDie(config)
	dynamicClient := dynamic.NewForConfigOrDie(config)

	for {
		klog.Info("Setting the Deployment uninstall annotation")
		var err error

		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled before the uninstallation preparation was complete")
		default:
		}

		deploymentRsrc := client.AppsV1().Deployments(deploymentNamespace)

		deployment, err := deploymentRsrc.Get(ctx, deploymentName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		annotations := deployment.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}

		annotations[common.UninstallingAnnotation] = time.Now().Format(time.RFC3339)
		deployment.SetAnnotations(annotations)

		_, err = deploymentRsrc.Update(ctx, deployment, metav1.UpdateOptions{})
		if err != nil {
			if k8serrors.IsServerTimeout(err) || k8serrors.IsTimeout(err) || k8serrors.IsConflict(err) {
				klog.Infof("Retrying setting the Deployment uninstall annotation due to error: %s", err)

				continue
			}

			return err
		}

		break
	}

	configPolicyGVR := schema.GroupVersionResource{
		Group:    policyv1.GroupVersion.Group,
		Version:  policyv1.GroupVersion.Version,
		Resource: "configurationpolicies",
	}

	for {
		klog.Info("Checking if the uninstall preparation is complete")

		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled before the uninstallation preparation was complete")
		default:
		}

		cleanedUp := true

		for _, ns := range policyNamespaces {
			policyClient := dynamicClient.Resource(configPolicyGVR).Namespace(ns)

			configPolicies, err := policyClient.List(ctx, metav1.ListOptions{})
			if err != nil {
				klog.Errorf("Error listing policies: %s", err)

				return err
			}

			for _, configPolicy := range configPolicies.Items {
				if len(configPolicy.GetFinalizers()) != 0 {
					cleanedUp = false

					annos := configPolicy.GetAnnotations()
					if annos == nil {
						annos = map[string]string{}
					}

					annos[common.UninstallingAnnotation] = time.Now().Format(time.RFC3339)

					updatedPolicy := configPolicy.DeepCopy()

					updatedPolicy.SetAnnotations(annos)

					if _, err := policyClient.Update(ctx, updatedPolicy, metav1.UpdateOptions{}); err != nil {
						klog.Errorf("Error updating policy %v/%v with an uninstalling annotation: %s",
							configPolicy.GetNamespace(), configPolicy.GetName(), err)

						return err
					}
				}
			}
		}

		if cleanedUp {
			break
		}

		klog.Info("The uninstall preparation is not complete. Sleeping five seconds before checking again.")
		time.Sleep(5 * time.Second)
	}

	return nil
}
