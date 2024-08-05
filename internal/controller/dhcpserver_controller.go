/*
Copyright 2024.

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

package controller

import (
	"context"
	"time"

	"github.com/lootbot-cloud/k8s-dhcp-cluster/dhcp"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dhcpv1 "github.com/lootbot-cloud/k8s-dhcp-cluster/api/v1"
)

// DHCPServerReconciler reconciles a DHCPServer object
type DHCPServerReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	DHCPServer *dhcp.Server
	cache      *ObjectsCache
	log        dhcp.RLogger
}

func NewDHCPServerReconciler(c client.Client, scheme *runtime.Scheme, cache *ObjectsCache, log dhcp.RLogger) *DHCPServerReconciler {

	return &DHCPServerReconciler{
		Client: c,
		Scheme: scheme,
		cache:  cache,
		log:    log,
	}
}

// +kubebuilder:rbac:groups=dhcp.lootbot.cloud,resources=dhcpservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dhcp.lootbot.cloud,resources=dhcpservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dhcp.lootbot.cloud,resources=dhcpservers/finalizers,verbs=update

func (r *DHCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.cache.ListensLock.Lock()
	defer r.cache.ListensLock.Unlock()

	l := log.FromContext(ctx)
	sv := &dhcpv1.DHCPServer{}
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, sv)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object Deleted
			l.Info("deleted listen")
			err = r.DHCPServer.DeleteListen(req.Name)
			delete(r.cache.knownListens, req.Name)
			return ctrl.Result{Requeue: false}, err
		}
		l.Error(err, "failed to get Listen")
		// Error reading the object - requeue the request.
		// possibly due to network issues
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: time.Second * 10,
		}, err
	}

	// Check if our listen is already known
	listenObj := r.cache.knownListens[req.Name]
	if listenObj != nil {
		l.Info("Listen is already known")
		err = r.DHCPServer.DeleteListen(req.Name)
		if err != nil {
			return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, err
		}
	}
	// Add new listen to server
	l.Info("New Listen", "obj", sv)
	err = r.DHCPServer.AddListen(sv.ToListen())
	if err != nil {
		l.Error(err, "Failed to add listen")
		sv.Status.ErrorMessage = err.Error()
	} else {
		// DHCP is now listening on the new listen address
		r.cache.knownListens[req.Name] = sv
		// Update the status of the listen
		// TODO: Update the status of the listen
	}
	return ctrl.Result{}, err
}

// loadAndAddSubnets loads all subnets and adds them to the DHCP server.
func (r *DHCPServerReconciler) loadAndAddSubnets(ctx context.Context) error {
	r.cache.ListensLock.Lock()
	defer r.cache.ListensLock.Unlock()
	r.log.Infof("Loading all subnets")
	subnetList := &dhcpv1.DHCPSubnetList{}
	err := r.Client.List(ctx, subnetList)
	if err != nil {
		return err
	}
	for _, sn := range subnetList.Items {
		err = r.DHCPServer.AddSubnet(sn.ToSubnet())
		if err != nil {
			return err
		}
	}
	return nil
}

// loadAndAddListens loads all listeners and adds them to the DHCP server.
func (r *DHCPServerReconciler) loadAndAddListens(ctx context.Context) error {
	r.cache.ListensLock.Lock()
	defer r.cache.ListensLock.Unlock()
	l := log.FromContext(ctx)
	r.log.Infof("Loading all listeners")
	listenList := &dhcpv1.DHCPServerList{}
	err := r.Client.List(ctx, listenList)
	if err != nil {
		return err
	}
	for _, ln := range listenList.Items {
		err = r.DHCPServer.AddListen(ln.ToListen())
		if err != nil {
			l.Error(err, "Failed to add listen")
			ln.Status.ErrorMessage = err.Error()
		} else {
			// DHCP is now listening on the new listen address
			//TODO: might be segafulting here
			r.cache.knownListens[ln.Name] = &ln
			// Update the status of the listen
			// TODO: Update the status of the listen
		}
	}
	return nil
}

// Initialize the DHCP Server
// This function is called when the controller is started
// It loads all subnets and listens from the k8s API
// and adds them to the DHCP Server
// Assumes the CRDs are cluster scoped
func (r *DHCPServerReconciler) Initialize(ctx context.Context) error {
	r.log.Infof("Loading all subnets")
	// Load all subnets
	err := r.loadAndAddSubnets(ctx)
	if err != nil {
		return err
	}
	r.log.Infof("Loading all Liseners")
	// Load all listens
	err = r.loadAndAddListens(ctx)
	if err != nil {
		return err
	}
	//Load all leases
	r.log.Infof("Loading all leases")
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DHCPServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dhcpv1.DHCPServer{}).
		Complete(r)
}
