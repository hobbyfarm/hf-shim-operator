module github.com/ibrokethecloud/hf-ec2-vmcontroller

go 1.13

replace (
	k8s.io/client-go => k8s.io/client-go v0.17.2
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.5.0
)

require (
	github.com/appscode/jsonpatch v1.0.1 // indirect
	github.com/dgrijalva/jwt-go v3.2.1-0.20200107013213-dc14462fd587+incompatible // indirect
	github.com/go-logr/logr v0.1.0
	github.com/go-logr/zapr v0.1.0 // indirect
	github.com/golang/groupcache v0.0.0-20190129154638-5b532d6fd5ef // indirect
	github.com/googleapis/gnostic v0.3.1 // indirect
	github.com/gorilla/handlers v1.4.0 // indirect
	github.com/gorilla/mux v1.7.1 // indirect
	github.com/imdario/mergo v0.3.7 // indirect
	github.com/jetstack/cert-manager v0.7.2 // indirect
	github.com/knative/build v0.6.0 // indirect
	github.com/knative/pkg v0.0.0-20190514205332-5e4512dcb2ca // indirect
	github.com/matryer/moq v0.0.0-20190312154309-6cfb0558e1bd // indirect
	github.com/mattbaird/jsonpatch v0.0.0-20171005235357-81af80346b1a // indirect
	github.com/onsi/ginkgo v1.11.0
	github.com/onsi/gomega v1.8.1
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	golang.org/x/crypto v0.0.0-20191227163750-53104e6ec876 // indirect
	gomodules.xyz/jsonpatch/v2 v2.0.1 // indirect
	gopkg.in/yaml.v2 v2.2.4
	k8s.io/api v0.17.2
	k8s.io/apimachinery v0.17.2
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/code-generator v0.17.2 // indirect
	sigs.k8s.io/controller-runtime v0.5.0
	sigs.k8s.io/testing_frameworks v0.1.2 // indirect
)
