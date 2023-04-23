package controller

import (
	"context"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	v1 "test.com/rediscrd/api/v1"
)

func isExistPod(client client.Client, podName string, redisConfig *v1.RedisCRD) bool {
	err := client.Get(context.Background(), types.NamespacedName{
		Name:      podName,
		Namespace: redisConfig.Namespace,
	}, &coreV1.Pod{})
	if err != nil {
		return false
	}
	return true
}

// CreateRedis runtime.Scheme是干嘛的
// 用于定义 Kubernetes API 对象的序列化和反序列化
// 在 Kubernetes 中，每个 API 对象都由一个 API 版本和一个 API 资源类型组成，
// 例如 v1.Pod 表示 Pod 资源对象的 API 版本为 v1，资源类型为 Pod。
// 而 *runtime.Scheme 则用于管理和注册这些 API 资源类型。
func CreateRedis(client client.Client, podName string, redisConfig *v1.RedisCRD, scheme *runtime.Scheme) (string, error) {

	// 判断一下pod是否已经被创建
	if isExistPod(client, podName, redisConfig) {
		return "", nil
	}
	newPod := &coreV1.Pod{}
	newPod.Name = podName
	newPod.Namespace = redisConfig.Namespace
	newPod.Spec.Containers = []coreV1.Container{
		{
			Name:            podName,
			Image:           "redis:5.0.14",
			ImagePullPolicy: coreV1.PullIfNotPresent,
			Ports: []coreV1.ContainerPort{
				{
					ContainerPort: int32(redisConfig.Spec.Port),
				},
			},
		},
	}
	// 是 Kubernetes 中一个常用的工具函数，用于为资源对象设置 OwnerReference
	// 第一个参数：owner：表示父资源对象，即控制器对象
	// 第二个参数：object：表示子资源对象，即需要与控制器进行关联的资源对象
	// 第三个参数：scheme：表示k8s API Scheme。该参数用于将对象转换为字节流，并将其存储到etcd中
	controllerutil.SetControllerReference(redisConfig, newPod, scheme)
	err := client.Create(context.Background(), newPod)
	for _, v := range redisConfig.Finalizers {
		// 如果有就不添加了
		if v == podName {
			return newPod.Name, err
		}
	}
	redisConfig.Finalizers = append(redisConfig.Finalizers, podName)
	return newPod.Name, err
}

func (r *RedisCRDReconciler) clearRedis(ctx context.Context, redis *v1.RedisCRD) error {
	// 从finalizers中取出podName，然后执行删除
	for _, finalizer := range redis.Finalizers {
		// 删除pod
		// r.Client.Delete用于删除Kubernetes API中的资源对象
		err := r.Client.Delete(ctx, &coreV1.Pod{
			ObjectMeta: v12.ObjectMeta{
				Name:      finalizer,
				Namespace: redis.Namespace,
			},
		})
		if err != nil {
			log.Log.Error(err, "delete error")
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}
		log.Log.Info("delete success!!")
	}
	// 清空finalizers，只要它有值，就无法删除Kind
	redis.Finalizers = []string{}
	// 需要更新状态
	return r.Client.Update(ctx, redis)
}

func (r *RedisCRDReconciler) clearRedisUpdateNum(redis *v1.RedisCRD) error {
	// 从finalizers中取出podName，然后执行 5 -> 4
	num := redis.Spec.Num
	// 如果小于等于0说明没有什么好清理的
	if len(redis.Finalizers) <= 0 {
		return nil
	}
	var err error
	for i := len(redis.Finalizers); i > num; i-- {
		log.Log.Info("clear redis num", "finalizers", redis.Finalizers[i-1])
		// 删除pod
		err = r.Client.Delete(context.Background(), &coreV1.Pod{
			ObjectMeta: v12.ObjectMeta{
				Name:      redis.Finalizers[i-1],
				Namespace: redis.Namespace,
			},
		})
		redis.Finalizers = redis.Finalizers[:i-1]
	}
	if err != nil {
		log.Log.Error(err, "delete error")
		return err
	}
	return r.Client.Update(context.Background(), redis)
}

func (r *RedisCRDReconciler) podDeleteHandler(event event.DeleteEvent, limitingInterface workqueue.RateLimitingInterface) {
	log.Log.Info("delete pod:", "podName", event.Object.GetName())
	// 在k8s中，每个资源对象可以有多个OwnerReferences，用于表示当前资源对象的所有者或者控制器
	// 这个方法是获取资源对象的OwnerReferences列表的方法
	// 在自定义控制器中，可以使用该方法来获取自定义资源对象的 OwnerReferences，
	// 从而判断当前资源对象的控制器是哪个，并进行相应的处理。
	references := event.Object.GetOwnerReferences()
	for _, v := range references {
		if v.Kind == "RedisCRD" && v.Name == "rediscrd-sample" {
			// 需要删除重建
			// reconcile.Request
			// 指示自定义控制器应该对哪些自定义资源对象进行调度和管理
			// 示自定义资源对象的名称和所属命名空间。该字段是一个 types.NamespacedName 类型的结构体，
			// 包含两个字段：Name 和 Namespace，分别表示自定义资源对象的名称和所属命名空间。
			limitingInterface.Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: event.Object.GetNamespace(),
					Name:      v.Name,
				},
			})
		}
	}
}
