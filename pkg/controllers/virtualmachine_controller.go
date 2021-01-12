/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/types"

	ec2v1alpha1 "github.com/ibrokethecloud/ec2-operator/pkg/api/v1alpha1"

	"github.com/go-logr/logr"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	hfv1 "github.com/hobbyfarm/gargantua/pkg/apis/hobbyfarm.io/v1"
	"github.com/hobbyfarm/gargantua/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VirtualMachineReconciler reconciles a VirtualMachine object
type VirtualMachineReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

var provisionNS = "hobbyfarm"
var defaultInstanceType = "t2.medium"

func init() {
	ns := os.Getenv("HF_NAMESPACE")
	if ns != "" {
		provisionNS = ns
	}
}

func (r *VirtualMachineReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {

	var err error
	ctx := context.Background()
	log := r.Log.WithValues("virtualmachine", req.NamespacedName)

	vm := &hfv1.VirtualMachine{}

	if err := r.Get(ctx, req.NamespacedName, vm); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch virtualmachine")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// initialize status //
	status := vm.Status.DeepCopy()

	// we only delete VMs that are tainted (and that also haven't already been deleted)
	// tainting occurs when a session ends, and gargantua marks the vm as tainted, indicating recycling can occur
	if vm.Status.Tainted && vm.ObjectMeta.DeletionTimestamp.IsZero() {
		// deprov logic
		// first, ensure the vm is not ready
		if vm.Labels["ready"] != "false" {
			vm.Labels["ready"] = "false"
			if err := r.Update(ctx, vm); err != nil {
				log.Error(fmt.Errorf("ErrUpdate"), "Error updating labels of VM")
				return ctrl.Result{}, nil
			}
		}

		// now that the vm is not ready, we can proceed with deleting it
		if err := r.Delete(ctx, vm); err != nil {
			log.Error(fmt.Errorf("ErrDelete"), "Error deleting VM")
			return ctrl.Result{}, nil
		}
		log.Info("VM deleted")
	}

	if vm.ObjectMeta.DeletionTimestamp.IsZero() {
		// provisioning logic
		if vm.Status.Status == hfv1.VmStatusRFP {
			status, err = r.launchInstance(ctx, vm)
		} else if vm.Status.Status == hfv1.VmStatusProvisioned {
			// Lets poll ec2 instance and get details
			status, err = r.fetchVMDetails(ctx, vm)
		} else {
			log.Info("vm is not in a valid status will be ignored")
			return ctrl.Result{}, nil
		}

		if err != nil {
			return ctrl.Result{}, err
		}

		vm.Status = *status
		if err := r.Update(ctx, vm); err != nil {
			log.Error(fmt.Errorf("ErrUpdate"), "Error Updating status of VM")
			return ctrl.Result{}, nil
		}
		// update status and requeue object so it can
		// fall through the workflow logic again

	}

	if vm.Status.Status != "running" {
		// lets requeue to allow it to fall through queue again
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

func (r *VirtualMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hfv1.VirtualMachine{}).
		Owns(&ec2v1alpha1.Instance{}).
		Owns(&ec2v1alpha1.ImportKeyPair{}).
		Owns(&v1.Secret{}).
		Complete(r)
}

// Fetch environment information //
func (r *VirtualMachineReconciler) fetchEnvironment(ctx context.Context,
	environmentName string) (environment *hfv1.Environment, err error) {
	environment = &hfv1.Environment{}
	err = r.Get(ctx, types.NamespacedName{Name: environmentName}, environment)
	if err != nil {
		r.Log.Error(fmt.Errorf("Error fetching envrionment: "), environmentName)
	}
	return environment, err
}

// Fetch VMTemplate information //
func (r *VirtualMachineReconciler) fetchVMTemplate(ctx context.Context,
	vmTemplateName string) (vmTemplate *hfv1.VirtualMachineTemplate, err error) {
	vmTemplate = &hfv1.VirtualMachineTemplate{}
	err = r.Get(ctx, types.NamespacedName{Name: vmTemplateName}, vmTemplate)
	if err != nil {
		r.Log.Error(fmt.Errorf("Error fetching VMTemplate: "), vmTemplateName)
	}

	return vmTemplate, err
}

// Fetch Instance information //
func (r *VirtualMachineReconciler) fetchInstance(ctx context.Context,
	instanceName string) (instance *ec2v1alpha1.Instance, err error) {
	instance = &ec2v1alpha1.Instance{}
	err = r.Get(ctx, types.NamespacedName{Name: instanceName, Namespace: provisionNS}, instance)
	if err != nil {
		r.Log.Error(fmt.Errorf("Error fetching EC2 Instance: "), instanceName)
	}
	return instance, err
}

// Launch a new EC2 Instance
func (r *VirtualMachineReconciler) launchInstance(ctx context.Context,
	vm *hfv1.VirtualMachine) (status *hfv1.VirtualMachineStatus,
	err error) {
	status = vm.Status.DeepCopy()
	var pubKey, privKey string
	vmTemplate, err := r.fetchVMTemplate(ctx, vm.Spec.VirtualMachineTemplateId)
	if err != nil {
		status.Status = "Error Fetching VMTemplate"
		return status, err
	}

	environment, err := r.fetchEnvironment(ctx, status.EnvironmentId)
	if err != nil {
		status.Status = "Error fetching Environment"
		return status, err
	}

	// Lets build the ssh key secret //
	// Check avoids recreation of secrets //
	if !keyCreationDone(vm, "secret-provisioned") {
		pubKey, privKey, err = util.GenKeyPair()
		if err != nil {
			status.Status = "Error generating ssh keypair"
			return status, err
		}

		err = r.createImportKeyPair(ctx, pubKey, vm, environment)
		if err != nil {
			r.Log.Info("Error creating keypair")
			return status, err
		}

		keyPairName, err := r.createSecret(ctx, pubKey, privKey, vm)
		if err != nil {
			r.Log.Info("Error during secret creation")
			return status, err
		}
		// update vm spec with Keypair SecretName
		vm.Spec.KeyPair = keyPairName
		vm.Labels["secret-provisioned"] = "true"

	}

	// create a ec2 Instance object //
	err = r.createEC2Instance(ctx, vm, environment, vmTemplate, pubKey)

	if err != nil {
		r.Log.Info("Error during instance creation")
		return status, err
	}
	status.WsEndpoint = environment.Spec.WsEndpoint
	status.Status = hfv1.VmStatusProvisioned

	return status, nil
}

// create a managed secret which contains the ssh keys
func (r *VirtualMachineReconciler) createSecret(ctx context.Context, pubKey string, privKey string,
	vm *hfv1.VirtualMachine) (keyPairName string, err error) {
	random := fmt.Sprintf("%08x", rand.Uint32())

	secretData := make(map[string][]byte)
	secretData["public_key"] = []byte(pubKey)
	secretData["private_key"] = []byte(privKey)
	secretName := strings.Join([]string{vm.Name + "-secret", random}, "-")
	keypair := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: provisionNS,
		},
	}

	if _, err = controllerutil.CreateOrUpdate(ctx, r.Client, keypair, func() error {

		keypair.Data = secretData

		if err := controllerutil.SetControllerReference(vm, keypair, r.Scheme); err != nil {
			r.Log.Error(err, "unable to set ownerReference for secret")
			return err
		}

		return nil
	}); err != nil {
		r.Log.Error(fmt.Errorf("Error creating secret "), secretName)
		return "", err
	}

	keyPairName = keypair.Name
	return keyPairName, nil
}

// create an Ec2 instance managed by the parent VM
func (r *VirtualMachineReconciler) createEC2Instance(ctx context.Context, vm *hfv1.VirtualMachine,
	environment *hfv1.Environment, vmTemplate *hfv1.VirtualMachineTemplate, pubKey string) (err error) {

	instance := &ec2v1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vm.Name,
			Namespace: provisionNS,
		},
	}

	credSecret, ok := environment.Spec.EnvironmentSpecifics["cred_secret"]
	if !ok {
		return fmt.Errorf("No cred_secret found in env spec")
	}

	region, ok := environment.Spec.EnvironmentSpecifics["region"]
	if !ok {
		return fmt.Errorf("No region found in env spec")
	}

	subnet, ok := environment.Spec.EnvironmentSpecifics["subnet"]
	if !ok {
		return fmt.Errorf("No subnet found in env spec")
	}

	ami, ok := environment.Spec.TemplateMapping[vmTemplate.Name]["image"]
	if !ok {
		return fmt.Errorf("No ami specified for vm template in env spec")
	}

	cloudInit, _ := environment.Spec.TemplateMapping[vmTemplate.Name]["cloudInit"]

	if err != nil {
		return fmt.Errorf("Error merging cloud init")
	}

	instanceType, ok := environment.Spec.TemplateMapping[vmTemplate.Name]["instanceType"]
	if !ok {
		instanceType = defaultInstanceType
	}

	securityGroup, ok := environment.Spec.EnvironmentSpecifics["vpc_security_group_id"]
	if !ok {
		return fmt.Errorf("No vpc_security_group_ip found in environment_specifics")
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
		instance.Spec.KeyName = vm.Name

		// Set owner //
		if err := controllerutil.SetControllerReference(vm, instance, r.Scheme); err != nil {
			r.Log.Error(err, "unable to set ownerReference for instance")
			return err
		}

		return nil
	}); err != nil {
		r.Log.Error(fmt.Errorf("Error creating insance "), instance.Name)
		return err
	}
	return nil
}

// fetch ec2 instance details to update the vm status

func (r *VirtualMachineReconciler) fetchVMDetails(ctx context.Context,
	vm *hfv1.VirtualMachine) (status *hfv1.VirtualMachineStatus, err error) {
	status = vm.Status.DeepCopy()

	instance, err := r.fetchInstance(ctx, vm.Name)
	if err != nil {
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
		status.Status = hfv1.VmStatusRunning
	}

	if status.Status != "running" {
		return status, fmt.Errorf("VM still not running")
	}

	vm.Labels["ready"] = "true"
	// VM is provisioned and we have all the endpoint info we needed //
	return status, nil
}

func (r *VirtualMachineReconciler) createImportKeyPair(ctx context.Context, pubKey string, vm *hfv1.VirtualMachine,
	env *hfv1.Environment) (err error) {

	pubKey = strings.TrimSpace(pubKey)
	keyPair := &ec2v1alpha1.ImportKeyPair{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vm.Name,
			Namespace: provisionNS,
		},
	}

	credSecret, ok := env.Spec.EnvironmentSpecifics["cred_secret"]
	if !ok {
		return fmt.Errorf("No cred_secret found in env spec")
	}

	region, ok := env.Spec.EnvironmentSpecifics["region"]
	if !ok {
		return fmt.Errorf("No cred_secret found in env spec")
	}

	if !ok {
		return fmt.Errorf("No region found in env spec")
	}

	if _, err = controllerutil.CreateOrUpdate(ctx, r.Client, keyPair, func() error {
		keyPair.Spec.PublicKey = pubKey
		keyPair.Spec.KeyName = vm.Name
		keyPair.Spec.Secret = credSecret
		keyPair.Spec.Region = region

		if err := controllerutil.SetControllerReference(vm, keyPair, r.Scheme); err != nil {
			r.Log.Error(err, "unable to set ownerReference for keypair")
			return err
		}
		return nil
	}); err != nil {
		r.Log.Error(fmt.Errorf("Error creating keypair "), keyPair.Name)
		return err
	}

	return nil
}

func keyCreationDone(vm *hfv1.VirtualMachine, key string) (ok bool) {
	_, ok = vm.Labels[key]
	return ok
}