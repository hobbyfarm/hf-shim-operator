## hf-shim-operator

Operator to handle Hobbyfarm VM types and generate associated ec2-operator Instances

It interacts with the Hobbyfarm VirtualMachineTemplates, Environment and VirtualMachine resources, and allows users 
to replace the terraform provisioning controller.

It creates ec2 Instance and ImportKeyPair resources which are managed by the [ec2-operator](https://github.com/hobbyfarm/ec2-operator)

The controller will also create pub/private keypair secrets in your namespace to allow the Gargantua shell controller
to allow ssh into the instances launched by this and the ec2-operator.

There is a helm chart available which allows for management of the same.

To get started the chart can be installed as follows:

```
helm install hf-shim-operator ./chart/hf-shim-operator
```