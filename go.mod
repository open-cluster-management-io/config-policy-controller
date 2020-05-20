module github.com/open-cluster-management/config-policy-controller

go 1.13

require (
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.0
	github.com/operator-framework/operator-sdk v0.17.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.4.0
	golang.org/x/net v0.0.0-20200226121028-0de0cce0169b
	k8s.io/api v0.17.4
	k8s.io/apimachinery v0.17.4
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/klog v1.0.0
	k8s.io/kube-openapi v0.0.0-20200121204235-bf4fb3bd569c // indirect
	sigs.k8s.io/controller-runtime v0.5.2
// github.com/open-cluster-management/governance-policy-propagator v0.0.0-20200511192636-919b118d8de3
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	github.com/open-cluster-management/config-policy-controller/test => ./test
	k8s.io/client-go => k8s.io/client-go v0.17.4 // Required by prometheus-operator
)
