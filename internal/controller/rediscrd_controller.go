/*
Copyright 2023.

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
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	myappv1 "test.com/rediscrd/api/v1"
)

// RedisCRDReconciler reconciles a RedisCRD object
type RedisCRDReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=myapp.test.com,resources=rediscrds,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=myapp.test.com,resources=rediscrds/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=myapp.test.com,resources=rediscrds/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the RedisCRD object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.4/pkg/reconcile
// 这个函数干了什么
// 获取当前 Kubernetes 资源的状态和期望状态；
// 根据状态差异执行一系列的操作，例如创建、更新或删除 Kubernetes 资源；
// 更新 Kubernetes 资源的状态，以反映执行操作后的最新状态；
// 返回一个结果，以指示该方法的执行结果。
// 这个方法什么时候被执行？
// 首次部署资源: 当 Kubernetes 集群中没有指定资源时，自定义控制器会创建对应的资源，并调用 Reconcile() 方法来更新资源的状态
// 资源状态发生变化: 当 Kubernetes 资源的状态发生变化时，例如某个资源被创建、更新或删除时，自定义控制器会接收到相应的事件，并调用 Reconcile() 方法来更新资源的状态。
// 定期检查资源：自定义控制器可以定期检查 Kubernetes 资源的状态，
// 并调用 Reconcile() 方法来更新资源的状态。在这种情况下，自定义控制器需要通过 Kubernetes 的 CronJob 或者其他方式来触发定期检查。
// 如果更新操作成功，Kubernetes API Server 会触发相应的事件通知自定义控制器，
// 通知的内容包括资源名称、命名空间、事件类型等信息。自定义控制器接收到事件通知后，会根据事件类型来判断是否需要重新调用 Reconcile 方法。
// 如果事件类型是 MODIFIED，即资源状态被修改，自定义控制器会重新调用 Reconcile 方法来检查状态是否已经更新为期望状态。
// 如果事件类型是 DELETED，即资源被删除，自定义控制器可以选择重新创建资源或者清理相关资源，也可以选择不执行任何操作。
func (r *RedisCRDReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Enabled()
	redis := &myappv1.RedisCRD{}
	err := r.Get(ctx, req.NamespacedName, redis)
	if err != nil {
		logger.Error(err, "get RedisCRD err")
		return ctrl.Result{}, nil
	}
	// 删除Pod，删除Kind:RedisCRD过程中，会自动加上DeleteTimestamp字段
	// 据此判断是否删除了自定义资源
	if !redis.DeletionTimestamp.IsZero() {
		// 如果这个字段不为空的话，那么我们就需要删除了
		err := r.clearRedis(ctx, redis)
		if err != nil {
			logger.Error(err, "delete update err")
		}
		return ctrl.Result{}, err
	}
	logger.Info("redis Obj", "obj", redis)

	num := redis.Spec.Num
	logger.Info("msg===", "num", num, "fini", len(redis.Finalizers))
	// 如果更新了num 小于Finalizers数量 删除pod 达到此数量
	if len(redis.Finalizers) > num {
		// 应该进行缩容
		err := r.clearRedisUpdateNum(redis)
		if err != nil {
			logger.Error(err, "delete update err")
		}
		return ctrl.Result{}, nil
	}
	for i := 1; i <= num; i++ {
		// 创建redis pod redis-sample-1
		podName := fmt.Sprintf("%s-%d", redis.Name, i)
		logger.Info("CreateRedis", "podname:", podName)
		_, err = CreateRedis(r.Client, podName, redis, r.Scheme)
		if err != nil {
			logger.Info("create pod failure,", "errInfo", err)
			// 如果错误是pod已经存在了，就直接返回，无视即可
			if errors.IsAlreadyExists(err) {
				continue
			}
			return ctrl.Result{}, err
		}
	}

	// 一旦更新后，Reconcile还会再次调用一次，而且是马上调用
	err = r.Client.Update(ctx, redis)
	if err != nil {
		logger.Error(err, "Update err")
		return ctrl.Result{}, err
	}
	logger.Info("call result...")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RedisCRDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&myappv1.RedisCRD{}).
		Watches(&source.Kind{Type: &v1.Pod{}}, handler.Funcs{
			DeleteFunc: r.podDeleteHandler,
		}).
		Complete(r)
}
