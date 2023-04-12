module github.com/hobbyfarm/hf-shim-operator

go 1.16

replace (
	github.com/hobbyfarm/ec2-operator => github.com/hobbyfarm/ec2-operator v0.1.7
	k8s.io/client-go => k8s.io/client-go v0.23.0
)

require (
	github.com/go-logr/logr v1.2.0
	github.com/hobbyfarm/ec2-operator v0.0.0-20210503053736-8f6f258f7b24
	github.com/hobbyfarm/gargantua v1.0.0
	github.com/hobbyfarm/metal-operator v1.0.0
	github.com/ibrokethecloud/droplet-operator v0.0.0-20210505085619-7a30ebe921b2
	github.com/ibrokethecloud/k3s-operator v0.0.0-20210110055129-f26a2d855653
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.17.0
	github.com/sirupsen/logrus v1.8.1
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.23.0
	k8s.io/apimachinery v0.23.0
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	sigs.k8s.io/controller-runtime v0.11.0

)
