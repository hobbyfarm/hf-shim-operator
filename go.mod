module github.com/ibrokethecloud/hf-ec2-vmcontroller

go 1.13

replace (
	k8s.io/client-go => k8s.io/client-go v0.17.2
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.5.0
)

require (
	github.com/appscode/jsonpatch v1.0.1 // indirect
	github.com/go-logr/logr v0.1.0
	github.com/hobbyfarm/gargantua v0.1.7
	github.com/ibrokethecloud/ec2-operator v0.0.0-20200907031959-a9f8469f5710
	github.com/onsi/ginkgo v1.11.0
	github.com/onsi/gomega v1.8.1
	gopkg.in/yaml.v2 v2.2.4
	k8s.io/api v0.17.2
	k8s.io/apimachinery v0.17.2
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	sigs.k8s.io/controller-runtime v0.5.0
	sigs.k8s.io/testing_frameworks v0.1.2 // indirect
)
