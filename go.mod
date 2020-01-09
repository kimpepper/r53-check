module github.com/skpr/r53-check

go 1.13

require (
	github.com/aws/aws-sdk-go v1.27.3
	github.com/go-logr/logr v0.1.0
	github.com/go-test/deep v1.0.4
	github.com/onsi/ginkgo v1.8.0
	github.com/onsi/gomega v1.5.0
	golang.org/x/crypto v0.0.0-20191011191535-87dc89f01550 // indirect
	golang.org/x/net v0.0.0-20191209160850-c0dbc17a3553 // indirect
	golang.org/x/xerrors v0.0.0-20191011141410-1b5146add898 // indirect
	k8s.io/apimachinery v0.0.0-20190913080033-27d36303b655
	k8s.io/client-go v0.0.0-20190918160344-1fbdaa4c8d90
	sigs.k8s.io/controller-runtime v0.4.0
)
