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
	nomalerrors "errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	distriinferv1 "github.com/pipeline-operator/api/v1"
	"github.com/pipeline-operator/internal/controller/utils"
)

// PipelineReconciler reconciles a Pipeline object
type PipelineReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const pipelineNamespace = "pipeline"

func (r *PipelineReconciler) getPVkey(pipeline distriinferv1.Pipeline) (pvkey types.NamespacedName) {
	return client.ObjectKey{
		Name:      fmt.Sprintf("%s-pv", pipeline.Name),
		Namespace: pipelineNamespace, //pv是集群范围资源
	}

}
func (r *PipelineReconciler) getPVCkey(pipeline distriinferv1.Pipeline) (pvckey types.NamespacedName) {
	return client.ObjectKey{
		Name:      fmt.Sprintf("%s-pvc", pipeline.Name),
		Namespace: pipelineNamespace,
	}

}
func (r *PipelineReconciler) getDeploymentskey(pipeline distriinferv1.Pipeline) (deploymentskey []types.NamespacedName) {
	for _, step := range pipeline.Spec.Steps {
		depkey := client.ObjectKey{
			Name:      step.Model,
			Namespace: pipelineNamespace,
		}
		deploymentskey = append(deploymentskey, depkey)
	}
	return deploymentskey

}
func (r *PipelineReconciler) getServiceskey(pipeline distriinferv1.Pipeline) (serviceskey []types.NamespacedName) {
	for _, step := range pipeline.Spec.Steps {
		svckey := client.ObjectKey{
			Name:      step.Model,
			Namespace: pipelineNamespace,
		}
		serviceskey = append(serviceskey, svckey)
	}
	return serviceskey

}

// 处理pv pvc
func (r *PipelineReconciler) syncStorageVolume(pipeline *distriinferv1.Pipeline, ctx context.Context) error {
	logger := log.FromContext(ctx)
	//查找同名pv-->exist,not found,
	fmt.Printf("处理pv...\n")
	pv := &corev1.PersistentVolume{}
	pvkey := r.getPVkey(*pipeline)

	pvc := &corev1.PersistentVolumeClaim{}
	pvckey := r.getPVCkey(*pipeline)

	newpv, newpvc := utils.NewStorageVolume(pipeline)

	if err := controllerutil.SetControllerReference(pipeline, newpv, r.Scheme); err != nil {
		logger.Error(err, "SetControllerReference failed!")
		return err
	}
	if err := controllerutil.SetControllerReference(pipeline, newpvc, r.Scheme); err != nil {
		logger.Error(err, "SetControllerReference failed!")
		return err
	}

	if err1 := r.Get(ctx, pvkey, pv); err1 != nil {
		if k8serrors.IsNotFound(err1) { //not found->create
			fmt.Printf("pvIsNotFound\n")
			if err := r.Create(ctx, newpv, &client.CreateOptions{}); err != nil {
				logger.Error(err, "create pv failed!")
				return err
			}
			fmt.Printf("CreateNewpv success\n")
			fmt.Printf("newpv.DetailPhase.PVPhase: %+v\n", newpv.Status.Phase)
			//成功创建，更新status
			pipeline.Status.DetailPhase.PVPhase = string(newpv.Status.Phase)

		}
	} else { //exist pv
		// 获取到了 PV 对象
		fmt.Printf("find PV success\n")
		fmt.Printf("PVStatus: %+v\n", pv.Status.Phase)
		//fmt.Printf("pc.DetailPhase.PVCPhase: %+v\n", pvc.Status.Phase)
		//更新status
		pipeline.Status.DetailPhase.PVPhase = string(pv.Status.Phase)
		// pvStatus := &corev1.PersistentVolumeStatus{}
		// 判断 PV 的状态是否为 Released，是的话先删除再重建
		if pv.Status.Phase == corev1.VolumeReleased {
			fmt.Printf("pv.Status: Released!\n")
			if err := r.Delete(ctx, newpv, &client.DeleteOptions{}); err != nil {
				logger.Error(err, "Delete pv failed!")
				// fmt.Printf("Delete pv failed!\n")
				return err
			}

			if err := r.Create(ctx, newpv, &client.CreateOptions{}); err != nil {
				logger.Error(err, "create pv failed!")
				// fmt.Printf("create pv failed!\n")
				return err
			}
			fmt.Printf("newpv.Status.Phase: %+v\n", newpv.Status.Phase)
			//成功创建，更新status
			pipeline.Status.DetailPhase.PVPhase = string(newpv.Status.Phase)
		}

	}
	//存在pv，开始处理pvc
	if err := r.Get(ctx, pvckey, pvc); err != nil { //pvc
		fmt.Printf("pvcIsNotFound\n")
		if k8serrors.IsNotFound(err) { //not found->create
			fmt.Printf("CreateNewpvc\n")
			if err := r.Create(ctx, newpvc, &client.CreateOptions{}); err != nil {
				logger.Error(err, "create pvc failed!")
				return err
			}
			fmt.Printf("newpvc.Status.Phase: %+v\n", newpvc.Status.Phase)
			//成功创建，更新status
			pipeline.Status.DetailPhase.PVCPhase = string(newpvc.Status.Phase)
		}
	} else { //exist pv pvc
		fmt.Printf("find PVC success\n")

		//存在，更新status
		fmt.Printf("PVStatus: %+v\n", pv.Status.Phase)
		fmt.Printf("PVCStatus: %+v\n", pvc.Status.Phase)
		//更新status
		pipeline.Status.DetailPhase.PVPhase = string(pv.Status.Phase)
		pipeline.Status.DetailPhase.PVCPhase = string(pvc.Status.Phase)

	}
	return nil
}

// 处理depolyment
func (r *PipelineReconciler) syncDeployments(pipeline *distriinferv1.Pipeline, ctx context.Context) error {
	logger := log.FromContext(ctx)
	fmt.Printf("处理deployments ...\n")
	newDeps := *utils.NewDeployments(pipeline)
	createDepFlag := 0

	//key
	deploymentsKey := r.getDeploymentskey(*pipeline)
	// fmt.Printf("pipeline.Spec.Steps: %d\n", len(pipeline.Spec.Steps)) //3
	// fmt.Printf("length of newDeps: %d\n", len(newDeps)) //3

	for i := 0; i < len(pipeline.Spec.Steps); i++ {
		depkey := deploymentsKey[i]
		// print
		fmt.Printf("deployment %d key: %+v\n", i, depkey)
		deployment := &appv1.Deployment{}
		// deploys = append(deploys, *deployment)
		if err := r.Get(ctx, depkey, deployment); err != nil {
			if k8serrors.IsNotFound(err) { //not found->create
				//存在一个deployment没找到就跳出重创建
				createDepFlag = 1
				fmt.Printf("exist deployment IsNotFound\n")
				break

			}
		} else { //exist: delete,update
			fmt.Printf("find Deployment %d success，deployment Replicas：%d\n", i, *deployment.Spec.Replicas)
			fmt.Printf("deploymentStatus: %s\n", fmt.Sprintf("%d/%d", deployment.Status.ReadyReplicas, deployment.Status.Replicas))

			if areDeploymentsEqual(newDeps[i], *deployment) {
				fmt.Printf("不需要update，deploymentStatus: %s\n", fmt.Sprintf("%d/%d", deployment.Status.ReadyReplicas, deployment.Status.Replicas))

			} else {
				fmt.Printf("newDeps[i].Spec 和 deployment.Spec不相同\n")
				// fmt.Printf("newDeps[i].Spec: %+v\n", newDeps[i].Spec)
				// fmt.Printf("deployment.Spec: %+v\n", deployment.Spec)

				fmt.Printf("更新...\n")
				// 更新 Deployment 的规格
				deployment.Spec = newDeps[i].Spec
				// deployment = &newDeps[i]
				if err := r.Update(ctx, deployment); err != nil {
					return err
				}
			}
			// 存在，更新status
			pipeline.Status.DetailPhase.StepsPhase[i].DeploymentPhase = fmt.Sprintf("%d/%d", deployment.Status.ReadyReplicas, deployment.Status.Replicas)

		}
	}

	if createDepFlag != 0 {
		for i := 0; i < len(pipeline.Spec.Steps); i++ {
			if err := controllerutil.SetControllerReference(pipeline, &newDeps[i], r.Scheme); err != nil {
				logger.Error(err, "SetControllerReference failed!")
				return err
			}
			if err := r.Create(ctx, &newDeps[i], &client.CreateOptions{}); err != nil {
				if !k8serrors.IsAlreadyExists(err) {
					logger.Error(err, "create deployment failed!")
					return err
				} else {
					fmt.Printf("deployment %d IsAlreadyExists \n", i)
				}

			} else {
				fmt.Printf("createNewDeployment success\n")
			}
			fmt.Printf("newDeps deploymentStatus: %s\n", string(newDeps[i].Status.ReadyReplicas)+"/"+string(newDeps[i].Status.Replicas))
			//成功创建，更新status
			pipeline.Status.DetailPhase.StepsPhase[i].DeploymentPhase = string(newDeps[i].Status.ReadyReplicas) + "/" + string(newDeps[i].Status.Replicas)

		}
		return nil
	}

	//delete
	if err := r.delDeployments(pipeline, newDeps, ctx); err != nil {
		return err
	}
	return nil

}

// 比较deployment是否需要更新
func areDeploymentsEqual(deployment1, deployment2 appv1.Deployment) bool {
	fmt.Printf("比较deployment:\n")

	// 比较 Selector
	if !reflect.DeepEqual(deployment1.Spec.Selector, deployment2.Spec.Selector) {
		fmt.Print(".Spec.Selector不同\n")
		return false
	}

	// 比较 Replicas
	if !reflect.DeepEqual(deployment1.Spec.Replicas, deployment2.Spec.Replicas) {
		fmt.Print(".Spec.Replicas不同\n")
		return false
	}

	// 比较 Template
	if !reflect.DeepEqual(deployment1.Spec.Template.Spec.Affinity, deployment2.Spec.Template.Spec.Affinity) {
		fmt.Print(".Spec.Template.Spec.Affinity 不同\n")
		return false
	}
	if !reflect.DeepEqual(deployment1.Spec.Template.Spec.Volumes, deployment2.Spec.Template.Spec.Volumes) {
		fmt.Print(".Spec.Template.Spec.Volumes 不同\n")
		return false
	}
	if !reflect.DeepEqual(deployment1.Spec.Template.Spec.Containers[0].Image, deployment2.Spec.Template.Spec.Containers[0].Image) {
		fmt.Print(".Spec.Template.Spec.Containers[0].Image 不同\n")
		return false
	}
	if !reflect.DeepEqual(deployment1.Spec.Template.Spec.Containers[0].ImagePullPolicy, deployment2.Spec.Template.Spec.Containers[0].ImagePullPolicy) {
		fmt.Print(".Spec.Template.Spec.Containers[0].ImagePullPolicy 不同\n")
		return false
	}
	if !reflect.DeepEqual(deployment1.Spec.Template.Spec.Containers[0].Name, deployment2.Spec.Template.Spec.Containers[0].Name) {
		fmt.Print(".Spec.Template.Spec.Containers[0].Name 不同\n")
		return false
	}
	if !reflect.DeepEqual(deployment1.Spec.Template.Spec.Containers[0].Ports, deployment2.Spec.Template.Spec.Containers[0].Ports) {
		fmt.Print(".Spec.Template.Spec.Containers[0].Ports 不同\n")
		return false
	}
	if !reflect.DeepEqual(deployment1.Spec.Template.Spec.Containers[0].VolumeMounts, deployment2.Spec.Template.Spec.Containers[0].VolumeMounts) {
		fmt.Print(".Spec.Template.Spec.Containers[0].VolumeMounts 不同\n")
		return false
	}
	if !reflect.DeepEqual(deployment1.Spec.Template.Spec.Containers[0].Env, deployment2.Spec.Template.Spec.Containers[0].Env) {
		fmt.Print(".Spec.Template.Spec.Containers[0].Env 不同\n")
		return false
	}

	// 如果有其他需要比较的属性，可以在这里继续添加

	return true
}

// 删除deployment
func (r *PipelineReconciler) delDeployments(pipeline *distriinferv1.Pipeline, newDeps []appv1.Deployment, ctx context.Context) error {
	//筛选出当前pipeline的所有deployment
	existDepList, err := r.selectDeployment(pipeline, ctx)
	if err != nil {
		return err
	}
	if len(pipeline.Spec.Steps) < len(existDepList.Items) { //more than spec,need to delete
		//delete
		r.deleteBadDeployment(newDeps, existDepList, ctx)
	}
	return nil
}

// 删除多于预期部分的deployment
func (r *PipelineReconciler) deleteBadDeployment(newDeps []appv1.Deployment, existDepList *appv1.DeploymentList, ctx context.Context) error {
	//创建映射，高效查找
	newDepsMap := make(map[string]appv1.Deployment)
	for _, dep := range newDeps {
		newDepsMap[dep.Name] = dep
	}
	//找到要删除的放入toDelete
	var toDelete []appv1.Deployment
	for _, dep := range existDepList.Items {
		if _, ok := newDepsMap[dep.Name]; !ok {
			toDelete = append(toDelete, dep)
		}
	}
	//删除
	for _, toDel := range toDelete {
		if err := r.Delete(ctx, &toDel); err != nil {
			return err
		}
	}
	return nil
}

// 筛选出当前pipeline的所有deployment
func (r *PipelineReconciler) selectDeployment(pipeline *distriinferv1.Pipeline, ctx context.Context) (*appv1.DeploymentList, error) {
	// Create a new DeploymentList to store the list of Deployments
	existDepList := &appv1.DeploymentList{}
	// Define the label selector to filter Deployments
	labelSelector := labels.SelectorFromSet(labels.Set{pipelineNamespace: pipeline.Name})
	// Create a ListOptions with the label selector
	listOptions := &client.ListOptions{
		Namespace:     pipelineNamespace,
		LabelSelector: labelSelector,
	}
	// fmt.Printf("listOptions为 Namespace: %v,labelSelector: %v\n", pipelineNamespace, labelSelector)
	// List Deployments with the specified label
	if err := r.Client.List(context.TODO(), existDepList, listOptions); err != nil {
		return nil, err
	}
	// fmt.Printf("existDepList: %+v\n", existDepList.Items)
	return existDepList, nil

}

// 处理service
func (r *PipelineReconciler) syncServices(pipeline *distriinferv1.Pipeline, ctx context.Context) error {
	logger := log.FromContext(ctx)
	fmt.Printf("处理services ...\n")

	newServices := *utils.NewServices(pipeline)
	createSerFlag := 0
	//key
	serviceskey := r.getServiceskey(*pipeline)
	for i := 0; i < len(pipeline.Spec.Steps); i++ {
		servicekey := serviceskey[i]
		service := &corev1.Service{}
		if err := r.Get(ctx, servicekey, service); err != nil {
			if k8serrors.IsNotFound(err) { //not found->create
				//存在service没找到就重新创建
				createSerFlag = 1
				break

			}
		} else { //exist: delete,update
			fmt.Printf("find Service %d success\n", i)

			if areServicesEqual(newServices[i], *service) {
				fmt.Printf("不需要update\n")

			} else {
				fmt.Printf("newServices[i].Spec 和 service.Spec不相同\n")
				// fmt.Printf("newServices[i].Spec: %+v\n", newServices[i].Spec)
				// fmt.Printf("service.Spec: %+v\n", service.Spec)

				fmt.Printf("更新...\n")
				// 更新 service 的规格
				service.Spec = newServices[i].Spec
				// service = &newservice[i]
				if err := r.Update(ctx, service); err != nil {
					return err
				}
			}
		}

	}
	if createSerFlag != 0 {
		for i := 0; i < len(pipeline.Spec.Steps); i++ {
			if err := controllerutil.SetControllerReference(pipeline, &newServices[i], r.Scheme); err != nil {
				logger.Error(err, "SetControllerReference services failed!")
				return err
			}
			if err := r.Create(ctx, &newServices[i], &client.CreateOptions{}); err != nil {
				if !k8serrors.IsAlreadyExists(err) {
					logger.Error(err, "create service failed!")
					return err
				} else {
					fmt.Printf("service %d IsAlreadyExists \n", i)
				}

			} else {
				fmt.Printf("createNewService success\n")
			}
		}
		return nil
	}

	// delete
	if err := r.delServices(pipeline, newServices, ctx); err != nil {
		return err
	}
	return nil

}

// 比较service是否需要更新
func areServicesEqual(service1, service2 corev1.Service) bool {
	// 比较 Selector
	if !reflect.DeepEqual(service1.Spec.Selector, service2.Spec.Selector) {
		return false
	}

	// 比较 Ports
	if !reflect.DeepEqual(service1.Spec.Ports, service2.Spec.Ports) {
		return false
	}

	// 如果有其他需要比较的属性，可以在这里继续添加

	return true
}

// 删除service
func (r *PipelineReconciler) delServices(pipeline *distriinferv1.Pipeline, newServices []corev1.Service, ctx context.Context) error {
	//筛选出当前pipeline的所有service
	existSVCList, err := r.selectService(pipeline, ctx)
	if err != nil {
		return err
	}
	if len(pipeline.Spec.Steps) < len(existSVCList.Items) { //more than spec,need to delete
		//delete
		fmt.Printf("delete SVC\n")
		r.deleteBadService(newServices, existSVCList, ctx)
	}
	return nil
}

// 筛选出当前pipeline的所有service
func (r *PipelineReconciler) selectService(pipeline *distriinferv1.Pipeline, ctx context.Context) (*corev1.ServiceList, error) {
	// Create a new ServiceList to store the list of Services
	existSVCList := &corev1.ServiceList{}
	// Define the label selector to filter Services
	labelSelector := labels.SelectorFromSet(labels.Set{pipelineNamespace: pipeline.Name})
	// Create a ListOptions with the label selector
	listOptions := &client.ListOptions{
		Namespace:     pipelineNamespace,
		LabelSelector: labelSelector,
	}
	// fmt.Printf("listOptions为 Namespace: %v,labelSelector: %v\n", pipelineNamespace, labelSelector)
	// List Services with the specified label
	if err := r.Client.List(context.TODO(), existSVCList, listOptions); err != nil {
		fmt.Printf("列出服务时发生错误: %v\n", err)
		return nil, err
	}
	// fmt.Printf("existSVCList: %+v\n", existSVCList.Items)
	return existSVCList, nil
}

// 删除多于预期部分的Service
func (r *PipelineReconciler) deleteBadService(newServices []corev1.Service, existSVCList *corev1.ServiceList, ctx context.Context) error {
	// 创建映射，高效查找
	newSVCMap := make(map[string]corev1.Service)
	for _, svc := range newServices {
		newSVCMap[svc.Name] = svc
	}
	// 找到要删除的放入toDelete
	var toDelete []corev1.Service
	for _, svc := range existSVCList.Items {
		if _, ok := newSVCMap[svc.Name]; !ok {
			toDelete = append(toDelete, svc)
		}
	}
	// 删除
	for _, toDel := range toDelete {
		if err := r.Delete(ctx, &toDel); err != nil {
			return err
		}
	}
	return nil
}

// +kubebuilder:rbac:groups=distri-infer.ndsl.cn,resources=pipelines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=distri-infer.ndsl.cn,resources=pipelines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=distri-infer.ndsl.cn,resources=pipelines/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=*,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=*,resources=persistentvolumes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=*,resources=persistentvolumes/status,verbs=get
// +kubebuilder:rbac:groups=*,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=*,resources=persistentvolumeclaims/status,verbs=get
func (r *PipelineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// logger := log.FromContext(ctx)
	fmt.Printf("处理Reconcile...\n")

	//获取pipeline
	pipeline := &distriinferv1.Pipeline{}
	if err := r.Get(ctx, req.NamespacedName, pipeline); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if len(pipeline.Status.DetailPhase.StepsPhase) != len(pipeline.Spec.Steps) {
		stepsPhase := make([]distriinferv1.StepPhase, len(pipeline.Spec.Steps))
		pipeline.Status.DetailPhase.StepsPhase = stepsPhase

	}
	pipeline.Status.StepsLength = len(pipeline.Spec.Steps)

	//处理pv pvc
	if err := r.syncStorageVolume(pipeline, ctx); err != nil {
		return ctrl.Result{}, err
	}

	//处理depolyment
	if err := r.syncDeployments(pipeline, ctx); err != nil {
		return ctrl.Result{}, err
	}

	//处理service
	if err := r.syncServices(pipeline, ctx); err != nil {
		return ctrl.Result{}, err
	}

	//上述都成功创建，更新status
	return r.updateStatus(pipeline, ctx)
}

// update status
func (r *PipelineReconciler) updateStatus(pipeline *distriinferv1.Pipeline, ctx context.Context) (ctrl.Result, error) {
	isAvailable, err := r.checkPipelineAvailable(*pipeline)
	if err != nil {
		fmt.Printf("Error checking pipeline availability: %v\n", err)
	}
	if isAvailable {
		fmt.Println("Pipeline is available.")
		pipeline.Status.Phase = string(distriinferv1.PipelineAvailable) // available  unavailable
		pipeline.Status.DetailPhase.LastTransitionTime = metav1.Now()
		//update status
		if err := r.Status().Update(ctx, pipeline); err != nil {
			return ctrl.Result{}, err
		}

	} else {
		fmt.Println("Pipeline is not available.")
		pipeline.Status.Phase = string(distriinferv1.PipelineUnAvailable) // available  unavailable
		pipeline.Status.DetailPhase.LastTransitionTime = metav1.Now()
		//update status
		// if err := r.Status().Update(ctx, pipeline); err != nil {
		// 	return ctrl.Result{}, err
		// }
	}

	fmt.Printf("r.Status：%+v\n", pipeline.Status)
	return ctrl.Result{Requeue: false}, nil
}

// check Pipeline is Available
func (r *PipelineReconciler) checkPipelineAvailable(pipeline distriinferv1.Pipeline) (bool, error) {
	if pipeline.Status.DetailPhase.PVCPhase == "Bound" && pipeline.Status.DetailPhase.PVPhase == "Bound" {
		for i := 0; i < len(pipeline.Spec.Steps); i++ {
			if err := checkStepAvailability(pipeline, i); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	return false, nil
}

// check Pipeline'steps is Available
func checkStepAvailability(pipeline distriinferv1.Pipeline, stepIndex int) error {
	deploymentPhase := pipeline.Status.DetailPhase.StepsPhase[stepIndex].DeploymentPhase
	results := strings.Split(deploymentPhase, "/")

	if len(results) != 2 {
		return nomalerrors.New("invalid format for DeploymentPhase")
	}

	readyReplicasStr := results[0]
	replicasStr := results[1]

	readyReplicas, err1 := strconv.Atoi(readyReplicasStr)
	replicas, err2 := strconv.Atoi(replicasStr)

	if err1 != nil || err2 != nil {
		return nomalerrors.New("error converting strings to integers")
	}

	if readyReplicas != replicas {
		return nomalerrors.New("ready replicas not equal to total replicas")
	}

	return nil
}

// In order to avoid invalid reconcile, we just focus on these two cases:
// 1. when the Pipeline cr is deleted.
// 2. when the spec of Pipeline cr is updated.
func (r *PipelineReconciler) onPipelineUpdate(updateEvent event.UpdateEvent) bool {
	newObj := updateEvent.ObjectNew.(*distriinferv1.Pipeline)
	oldObj := updateEvent.ObjectOld.(*distriinferv1.Pipeline)

	if !newObj.DeletionTimestamp.IsZero() {
		return true
	}

	return !reflect.DeepEqual(newObj.Spec, oldObj.Spec)
}

// SetupWithManager sets up the controller with the Manager.
func (r *PipelineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// For(&distriinferv1.Pipeline{}).
		For(&distriinferv1.Pipeline{},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: r.onPipelineUpdate,
			})).
		Owns(&corev1.PersistentVolume{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&appv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
