/*
 Copyright 2021 - 2024 Crunchy Data Solutions, Inc.
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

package postgrescluster

import (
	"context"

	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/percona/percona-postgresql-operator/internal/naming"
	"github.com/percona/percona-postgresql-operator/internal/patroni"
)

// watchPods returns a handler.EventHandler for Pods.
func (*Reconciler) watchPods() handler.Funcs {
	return handler.Funcs{
		UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			labels := e.ObjectNew.GetLabels()
			cluster := labels[naming.LabelCluster]

			// When a Patroni pod stops being standby leader, the entire cluster
			// may have come out of standby. Queue an event to start applying
			// changes if PostgreSQL is now writable.
			if len(cluster) != 0 &&
				patroni.PodIsStandbyLeader(e.ObjectOld) &&
				!patroni.PodIsStandbyLeader(e.ObjectNew) {
				q.Add(reconcile.Request{NamespacedName: client.ObjectKey{
					Namespace: e.ObjectNew.GetNamespace(),
					Name:      cluster,
				}})
				return
			}

			// Queue an event when a Patroni pod indicates it needs to restart
			// or finished restarting.
			if len(cluster) != 0 &&
				(patroni.PodRequiresRestart(e.ObjectOld) ||
					patroni.PodRequiresRestart(e.ObjectNew)) {
				q.Add(reconcile.Request{NamespacedName: client.ObjectKey{
					Namespace: e.ObjectNew.GetNamespace(),
					Name:      cluster,
				}})
				return
			}

			// Queue an event to start applying changes if the PostgreSQL instance
			// now has the "master" role.
			if len(cluster) != 0 &&
				!patroni.PodIsPrimary(e.ObjectOld) &&
				patroni.PodIsPrimary(e.ObjectNew) {
				q.Add(reconcile.Request{NamespacedName: client.ObjectKey{
					Namespace: e.ObjectNew.GetNamespace(),
					Name:      cluster,
				}})
				return
			}

			oldAnnotations := e.ObjectOld.GetAnnotations()
			newAnnotations := e.ObjectNew.GetAnnotations()
			// If the suggested-pgdata-pvc-size annotation is added or changes, reconcile.
			if len(cluster) != 0 && oldAnnotations["suggested-pgdata-pvc-size"] != newAnnotations["suggested-pgdata-pvc-size"] {
				q.Add(reconcile.Request{NamespacedName: client.ObjectKey{
					Namespace: e.ObjectNew.GetNamespace(),
					Name:      cluster,
				}})
				return
			}
		},
	}
}
