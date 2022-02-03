package controllers

import (
	"context"
	b64 "encoding/base64"
	"fmt"

	"github.com/hobbyfarm/hf-shim-operator/pkg/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	hfv1 "github.com/hobbyfarm/gargantua/pkg/apis/hobbyfarm.io/v1"
	equinixv1alpha1 "github.com/hobbyfarm/metal-operator/pkg/api/v1alpha1"
)

// createEquinixImportKeyPair will create the ssh key pair in the project
func (r *VirtualMachineReconciler) createEquinixImportKeyPair(ctx context.Context, vm *hfv1.VirtualMachine,
	env *hfv1.Environment, pubKey string) (status *hfv1.VirtualMachineStatus, err error) {
	status = vm.Status.DeepCopy()

	credSecret, ok := env.Spec.EnvironmentSpecifics["cred_secret"]
	if !ok {
		return status, fmt.Errorf("no cred_secret found in env spec")
	}

	if !ok {
		return status, fmt.Errorf("no region found in env spec")
	}

	keyPair := &equinixv1alpha1.ImportKeyPair{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vm.Name,
			Namespace: vm.Namespace,
		},
	}

	if _, err = controllerutil.CreateOrUpdate(ctx, r.Client, keyPair, func() error {
		keyPair.Spec.Key = pubKey
		keyPair.Spec.Secret = credSecret

		if err := controllerutil.SetControllerReference(vm, keyPair, r.Scheme); err != nil {
			r.Log.Error(err, "unable to set owner reference for Equinix keypair")
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

func (r *VirtualMachineReconciler) createEquinixInstance(ctx context.Context, vm *hfv1.VirtualMachine,
	env *hfv1.Environment, vmTemplate *hfv1.VirtualMachineTemplate) (err error) {
	credSecret, ok := env.Spec.EnvironmentSpecifics["cred_secret"]
	if !ok {
		return fmt.Errorf("no cred_secret found in env spec")
	}

	billingCycle, ok := env.Spec.EnvironmentSpecifics["billing_cycle"]
	if !ok {
		billingCycle = defaultEquinixBillingCycle
	}

	ipxeScriptURL, ok := env.Spec.EnvironmentSpecifics["ipxe_script_url"]
	if !ok {
		ipxeScriptURL = defaultIPXEScriptURL
	}
	region, ok := env.Spec.EnvironmentSpecifics["region"]
	if !ok {
		return fmt.Errorf("no region found in env spec")
	}

	instance := &equinixv1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vm.Name,
			Namespace: vm.Namespace,
		},
	}

	instanceType, ok := env.Spec.TemplateMapping[vmTemplate.Name]["instanceType"]
	if !ok {
		instanceType = defaultEquinixInstanceType
	}

	vm.Annotations[instanceTypeAnnotation] = instanceType

	equinixKeyPair := &equinixv1alpha1.ImportKeyPair{}
	err = r.Get(ctx, types.NamespacedName{Namespace: vm.Namespace, Name: vm.Annotations["importKeyPair"]}, equinixKeyPair)
	if err != nil {
		return err
	}

	if equinixKeyPair.Status.KeyPairID == "" {
		return fmt.Errorf("equinix importKeyPair not yet processed")
	}

	if _, err = controllerutil.CreateOrUpdate(ctx, r.Client, instance, func() error {
		instance.Spec.Facility = []string{region}
		instance.Spec.Secret = credSecret
		instance.Spec.OS = "custom_ipxe"
		instance.Spec.BillingCycle = billingCycle
		instance.Spec.IPXEScriptURL = ipxeScriptURL
		instance.Spec.ProjectSSHKeys = []string{equinixKeyPair.Status.KeyPairID}
		instance.Spec.Plan = instanceType

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

func (r *VirtualMachineReconciler) fetchEquinixInstance(ctx context.Context,
	vm *hfv1.VirtualMachine) (status *hfv1.VirtualMachineStatus, err error) {
	status = vm.Status.DeepCopy()
	instance := &equinixv1alpha1.Instance{}
	err = r.Get(ctx, types.NamespacedName{Name: vm.Name, Namespace: vm.Namespace}, instance)
	if err != nil {
		r.Log.Error(fmt.Errorf("error fetching equinix instance: "), vm.Name)
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

	if instance.Status.Status == "active" {
		// additional update for vm object to make it possible to ssh into instance
		vm.Spec.SshUsername = instance.Status.InstanceID
		status.Status = hfv1.VmStatusRunning
		vm.Annotations["sshEndpoint"] = fmt.Sprintf("sos.%s.platformequinix.com", instance.Spec.Facility[0])
	}

	if status.Status != hfv1.VmStatusRunning {
		return status, fmt.Errorf("VM still not running")
	}
	return status, err
}

func (r *VirtualMachineReconciler) equinixLivenessCheck(ctx context.Context, vm *hfv1.VirtualMachine,
	instance *equinixv1alpha1.Instance) (ready bool, err error) {
	keySecret := &v1.Secret{}
	var username, address string
	err = r.Get(ctx, types.NamespacedName{Name: vm.Spec.KeyPair, Namespace: vm.Namespace}, keySecret)
	if err != nil {
		return ready, err
	}
	username = instance.Status.InstanceID
	privKey, ok := keySecret.Data["private_key"]
	if !ok {
		return ready, fmt.Errorf("private_key not found in secret %s", keySecret.Name)
	}
	encodeKey := b64.StdEncoding.EncodeToString(privKey)
	address = fmt.Sprintf("sos.%s.platformequinix.com:22", instance.Spec.Facility[0])
	vm.Annotations["sshEndpoint"] = fmt.Sprintf("sos.%s.platformequinix.com", instance.Spec.Facility[0])
	ready, err = utils.PerformLivenessCheck(address, username, encodeKey, "help")
	return ready, err
}
