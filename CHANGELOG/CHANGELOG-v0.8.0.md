# v0.8.0

- Add the `pruneObjectBehavior` field to be able to delete objects after the policy is deleted.
- Allow an alternative kubeconfig for evaluating policies.
- Make the metrics bind address configurable.
- Add the option for label selectors in the policy `namespaceSelector`.
- Add a metric to record the duration of evaluating all configuration policies.
- Add the option to evaluate policies concurrently.
- Status in a policy no longer prevents spec updates.
- Use a volume mounted secret for the Hub kubeconfig.
