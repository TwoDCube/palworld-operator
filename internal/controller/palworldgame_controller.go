/*
Copyright 2026.

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
	"os"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
	"github.com/twodcube/palworld-operator/internal/resources"
)

const (
	defaultServerImageFallback = "quay.io/twodcube/palworld-server:latest"
	defaultOperatorNamespace   = "palworld-operator-system"
	requeueInterval            = 60 * time.Second
)

// PalworldGameReconciler reconciles a PalworldGame object.
type PalworldGameReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// Runtime configuration, populated in SetupWithManager from the environment.
	OperatorNamespace  string
	OperatorImage      string
	DefaultServerImage string
	SteamInfoEndpoint  string

	// Capability detection (probed once, lazily).
	capOnce           sync.Once
	hasRoute          bool
	hasServiceMonitor bool
}

// +kubebuilder:rbac:groups=palworld.twodcube.io,resources=palworldgames,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=palworld.twodcube.io,resources=palworldgames/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=palworld.twodcube.io,resources=palworldgames/finalizers,verbs=update
// +kubebuilder:rbac:groups=palworld.twodcube.io,resources=palworldbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps;secrets;serviceaccounts;persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives a PalworldGame toward its desired state.
func (r *PalworldGameReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var game palworldv1alpha1.PalworldGame
	if err := r.Get(ctx, req.NamespacedName, &game); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	r.probeCapabilities(ctx)

	// Handle deletion via finalizer.
	if !game.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &game)
	}

	// Ensure the finalizer is present so we can run cleanup on deletion.
	if !containsString(game.Finalizers, palworldv1alpha1.GameFinalizer) {
		game.Finalizers = append(game.Finalizers, palworldv1alpha1.GameFinalizer)
		if err := r.Update(ctx, &game); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Reconcile all owned resources.
	if err := r.reconcileResources(ctx, &game); err != nil {
		r.setProgressing(&game, "ReconcileError", err.Error())
		_ = r.updateStatus(ctx, &game)
		return ctrl.Result{}, err
	}

	// Refresh live/observed status (best-effort).
	r.reconcileObservedStatus(ctx, &game)

	// Version update detection + rollout.
	updateRes, err := r.reconcileUpdates(ctx, &game)
	if err != nil {
		logger.Error(err, "update reconciliation failed")
	}

	// Scheduled backups.
	if err := r.reconcileScheduledBackups(ctx, &game); err != nil {
		logger.Error(err, "scheduled backup reconciliation failed")
	}

	if err := r.updateStatus(ctx, &game); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	requeue := requeueInterval
	if updateRes.RequeueAfter > 0 && updateRes.RequeueAfter < requeue {
		requeue = updateRes.RequeueAfter
	}
	return ctrl.Result{RequeueAfter: requeue}, nil
}

// probeCapabilities detects optional cluster APIs (Routes, ServiceMonitor) once.
func (r *PalworldGameReconciler) probeCapabilities(ctx context.Context) {
	r.capOnce.Do(func() {
		r.hasRoute = hasAPI(ctx, r.Client, resources.RouteGVK)
		r.hasServiceMonitor = hasAPI(ctx, r.Client, resources.ServiceMonitorGVK)
		log.FromContext(ctx).Info("probed cluster capabilities",
			"routes", r.hasRoute, "serviceMonitor", r.hasServiceMonitor)
	})
}

func (r *PalworldGameReconciler) buildParams() resources.BuildParams {
	serverImage := r.DefaultServerImage
	if serverImage == "" {
		serverImage = defaultServerImageFallback
	}
	return resources.BuildParams{
		OpenShift:          r.hasRoute,
		OperatorImage:      r.OperatorImage,
		DefaultServerImage: serverImage,
	}
}

// reconcileResources ensures every child object exists and matches the spec.
func (r *PalworldGameReconciler) reconcileResources(ctx context.Context, game *palworldv1alpha1.PalworldGame) error {
	if err := r.ensureCredentials(ctx, game); err != nil {
		return err
	}
	if err := r.ensureServiceAccount(ctx, game); err != nil {
		return err
	}
	if err := r.ensureConfigMap(ctx, game); err != nil {
		return err
	}
	if err := r.ensureServices(ctx, game); err != nil {
		return err
	}
	if err := r.ensurePodDisruptionBudget(ctx, game); err != nil {
		return err
	}
	if err := r.ensureNetworkPolicy(ctx, game); err != nil {
		return err
	}
	if err := r.ensureMonitoring(ctx, game); err != nil {
		return err
	}
	if err := r.ensureRoute(ctx, game); err != nil {
		return err
	}
	if err := r.ensureStatefulSet(ctx, game); err != nil {
		return err
	}
	return nil
}

func (r *PalworldGameReconciler) ensureCredentials(ctx context.Context, game *palworldv1alpha1.PalworldGame) error {
	if game.Spec.Credentials.SecretName != "" {
		// User-managed secret; nothing to create.
		game.Status.CredentialsSecret = game.Spec.Credentials.SecretName
		return nil
	}
	name := resources.GeneratedSecretName(game)
	var secret corev1.Secret
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: game.Namespace}, &secret)
	if err == nil {
		game.Status.CredentialsSecret = name
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	pw, err := resources.GeneratePassword(24)
	if err != nil {
		return err
	}
	desired := resources.DesiredGeneratedSecret(game, pw)
	if err := controllerutil.SetControllerReference(game, desired, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	game.Status.CredentialsSecret = name
	r.Recorder.Event(game, corev1.EventTypeNormal, "CredentialsGenerated",
		"Generated admin credentials secret "+name)
	return nil
}

func (r *PalworldGameReconciler) ensureServiceAccount(ctx context.Context, game *palworldv1alpha1.PalworldGame) error {
	if game.Spec.ServiceAccountName != "" {
		return nil // externally managed
	}
	desired := resources.DesiredServiceAccount(game)
	sa := &corev1.ServiceAccount{}
	sa.Name = desired.Name
	sa.Namespace = desired.Namespace
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
		sa.Labels = desired.Labels
		return controllerutil.SetControllerReference(game, sa, r.Scheme)
	})
	return err
}

func (r *PalworldGameReconciler) ensureConfigMap(ctx context.Context, game *palworldv1alpha1.PalworldGame) error {
	desired, err := resources.DesiredConfigMap(game)
	if err != nil {
		return err
	}
	cm := &corev1.ConfigMap{}
	cm.Name = desired.Name
	cm.Namespace = desired.Namespace
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Labels = desired.Labels
		cm.Data = desired.Data
		return controllerutil.SetControllerReference(game, cm, r.Scheme)
	})
	return err
}

func (r *PalworldGameReconciler) ensureServices(ctx context.Context, game *palworldv1alpha1.PalworldGame) error {
	svcs := []*corev1.Service{
		resources.DesiredHeadlessService(game),
		resources.DesiredGameService(game),
		resources.DesiredAdminService(game),
	}
	if game.Spec.Monitoring.MetricsExporter && r.OperatorImage != "" {
		svcs = append(svcs, resources.DesiredMetricsService(game))
	}
	for _, svc := range svcs {
		if err := reconcileService(ctx, r.Client, game, r.Scheme, svc); err != nil {
			return err
		}
	}
	return nil
}

func (r *PalworldGameReconciler) ensurePodDisruptionBudget(ctx context.Context, game *palworldv1alpha1.PalworldGame) error {
	enabled := true
	if game.Spec.PodDisruptionBudget != nil {
		enabled = game.Spec.PodDisruptionBudget.Enabled
	}
	name := resources.PDBName(game)
	if !enabled {
		return r.deleteIfExists(ctx, &policyv1.PodDisruptionBudget{}, name, game.Namespace)
	}
	desired := resources.DesiredPodDisruptionBudget(game)
	pdb := &policyv1.PodDisruptionBudget{}
	pdb.Name = desired.Name
	pdb.Namespace = desired.Namespace
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, pdb, func() error {
		pdb.Labels = desired.Labels
		pdb.Spec.MinAvailable = desired.Spec.MinAvailable
		pdb.Spec.Selector = desired.Spec.Selector
		return controllerutil.SetControllerReference(game, pdb, r.Scheme)
	})
	// PDB selector is immutable on some versions; recreate on invalid update.
	if err != nil && apierrors.IsInvalid(err) {
		if delErr := r.Delete(ctx, pdb); delErr != nil {
			return delErr
		}
		return err
	}
	return err
}

func (r *PalworldGameReconciler) ensureNetworkPolicy(ctx context.Context, game *palworldv1alpha1.PalworldGame) error {
	desired := resources.DesiredNetworkPolicy(game, r.OperatorNamespace)
	np := &networkingv1.NetworkPolicy{}
	np.Name = desired.Name
	np.Namespace = desired.Namespace
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, np, func() error {
		np.Labels = desired.Labels
		np.Spec = desired.Spec
		return controllerutil.SetControllerReference(game, np, r.Scheme)
	})
	return err
}

func (r *PalworldGameReconciler) ensureMonitoring(ctx context.Context, game *palworldv1alpha1.PalworldGame) error {
	if !game.Spec.Monitoring.ServiceMonitor || !r.hasServiceMonitor {
		return nil
	}
	desired := resources.DesiredServiceMonitor(game)
	return reconcileUnstructured(ctx, r.Client, game, r.Scheme, desired)
}

func (r *PalworldGameReconciler) ensureRoute(ctx context.Context, game *palworldv1alpha1.PalworldGame) error {
	if !game.Spec.Networking.RESTAPI.Route || !r.hasRoute {
		return nil
	}
	desired := resources.DesiredRoute(game)
	return reconcileUnstructured(ctx, r.Client, game, r.Scheme, desired)
}

// deleteIfExists removes an owned object if present, ignoring NotFound.
func (r *PalworldGameReconciler) deleteIfExists(ctx context.Context, obj client.Object, name, namespace string) error {
	obj.SetName(name)
	obj.SetNamespace(namespace)
	err := r.Delete(ctx, obj)
	return client.IgnoreNotFound(err)
}

// SetupWithManager wires the controller into the manager.
func (r *PalworldGameReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.OperatorNamespace == "" {
		r.OperatorNamespace = envOr("OPERATOR_NAMESPACE", defaultOperatorNamespace)
	}
	if r.OperatorImage == "" {
		r.OperatorImage = os.Getenv("OPERATOR_IMAGE")
	}
	if r.DefaultServerImage == "" {
		r.DefaultServerImage = envOr("DEFAULT_SERVER_IMAGE", defaultServerImageFallback)
	}
	if r.SteamInfoEndpoint == "" {
		r.SteamInfoEndpoint = os.Getenv("STEAM_INFO_ENDPOINT")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&palworldv1alpha1.PalworldGame{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&palworldv1alpha1.PalworldBackup{}).
		Complete(r)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// listSelector returns a label selector for the game's pods (used by status).
func gameSelector(game *palworldv1alpha1.PalworldGame) labels.Selector {
	return labels.SelectorFromSet(resources.SelectorLabels(game))
}
