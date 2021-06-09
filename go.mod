module github.com/hobbyfarm/hf-shim-operator

go 1.13

replace (
	github.com/go-logr/logr => github.com/go-logr/logr v0.1.0
	k8s.io/api => k8s.io/api v0.17.2
	k8s.io/apimachinery => k8s.io/apimachinery v0.17.2
	k8s.io/client-go => k8s.io/client-go v0.17.2
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.5.14

)

require (
	github.com/go-logr/logr v0.3.0
	github.com/hobbyfarm/ec2-operator v0.0.0-20210503053736-8f6f258f7b24
	github.com/hobbyfarm/gargantua v0.1.8
	github.com/ibrokethecloud/droplet-operator v0.0.0-20210505085619-7a30ebe921b2
	github.com/ibrokethecloud/ec2-operator v0.0.0-20200909043908-30b62dc8600c
	github.com/ibrokethecloud/k3s-operator v0.0.0-20210110055129-f26a2d855653
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/sirupsen/logrus v1.6.0
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	gopkg.in/yaml.v2 v2.3.0
	k8s.io/api v0.20.2
	k8s.io/apimachinery v0.20.2
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	sigs.k8s.io/controller-runtime v0.8.3
)
