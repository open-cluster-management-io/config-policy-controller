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
	"k8s.io/klog"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	"open-cluster-management.io/config-policy-controller/pkg/common"
)

// TriggerUninstall will add an annotation to the controller's Deployment indicating that the controller needs to
// prepare to be uninstalled. This function will run until all ConfigurationPolicy objects have no finalizers.
func TriggerUninstall(
	ctx context.Context, config *rest.Config, deploymentName, deploymentNamespace, policyNamespace string,
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
		annotations[common.UninstallingAnnotation] = "true"
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

		configPolicies, err := dynamicClient.Resource(configPolicyGVR).Namespace(policyNamespace).List(
			ctx, metav1.ListOptions{},
		)
		if err != nil {
			if k8serrors.IsServerTimeout(err) || k8serrors.IsTimeout(err) {
				klog.Infof("Retrying listing the ConfigurationPolicy objects due to error: %s", err)

				continue
			}

			return err
		}

		cleanedUp := true

		for _, configPolicy := range configPolicies.Items {
			if len(configPolicy.GetFinalizers()) != 0 {
				cleanedUp = false

				break
			}
		}

		if cleanedUp {
			break
		}

		klog.Info("The uninstall preparation is not complete. Sleeping two seconds before checking again.")
		time.Sleep(2 * time.Second)
	}

	return nil
}
