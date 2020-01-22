module github.ibm.com/IBMPrivateCloud/multicloud-operators-policy-controller

require (
	github.com/alecthomas/gometalinter v3.0.0+incompatible // indirect
	github.com/alecthomas/units v0.0.0-20190924025748-f65c72e2690d // indirect
	github.com/apg/patter v0.0.0-20160930163000-bd185be70ac8 // indirect
	github.com/coreos/ v3.3.18+incompatible // indirect
	github.com/go-openapi/spec v0.17.2
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/onsi/gomega v1.5.0
	github.com/operator-framework/operator-sdk v0.11.1-0.20191021213754-e7135197e816
	github.com/spf13/pflag v1.0.3
	github.com/stretchr/testify v1.4.0
	github.com/wadey/gocovmerge v0.0.0-20160331181800-b5bfa59ec0ad // indirect
	github.ibm.com/OMaaS/alerttargetcontroller v0.0.0-20190912101104-acf59dd4715d
	golang.org/x/lint v0.0.0-20191125180803-fdd1cda4f05f // indirect
	golang.org/x/net v0.0.0-20190620200207-3b0461eec859
	golang.org/x/tools v0.0.0-20200122042241-dc16b66866f1 // indirect
	gopkg.in/alecthomas/kingpin.v3-unstable v3.0.0-20191105091915-95d230a53780 // indirect
	k8s.io/api v0.0.0-20190918155943-95b840bb6a1f
	k8s.io/apimachinery v0.0.0-20190913080033-27d36303b655
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/kube-openapi v0.0.0-20190401085232-94e1e7b7574c
	sigs.k8s.io/controller-runtime v0.2.0
)

// Pinned to kubernetes-1.14.1
replace (
	k8s.io/api => k8s.io/api v0.0.0-20190409021203-6e4e0e4f393b
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20190409022649-727a075fdec8
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190404173353-6a84e37a896d
	k8s.io/client-go => k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.0.0-20190409023720-1bc0c81fa51d
)

replace (
	// Indirect operator-sdk dependencies use git.apache.org, which is frequently
	// down. The github mirror should be used instead.
	// Locking to a specific version (from 'go mod graph'):
	git.apache.org/thrift.git => github.com/apache/thrift v0.0.0-20180902110319-2566ecd5d999
	github.com/coreos/prometheus-operator => github.com/coreos/prometheus-operator v0.31.1
	// Pinned to v2.10.0 (kubernetes-1.14.1) so https://proxy.golang.org can
	// resolve it correctly.
	github.com/prometheus/prometheus => github.com/prometheus/prometheus v1.8.2-0.20190525122359-d20e84d0fb64
)

replace github.com/operator-framework/operator-sdk => github.com/operator-framework/operator-sdk v0.11.0

go 1.13
