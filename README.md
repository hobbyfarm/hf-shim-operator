## HF EC2 VM interface controller

Controller to handle Hobbyfarm VM types and generate associated ec2 Instances.

It interacts with the Hobbyfarm VirtualMachineTemplates, Environment and VirtualMachine resources, and allows users 
to replace the terraform provisioning controller.

It creates ec2 Instance and ImportKeyPair resources which are managed by the [ec2-operator](https://github.com/ibrokethecloud/ec2-operator)

The controller will also create pub/private keypair secrets in your namespace to allow the Gargantua shell controller
to allow ssh into the instances launched by this and the ec2-operator.

There is a helm chart available which allows for management of the same.

To get started the chart can be installed as follows:

```
helm install hf-ec2-vmcontroller ./chart/hf-ec2-vmcontroller
```