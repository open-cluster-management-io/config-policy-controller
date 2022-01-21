# v0.6.0

* Updated the operator-sdk from v0.x to v1.x. This is mostly an internal change, however, the
  controller now uses both `ConfigMap`s and the `Lease` API for leader election by default. If this
  controller is running on a Kubernetes version without the `Lease` API, use the `--legacy-leader-elect` flag.
* Fixed a bug that would cause the `relatedObjects` on `ConfigurationPolicy` object statuses to be stale when
  modifying a policy with an invalid hub policy template.
* Fixed a bug that would cause objects with byte values to be erroneously updated even though they matched the policy.
* Improved the API validation on ConfigurationPolicy objects.
* Updated logging to use zap. The log level is now configurable with CLI flags.
* Fixed a bug that occurred when object-templates set an invalid annotation type.
