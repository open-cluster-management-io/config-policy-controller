# v0.9.0

- Switch leader election default
- Enable foreground deletion
- Send compliance events in more situations
- Update named objects in arrays instead of appending
- Perform validation of object definitions
- Evaluate policies on every loop when evaluationInterval isn't set
- Add metrics to record how long each config policy takes to be evaluated
- Add metrics to time the compare algorithm
- Send compliance state events synchronously for reliable ordering of compliance events
- Allow enforcing policies that define a status with missing required fields
- Update to Go v1.19
- Favor the existing array ordering and content when comparing arrays
