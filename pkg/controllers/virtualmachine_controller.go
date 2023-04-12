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
	b64 "encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"k8s.io/client-go/util/workqueue"

	dropletv1alpha1 "github.com/ibrokethecloud/droplet-operator/pkg/api/v1alpha1"
	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/types"

	ec2v1alpha1 "github.com/hobbyfarm/ec2-operator/pkg/api/v1alpha1"

	"github.com/go-logr/logr"
	hfv1 "github.com/hobbyfarm/gargantua/pkg/apis/hobbyfarm.io/v1"
	"github.com/hobbyfarm/gargantua/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlCtrl "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// VirtualMachineReconciler reconciles a VirtualMachine object
type VirtualMachineReconciler struct {
	client.Client
	Log     logr.Logger
	Scheme  *runtime.Scheme
	Threads int
}

var provisionNS = "hobbyfarm"
var defaultInstanceType = "t2.medium"

const (
	secretCreated              = "SecretCreated"
	importKeyPairCreated       = "ImportKeyPairCreated"
	defaultDOInstanceType      = "s-4vcpu-8gb"
	defaultEquinixInstanceType = "c3.small.x86"
	defaultEquinixBillingCycle = "hourly"
	defaultIPXEScriptURL       = "https://raw.githubusercontent.com/ibrokethecloud/custom_pxe/master/shell.ipxe"

	instanceTypeAnnotation = "hobbyfarm.io/instance-type"
)

func init() {
	ns := os.Getenv("HF_NAMESPACE")
	if ns != "" {
		provisionNS = ns
	}

}

func (r *VirtualMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	var err error
	//var ignoreVM bool
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
		switch state := vm.Status.Status; state {
		case hfv1.VmStatusRFP:
			status, err = r.createSecret(ctx, vm)
			if err != nil {
				return ctrl.Result{}, err
			}
		case secretCreated:
			status, err = r.createImportKeyPair(ctx, vm)
			if err != nil {
				return ctrl.Result{}, err
			}
		case importKeyPairCreated:
			status, err = r.launchInstance(ctx, vm)
			if err != nil {
				return ctrl.Result{}, err
			}
		case hfv1.VmStatusProvisioned:
			status, err = r.fetchVMDetails(ctx, vm)
			if err != nil {
				return ctrl.Result{}, err
			}
		case hfv1.VmStatusRunning:
			return ctrl.Result{}, nil
		case "default":
			return ctrl.Result{Requeue: false}, fmt.Errorf("VM in an undefined state. Ignoring")
		}
		vm.Status = *status
	}
	// if ignoreVM is not true.. we need to requeue to make sure we check the
	// ssh works
	err = r.Status().Update(ctx, vm.DeepCopy())
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.Update(ctx, vm)
}

func (r *VirtualMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(ctrlCtrl.Options{
			MaxConcurrentReconciles: r.Threads,
			RateLimiter:             workqueue.NewItemExponentialFailureRateLimiter(5*time.Second, 10*time.Second),
		}).
		For(&hfv1.VirtualMachine{}).
		Owns(&ec2v1alpha1.Instance{}).
		Owns(&ec2v1alpha1.ImportKeyPair{}).
		Owns(&dropletv1alpha1.Instance{}).
		Owns(&dropletv1alpha1.ImportKeyPair{}).
		Owns(&v1.Secret{}).
		Complete(r)
}

// Fetch environment information //
func (r *VirtualMachineReconciler) fetchEnvironment(ctx context.Context,
	environmentName string, namespace string) (environment *hfv1.Environment, err error) {
	environment = &hfv1.Environment{}
	err = r.Get(ctx, types.NamespacedName{Name: environmentName, Namespace: namespace}, environment)
	if err != nil {
		r.Log.Error(fmt.Errorf("Error fetching envrionment: "), environmentName)
	}
	return environment, err
}

// Fetch VMTemplate information //
func (r *VirtualMachineReconciler) fetchVMTemplate(ctx context.Context,
	vmTemplateName string, namespace string) (vmTemplate *hfv1.VirtualMachineTemplate, err error) {
	vmTemplate = &hfv1.VirtualMachineTemplate{}
	err = r.Get(ctx, types.NamespacedName{Name: vmTemplateName, Namespace: namespace}, vmTemplate)
	if err != nil {
		r.Log.Error(fmt.Errorf("Error fetching VMTemplate: "), vmTemplateName)
	}

	return vmTemplate, err
}

// Launch a new EC2 Instance
func (r *VirtualMachineReconciler) launchInstance(ctx context.Context,
	vm *hfv1.VirtualMachine) (status *hfv1.VirtualMachineStatus,
	err error) {
	status = vm.Status.DeepCopy()
	vmTemplate, err := r.fetchVMTemplate(ctx, vm.Spec.VirtualMachineTemplateId, vm.Namespace)
	if err != nil {
		status.Status = "Error Fetching VMTemplate"
		return status, err
	}

	environment, err := r.fetchEnvironment(ctx, status.EnvironmentId, vm.Namespace)
	if err != nil {
		status.Status = "Error fetching Environment"
		return status, err
	}

	// create a associated cloud provider instance //
	switch environment.Spec.Provider {
	case "aws":
		err = r.createEC2Instance(ctx, vm, environment, vmTemplate)
	case "digitalocean":
		err = r.createDropletInstance(ctx, vm, environment, vmTemplate)
	case "equinix":
		err = r.createEquinixInstance(ctx, vm, environment, vmTemplate)
	default:
		err = fmt.Errorf("unsupported environment type. currently support aws and digitalocean environments only")
	}

	if err != nil {
		r.Log.Info("Error during instance creation")
		return status, err
	}
	status.WsEndpoint = environment.Spec.WsEndpoint
	status.Status = hfv1.VmStatusProvisioned
	return status, nil
}

// create a managed secret which contains the ssh keys
func (r *VirtualMachineReconciler) createSecret(ctx context.Context, vm *hfv1.VirtualMachine) (status *hfv1.VirtualMachineStatus, err error) {
	status = vm.Status.DeepCopy()

	// setup annotations for the first run it doesnt exist
	if vm.GetAnnotations() == nil {
		vm.Annotations = make(map[string]string)
	}

	_, created := vm.Annotations["secret"]
	secretName := strings.Join([]string{vm.Name + "-secret"}, "-")
	existingSecret := &v1.Secret{}
	err = r.Get(ctx, types.NamespacedName{Namespace: provisionNS, Name: secretName}, existingSecret)

	if !created && errors.IsNotFound(err) {
		logrus.Info("creating new keypair")
		pubKey, privKey, err := util.GenKeyPair()

		if err != nil {
			status.Status = "Error generating ssh keypair"
			return status, err
		}

		secretData := make(map[string][]byte)
		secretData["public_key"] = []byte(pubKey)
		secretData["private_key"] = []byte(privKey)
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
			return status, err
		}

		vm.Annotations["pubKey"] = b64.StdEncoding.EncodeToString([]byte(pubKey))

	} else {
		vm.Annotations["pubKey"] = b64.StdEncoding.EncodeToString(existingSecret.Data["public_key"])
		vm.Spec.KeyPair = secretName
	}
	vm.Spec.KeyPair = secretName
	status.Status = secretCreated
	vm.Annotations["secret"] = "created"
	vm.Annotations["secretName"] = secretName
	return status, err
}

// fetch ec2 instance details to update the vm status

func (r *VirtualMachineReconciler) fetchVMDetails(ctx context.Context,
	vm *hfv1.VirtualMachine) (status *hfv1.VirtualMachineStatus, err error) {
	cloudProvider, ok := vm.Annotations["cloudProvider"]
	if !ok {
		return status, fmt.Errorf("no vm annotation for cloudProvider exists")
	}
	switch cloudProvider {
	case "aws":
		status, err = r.fetchEC2Instance(ctx, vm)
	case "digitalocean":
		status, err = r.fetchDOInstance(ctx, vm)
	case "equinix":
		status, err = r.fetchEquinixInstance(ctx, vm)
	default:
		return status, fmt.Errorf("unsupported cloud provider in fetchVMDetails")
	}
	if err != nil {
		vm.Labels["ready"] = "true"
	}
	// VM is provisioned and we have all the endpoint info we needed //
	return status, err
}

func (r *VirtualMachineReconciler) createImportKeyPair(ctx context.Context, vm *hfv1.VirtualMachine) (status *hfv1.VirtualMachineStatus, err error) {

	status = vm.Status.DeepCopy()

	b64PubKey, ok := vm.Annotations["pubKey"]
	if !ok {
		return status, fmt.Errorf("unable to find label pubKey on VM")
	}

	pubKeyByte, err := b64.StdEncoding.DecodeString(b64PubKey)
	if err != nil {
		return status, err
	}

	pubKey := strings.TrimSpace(string(pubKeyByte))

	env, err := r.fetchEnvironment(ctx, status.EnvironmentId, vm.Namespace)
	if err != nil {
		return status, err
	}

	switch env.Spec.Provider {
	case "aws":
		status, err = r.createEC2ImportKeyPair(ctx, vm, env, pubKey)
	case "digitalocean":
		status, err = r.createDOImportKeyPair(ctx, vm, env, pubKey)
	case "equinix":
		status, err = r.createEquinixImportKeyPair(ctx, vm, env, pubKey)
	default:
		err = fmt.Errorf("unsupported environment type. currently support aws and digitalocean environments only")
	}

	vm.Annotations["cloudProvider"] = env.Spec.Provider
	return status, err
}

func keyCreationDone(vm *hfv1.VirtualMachine, key string) (ok bool) {
	_, ok = vm.Labels[key]
	return ok
}
