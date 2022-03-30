# v0.7.0

* Added support for the `metadataComplianceType` field to change the compliance type when evaluating the `metadata` field
* Added support for the `evaluationInterval` field to customize how often policies are evaluated
* Added support for decrypting content encrypted using the Hub `protect` template function
* Changed the log level flag to `--log-level` and `-v` for library logs
* Improved logging
* Improved performance
* Updated the dependencies
* Fixed a bug that could cause policy status updates not to be sent under heavy load
