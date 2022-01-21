package controllers

import (
	"context"
	b64 "encoding/base64"
	"fmt"

	hfv1 "github.com/hobbyfarm/gargantua/pkg/apis/hobbyfarm.io/v1"
	"github.com/hobbyfarm/hf-shim-operator/pkg/utils"
	dropletv1alpha1 "github.com/ibrokethecloud/droplet-operator/pkg/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *VirtualMachineReconciler) createDOImportKeyPair(ctx context.Context, vm *hfv1.VirtualMachine,
	env *hfv1.Environment, pubKey string) (status *hfv1.VirtualMachineStatus, err error) {
	status = vm.Status.DeepCopy()

	credSecret, ok := env.Spec.EnvironmentSpecifics["cred_secret"]
	if !ok {
		return status, fmt.Errorf("No cred_secret found in env spec")
	}

	keyPair := &dropletv1alpha1.ImportKeyPair{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vm.Name,
			Namespace: vm.Namespace,
		},
	}

	if _, err = controllerutil.CreateOrUpdate(ctx, r.Client, keyPair, func() error {
		keyPair.Spec.PublicKey = pubKey
		keyPair.Spec.Secret = credSecret

		if err := controllerutil.SetControllerReference(vm, keyPair, r.Scheme); err != nil {
			r.Log.Error(err, "unable to set ownerReference for DO keypair")
			return err
		}
		return nil
	}); err != nil {
		return status, err
	}

	vm.Annotations["importKeyPair"] = keyPair.Name
	status.Status = importKeyPairCreated
	return status, nil
}

func (r *VirtualMachineReconciler) createDropletInstance(ctx context.Context, vm *hfv1.VirtualMachine,
	environment *hfv1.Environment, vmTemplate *hfv1.VirtualMachineTemplate) (err error) {
	credSecret, ok := environment.Spec.EnvironmentSpecifics["cred_secret"]
	if !ok {
		return fmt.Errorf("no cred_secret found in env spec")
	}

	region, ok := environment.Spec.EnvironmentSpecifics["region"]
	if !ok {
		return fmt.Errorf("no region found in env spec")
	}

	instance := &dropletv1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vm.Name,
			Namespace: vm.Namespace,
		},
	}

	instanceType, ok := environment.Spec.TemplateMapping[vmTemplate.Name]["instanceType"]
	if !ok {
		instanceType = defaultDOInstanceType
	}

	vm.Annotations[instanceTypeAnnotation] = instanceType

	slug, ok := environment.Spec.TemplateMapping[vmTemplate.Name]["image"]
	if !ok {
		return fmt.Errorf("no image specified for vm template in env spec")
	}
	instance.Spec.Image.Slug = slug
	cloudInit, ok := environment.Spec.TemplateMapping[vmTemplate.Name]["cloudInit"]
	if ok {
		instance.Spec.UserData = cloudInit
	}

	backup, ok := environment.Spec.TemplateMapping[vmTemplate.Name]["backup"]
	if ok && backup == "true" {
		instance.Spec.Backups = true
	}

	ipv6, ok := environment.Spec.TemplateMapping[vmTemplate.Name]["ipv6"]
	if ok && ipv6 == "true" {
		instance.Spec.IPv6 = true
	}

	privateNetworking, ok := environment.Spec.TemplateMapping[vmTemplate.Name]["privateNetworking"]
	if ok && privateNetworking == "true" {
		instance.Spec.PrivateNetworking = true
	}

	vpcuuid, ok := environment.Spec.TemplateMapping[vmTemplate.Name]["vpcUuid"]
	if ok {
		instance.Spec.VPCUUID = vpcuuid
	}

	doKeyPair := &dropletv1alpha1.ImportKeyPair{}

	err = r.Get(ctx, types.NamespacedName{Namespace: vm.Namespace, Name: vm.Annotations["importKeyPair"]}, doKeyPair)
	if err != nil {
		return err
	}

	if doKeyPair.Status.ID == 0 || len(doKeyPair.Status.FingerPrint) == 0 {
		return fmt.Errorf("droplet importKeyPair not yet processed")
	}

	var dropletKeys []dropletv1alpha1.DropletCreateSSHKey

	dropletKey := dropletv1alpha1.DropletCreateSSHKey{
		ID:          doKeyPair.Status.ID,
		Fingerprint: doKeyPair.Status.FingerPrint,
	}
	dropletKeys = append(dropletKeys, dropletKey)

	if _, err = controllerutil.CreateOrUpdate(ctx, r.Client, instance, func() error {
		instance.Spec.Name = vm.Name
		instance.Spec.Secret = credSecret
		instance.Spec.Region = region
		instance.Spec.Size = instanceType
		instance.Spec.SSHKeys = dropletKeys
		// Set owner //
		if err := controllerutil.SetControllerReference(vm, instance, r.Scheme); err != nil {
			r.Log.Error(err, "unable to set ownerReference for instance")
			return err
		}

		return nil
	}); err != nil {
		r.Log.Error(fmt.Errorf("error creating instance "), instance.Name)
		return err
	}
	return nil

}

// Fetch Droplet Instance information //
func (r *VirtualMachineReconciler) fetchDOInstance(ctx context.Context,
	vm *hfv1.VirtualMachine) (status *hfv1.VirtualMachineStatus, err error) {
	status = vm.Status.DeepCopy()
	instance := &dropletv1alpha1.Instance{}
	err = r.Get(ctx, types.NamespacedName{Name: vm.Name, Namespace: vm.Namespace}, instance)
	if err != nil {
		r.Log.Error(fmt.Errorf("Error fetching Droplet Instance: "), vm.Name)
		return status, err
	}

	if len(instance.Status.PublicIP) > 0 {
		status.PublicIP = instance.Status.PublicIP
	}

	if len(instance.Status.PrivateIP) > 0 {
		status.PrivateIP = instance.Status.PrivateIP
	}

	if instance.Status.InstanceID > 0 {
		status.Hostname = instance.Name
	}
	if instance.Status.Status == "provisioned" {
		//perform VM liveness check before this is ready //
		ready, err := r.doLivenessCheck(ctx, vm, instance)
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

// DO liveness check
func (r *VirtualMachineReconciler) doLivenessCheck(ctx context.Context, vm *hfv1.VirtualMachine,
	instance *dropletv1alpha1.Instance) (ready bool, err error) {
	keySecret := &v1.Secret{}
	var username, address string
	err = r.Get(ctx, types.NamespacedName{Name: vm.Spec.KeyPair, Namespace: vm.Namespace}, keySecret)
	if err != nil {
		return ready, err
	}
	if len(vm.Spec.SshUsername) != 0 {
		username = vm.Spec.SshUsername
	} else {
		username = "root"
	}

	privKey, ok := keySecret.Data["private_key"]
	if !ok {
		return ready, fmt.Errorf("private_key not found in secret %s", keySecret.Name)
	}
	encodeKey := b64.StdEncoding.EncodeToString(privKey)

	if len(instance.Status.PublicIP) > 0 {
		address = instance.Status.PublicIP + ":22"
		vm.Annotations["sshEndpoint"] = instance.Status.PublicIP
	} else {
		address = instance.Status.PrivateIP + ":22"
	}

	ready, err = utils.PerformLivenessCheck(address, username, encodeKey, "uptime")
	return ready, err
}
