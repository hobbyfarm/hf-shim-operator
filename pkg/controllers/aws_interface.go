package controllers

import (
	"context"
	b64 "encoding/base64"
	"fmt"

	ec2v1alpha1 "github.com/hobbyfarm/ec2-operator/pkg/api/v1alpha1"
	hfv1 "github.com/hobbyfarm/gargantua/pkg/apis/hobbyfarm.io/v1"
	"github.com/hobbyfarm/hf-shim-operator/pkg/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *VirtualMachineReconciler) createEC2ImportKeyPair(ctx context.Context, vm *hfv1.VirtualMachine,
	env *hfv1.Environment, pubKey string) (status *hfv1.VirtualMachineStatus, err error) {
	status = vm.Status.DeepCopy()

	credSecret, ok := env.Spec.EnvironmentSpecifics["cred_secret"]
	if !ok {
		return status, fmt.Errorf("no cred_secret found in env spec")
	}

	region, ok := env.Spec.EnvironmentSpecifics["region"]
	if !ok {
		return status, fmt.Errorf("no cred_secret found in env spec")
	}

	if !ok {
		return status, fmt.Errorf("no region found in env spec")
	}

	keyPair := &ec2v1alpha1.ImportKeyPair{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vm.Name,
			Namespace: provisionNS,
		},
	}

	if _, err = controllerutil.CreateOrUpdate(ctx, r.Client, keyPair, func() error {
		keyPair.Spec.PublicKey = pubKey
		keyPair.Spec.KeyName = vm.Name
		keyPair.Spec.Secret = credSecret
		keyPair.Spec.Region = region

		if err := controllerutil.SetControllerReference(vm, keyPair, r.Scheme); err != nil {
			r.Log.Error(err, "unable to set ownerReference for AWS keypair")
			return err
		}
		return nil
	}); err != nil {
		r.Log.Error(fmt.Errorf("Error creating keypair "), keyPair.Name)
		return status, err
	}

	vm.Annotations["importKeyPair"] = keyPair.Name
	status.Status = importKeyPairCreated
	return status, nil
}

// create an Ec2 instance managed by the parent VM
func (r *VirtualMachineReconciler) createEC2Instance(ctx context.Context, vm *hfv1.VirtualMachine,
	environment *hfv1.Environment, vmTemplate *hfv1.VirtualMachineTemplate) (err error) {

	instance := &ec2v1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vm.Name,
			Namespace: provisionNS,
		},
	}

	credSecret, ok := environment.Spec.EnvironmentSpecifics["cred_secret"]
	if !ok {
		return fmt.Errorf("no cred_secret found in env spec")
	}

	region, ok := environment.Spec.EnvironmentSpecifics["region"]
	if !ok {
		return fmt.Errorf("no region found in env spec")
	}

	subnet, ok := environment.Spec.EnvironmentSpecifics["subnet"]
	if !ok {
		return fmt.Errorf("no subnet found in env spec")
	}

	ami, ok := environment.Spec.TemplateMapping[vmTemplate.Name]["image"]
	if !ok {
		return fmt.Errorf("no ami specified for vm template in env spec")
	}

	cloudInit, _ := environment.Spec.TemplateMapping[vmTemplate.Name]["cloudInit"]

	if err != nil {
		return fmt.Errorf("error merging cloud init")
	}

	instanceType, ok := environment.Spec.TemplateMapping[vmTemplate.Name]["instanceType"]
	if !ok {
		instanceType = defaultInstanceType
	}

	securityGroup, ok := environment.Spec.EnvironmentSpecifics["vpc_security_group_id"]
	if !ok {
		return fmt.Errorf("no vpc_security_group_ip found in environment_specifics")
	}

	keyPair, ok := vm.Annotations["importKeyPair"]
	if !ok {
		return fmt.Errorf("no importKeyPair annotation found on vm object")
	}
	if _, err = controllerutil.CreateOrUpdate(ctx, r.Client, instance, func() error {
		instance.Spec.Secret = credSecret
		instance.Spec.SubnetID = subnet
		instance.Spec.ImageID = ami
		instance.Spec.Region = region
		instance.Spec.UserData = cloudInit
		instance.Spec.SecurityGroupIDS = []string{securityGroup}
		instance.Spec.InstanceType = instanceType
		instance.Spec.PublicIPAddress = true
		instance.Spec.KeyName = keyPair

		// Set owner //
		if err := controllerutil.SetControllerReference(vm, instance, r.Scheme); err != nil {
			r.Log.Error(err, "unable to set ownerReference for instance")
			return err
		}

		return nil
	}); err != nil {
		r.Log.Error(fmt.Errorf("Error creating instance "), instance.Name)
		return err
	}
	return nil
}

// Fetch EC2 Instance information //
func (r *VirtualMachineReconciler) fetchEC2Instance(ctx context.Context,
	vm *hfv1.VirtualMachine) (status *hfv1.VirtualMachineStatus, err error) {
	instance := &ec2v1alpha1.Instance{}
	status = vm.Status.DeepCopy()
	err = r.Get(ctx, types.NamespacedName{Name: vm.Name, Namespace: provisionNS}, instance)
	if err != nil {
		r.Log.Error(fmt.Errorf("Error fetching EC2 Instance: "), instance.Name)
		return status, err
	}
	if len(instance.Status.PublicIP) > 0 {
		status.PublicIP = instance.Status.PublicIP
	}

	if len(instance.Status.PrivateIP) > 0 {
		status.PrivateIP = instance.Status.PrivateIP
	}

	if len(instance.Status.InstanceID) > 0 {
		status.Hostname = instance.Status.InstanceID
	}
	if instance.Status.Status == "provisioned" {
		//perform VM liveness check before this is ready //
		ready, err := r.ec2LivenessCheck(ctx, vm, instance)
		if err != nil {
			return status, err
		}
		if ready {
			status.Status = hfv1.VmStatusRunning
		}
	}

	if status.Status != hfv1.VmStatusRunning {
		return status, fmt.Errorf("VM still not running")
	}
	return status, err
}

func (r *VirtualMachineReconciler) ec2LivenessCheck(ctx context.Context, vm *hfv1.VirtualMachine,
	instance *ec2v1alpha1.Instance) (ready bool, err error) {
	keySecret := &v1.Secret{}
	var username, address string
	err = r.Get(ctx, types.NamespacedName{Name: vm.Spec.KeyPair, Namespace: provisionNS}, keySecret)
	if err != nil {
		return ready, err
	}
	if len(vm.Spec.SshUsername) != 0 {
		username = vm.Spec.SshUsername
	} else {
		username = "ubuntu"
	}

	privKey, ok := keySecret.Data["private_key"]
	if !ok {
		return ready, fmt.Errorf("private_key not found in secret %s", keySecret.Name)
	}
	encodeKey := b64.StdEncoding.EncodeToString(privKey)

	if len(instance.Status.PublicIP) > 0 {
		address = instance.Status.PublicIP + ":22"
	} else {
		address = instance.Status.PrivateIP + ":22"
	}

	ready, err = utils.PerformLivenessCheck(address, username, encodeKey)
	return ready, err
}
