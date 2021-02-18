module github.com/ibrokethecloud/hf-ec2-vmcontroller

go 1.13

replace (
	k8s.io/client-go => k8s.io/client-go v0.17.2
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.5.0
)

require (
	github.com/go-logr/logr v0.1.0
	github.com/hobbyfarm/gargantua v0.1.8
	github.com/ibrokethecloud/ec2-operator v0.0.0-20200909043908-30b62dc8600c
	github.com/ibrokethecloud/k3s-operator v0.0.0-20210110055129-f26a2d855653
	github.com/onsi/ginkgo v1.11.0
	github.com/onsi/gomega v1.8.1
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	gopkg.in/yaml.v2 v2.2.4
	k8s.io/api v0.17.2
	k8s.io/apimachinery v0.17.2
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	sigs.k8s.io/controller-runtime v0.5.0
)
