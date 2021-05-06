package pxc

import (
	"fmt"
	"testing"
	"time"

	api "github.com/percona/percona-xtradb-cluster-operator/pkg/apis/pxc/v1"
	"github.com/percona/percona-xtradb-cluster-operator/pkg/pxc/app/statefulset"
	"github.com/percona/percona-xtradb-cluster-operator/version"
	"github.com/pkg/errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake" // nolint
)

var podStatusReady = corev1.PodStatus{
	ContainerStatuses: []corev1.ContainerStatus{
		corev1.ContainerStatus{Ready: true},
	},
	Conditions: []corev1.PodCondition{
		corev1.PodCondition{
			Type:   corev1.ContainersReady,
			Status: corev1.ConditionTrue,
		},
	},
}

func newCR(name, namespace string) *api.PerconaXtraDBCluster {
	return &api.PerconaXtraDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: api.PerconaXtraDBClusterSpec{
			Platform:  version.PlatformKubernetes,
			CRVersion: "1.6.0",
			PXC: &api.PXCSpec{
				PodSpec: &api.PodSpec{
					Enabled: true,
					Size:    3,
				},
			},
			HAProxy: &api.PodSpec{
				Enabled: true,
				Size:    3,
			},
			ProxySQL: &api.PodSpec{
				Enabled: false,
			},
		},
		Status: api.PerconaXtraDBClusterStatus{},
	}
}

func newMockPod(name, namespace string, labels map[string]string, status corev1.PodStatus) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec:   corev1.PodSpec{},
		Status: status,
	}
}

// creates a fake client to mock API calls with the mock objects
func buildFakeClient(objs []runtime.Object) *ReconcilePerconaXtraDBCluster {
	s := scheme.Scheme

	s.AddKnownTypes(api.SchemeGroupVersion, &api.PerconaXtraDBCluster{})

	cl := fake.NewFakeClientWithScheme(s, objs...)

	return &ReconcilePerconaXtraDBCluster{client: cl, scheme: s}
}

func TestAppStatusInit(t *testing.T) {
	cr := newCR("cr-mock", "pxc")

	pxc := statefulset.NewNode(cr)
	pxcSfs := pxc.StatefulSet()

	r := buildFakeClient([]runtime.Object{cr, pxcSfs})

	status, err := r.appStatus(pxc, cr.Namespace, cr.Spec.PXC.PodSpec, cr.CompareVersionWith("1.7.0") >= 0)
	if err != nil {
		t.Error(err)
	}

	if status.Status != api.AppStateInit {
		t.Errorf("AppStatus.Status got %#v, want %#v", status.Status, api.AppStateInit)
	}
}

func TestPXCAppStatusReady(t *testing.T) {
	cr := newCR("cr-mock", "pxc")

	pxc := statefulset.NewNode(cr)
	pxcSfs := pxc.StatefulSet()

	objs := []runtime.Object{cr, pxcSfs}

	for i := 0; i < int(cr.Spec.PXC.Size); i++ {
		objs = append(objs, newMockPod(fmt.Sprintf("pxc-mock-%d", i), cr.Namespace, pxc.Labels(), podStatusReady))
	}

	r := buildFakeClient(objs)

	status, err := r.appStatus(pxc, cr.Namespace, cr.Spec.PXC.PodSpec, cr.CompareVersionWith("1.7.0") >= 0)
	if err != nil {
		t.Error(err)
	}

	if status.Status != api.AppStateReady {
		t.Errorf("AppStatus.Status got %#v, want %#v", status.Status, api.AppStateReady)
	}

	if status.Ready != cr.Spec.PXC.Size {
		t.Errorf("AppStatus.Ready got %#v, want %#v", status.Ready, cr.Spec.PXC.Size)
	}
}

func TestHAProxyAppStatusReady(t *testing.T) {
	cr := newCR("cr-mock", "pxc")

	haproxy := statefulset.NewHAProxy(cr)
	haproxySfs := haproxy.StatefulSet()

	objs := []runtime.Object{cr, haproxySfs}

	for i := 0; i < int(cr.Spec.HAProxy.Size); i++ {
		objs = append(objs, newMockPod(fmt.Sprintf("haproxy-mock-%d", i), cr.Namespace, haproxy.Labels(), podStatusReady))
	}

	r := buildFakeClient(objs)

	status, err := r.appStatus(haproxy, cr.Namespace, cr.Spec.HAProxy, cr.CompareVersionWith("1.7.0") >= 0)
	if err != nil {
		t.Error(err)
	}

	if status.Status != api.AppStateReady {
		t.Errorf("AppStatus.Status got %#v, want %#v", status.Status, api.AppStateReady)
	}

	if status.Ready != cr.Spec.HAProxy.Size {
		t.Errorf("AppStatus.Ready got %#v, want %#v", status.Ready, cr.Spec.HAProxy.Size)
	}
}

func TestAppStatusError(t *testing.T) {
	cr := newCR("cr-mock", "pxc")

	pxc := statefulset.NewNode(cr)
	pxcSfs := pxc.StatefulSet()

	podStatus := corev1.PodStatus{
		Conditions: []corev1.PodCondition{
			corev1.PodCondition{
				Type:               corev1.PodScheduled,
				Reason:             corev1.PodReasonUnschedulable,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-5 * time.Minute)),
			},
		},
	}
	pxcPod := newMockPod("pxc-mock", cr.Namespace, pxc.Labels(), podStatus)

	r := buildFakeClient([]runtime.Object{cr, pxcSfs, pxcPod})

	status, err := r.appStatus(pxc, cr.Namespace, cr.Spec.PXC.PodSpec, cr.CompareVersionWith("1.7.0") >= 0)
	if err != nil {
		t.Error(err)
	}

	if status.Status != api.AppStateError {
		t.Errorf("AppStatus.Status got %#v, want %#v", status.Status, api.AppStateError)
	}
}

func TestUpdateStatusInit(t *testing.T) {
	cr := newCR("cr-mock", "pxc")

	pxc := statefulset.NewNode(cr)
	pxcSfs := pxc.StatefulSet()

	haproxy := statefulset.NewHAProxy(cr)
	haproxySfs := haproxy.StatefulSet()

	r := buildFakeClient([]runtime.Object{cr, pxcSfs, haproxySfs})

	if err := r.updateStatus(cr, nil); err != nil {
		t.Error(err)
	}

	if cr.Status.Status != api.AppStateInit {
		t.Errorf("cr.Status.Status got %#v, want %#v", cr.Status.Status, api.AppStateInit)
	}
}

func TestUpdateStatusReady(t *testing.T) {
	cr := newCR("cr-mock", "pxc")

	pxc := statefulset.NewNode(cr)
	pxcSfs := pxc.StatefulSet()
	haproxy := statefulset.NewHAProxy(cr)
	haproxySfs := haproxy.StatefulSet()

	objs := []runtime.Object{cr, pxcSfs, haproxySfs}

	for i := 0; i < int(cr.Spec.PXC.Size); i++ {
		objs = append(objs, newMockPod(fmt.Sprintf("pxc-mock-%d", i), cr.Namespace, pxc.Labels(), podStatusReady))
	}

	for i := 0; i < int(cr.Spec.HAProxy.Size); i++ {
		objs = append(objs, newMockPod(fmt.Sprintf("haproxy-mock-%d", i), cr.Namespace, haproxy.Labels(), podStatusReady))
	}

	r := buildFakeClient(objs)

	if err := r.updateStatus(cr, nil); err != nil {
		t.Error(err)
	}

	if cr.Status.Status != api.AppStateReady {
		t.Errorf("cr.Status.Status got %#v, want %#v", cr.Status.Status, api.AppStateReady)
	}
}

func TestUpdateStatusError(t *testing.T) {
	cr := newCR("cr-mock", "pxc")

	pxc := statefulset.NewNode(cr)
	pxcSfs := pxc.StatefulSet()

	haproxy := statefulset.NewHAProxy(cr)
	haproxySfs := haproxy.StatefulSet()

	r := buildFakeClient([]runtime.Object{cr, pxcSfs, haproxySfs})

	if err := r.updateStatus(cr, errors.New("mock error")); err != nil {
		t.Error(err)
	}

	if cr.Status.Status != api.AppStateError {
		t.Errorf("cr.Status.Status got %#v, want %#v", cr.Status.Status, api.AppStateError)
	}
}

func TestAppHostNoLoadBalancer(t *testing.T) {
	cr := newCR("cr-mock", "pxc")

	pxc := statefulset.NewNode(cr)
	pxcSfs := pxc.StatefulSet()

	haproxy := statefulset.NewHAProxy(cr)
	haproxySfs := haproxy.StatefulSet()

	r := buildFakeClient([]runtime.Object{cr, pxcSfs, haproxySfs})

	host, err := r.appHost(haproxy, cr.Namespace, cr.Spec.HAProxy)
	if err != nil {
		t.Error(err)
	}

	want := haproxy.Service() + "." + cr.Namespace
	if host != want {
		t.Errorf("host got %#v, want %#v", host, want)
	}
}

func TestAppHostLoadBalancerNoSvc(t *testing.T) {
	cr := newCR("cr-mock", "pxc")

	pxc := statefulset.NewNode(cr)
	pxcSfs := pxc.StatefulSet()

	haproxy := statefulset.NewHAProxy(cr)
	haproxySfs := haproxy.StatefulSet()
	cr.Spec.HAProxy.ServiceType = corev1.ServiceTypeLoadBalancer

	r := buildFakeClient([]runtime.Object{cr, pxcSfs, haproxySfs})

	_, err := r.appHost(haproxy, cr.Namespace, cr.Spec.HAProxy)
	if err == nil {
		t.Error("want err, got nil")
	}
}

func TestAppHostLoadBalancerOnlyIP(t *testing.T) {
	cr := newCR("cr-mock", "pxc")

	pxc := statefulset.NewNode(cr)
	pxcSfs := pxc.StatefulSet()

	haproxy := statefulset.NewHAProxy(cr)
	haproxySfs := haproxy.StatefulSet()
	cr.Spec.HAProxy.ServiceType = corev1.ServiceTypeLoadBalancer
	ip := "99.99.99.99"
	haproxySvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      haproxy.Service(),
			Namespace: cr.Namespace,
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{IP: ip}},
			},
		},
	}

	r := buildFakeClient([]runtime.Object{cr, pxcSfs, haproxySfs, haproxySvc})

	host, err := r.appHost(haproxy, cr.Namespace, cr.Spec.HAProxy)
	if err != nil {
		t.Error(err)
	}

	if host != ip {
		t.Errorf("host got %#v, want %#v", host, ip)
	}
}

func TestAppHostLoadBalancerWithHostname(t *testing.T) {
	cr := newCR("cr-mock", "pxc")

	pxc := statefulset.NewNode(cr)
	pxcSfs := pxc.StatefulSet()

	haproxy := statefulset.NewHAProxy(cr)
	haproxySfs := haproxy.StatefulSet()
	cr.Spec.HAProxy.ServiceType = corev1.ServiceTypeLoadBalancer
	wantHost := "cr-mock.haproxy.test"
	haproxySvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      haproxy.Service(),
			Namespace: cr.Namespace,
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{IP: "99.99.99.99", Hostname: wantHost}},
			},
		},
	}

	r := buildFakeClient([]runtime.Object{cr, pxcSfs, haproxySfs, haproxySvc})

	gotHost, err := r.appHost(haproxy, cr.Namespace, cr.Spec.HAProxy)
	if err != nil {
		t.Error(err)
	}

	if gotHost != wantHost {
		t.Errorf("host got %#v, want %#v", gotHost, wantHost)
	}
}

func TestClusterStatus(t *testing.T) {
	tests := map[string]struct {
		status            api.PerconaXtraDBClusterStatus
		wantAppState      api.AppState
		wantConditionType api.ClusterConditionType
	}{
		"Unknown": {
			status:            api.PerconaXtraDBClusterStatus{},
			wantAppState:      api.AppStateUnknown,
			wantConditionType: api.ClusterInit,
		},
		"PXC error": {
			status:            api.PerconaXtraDBClusterStatus{PXC: api.AppStatus{Status: api.AppStateError}},
			wantAppState:      api.AppStateError,
			wantConditionType: api.ClusterError,
		},
		"PXC init": {
			status:            api.PerconaXtraDBClusterStatus{PXC: api.AppStatus{Status: api.AppStateInit}},
			wantAppState:      api.AppStateInit,
			wantConditionType: api.ClusterInit,
		},
		"PXC ready": {
			status:            api.PerconaXtraDBClusterStatus{PXC: api.AppStatus{Status: api.AppStateReady}},
			wantAppState:      api.AppStateReady,
			wantConditionType: api.ClusterReady,
		},
		"HAProxy error": {
			status:            api.PerconaXtraDBClusterStatus{HAProxy: api.AppStatus{Status: api.AppStateError}},
			wantAppState:      api.AppStateError,
			wantConditionType: api.ClusterError,
		},
		"HAProxy init": {
			status:            api.PerconaXtraDBClusterStatus{HAProxy: api.AppStatus{Status: api.AppStateInit}},
			wantAppState:      api.AppStateInit,
			wantConditionType: api.ClusterInit,
		},
		"HAProxy ready": {
			status: api.PerconaXtraDBClusterStatus{
				PXC:     api.AppStatus{Status: api.AppStateReady},
				HAProxy: api.AppStatus{Status: api.AppStateReady},
			},
			wantAppState:      api.AppStateReady,
			wantConditionType: api.ClusterReady,
		},
		"ProxySQL error": {
			status:            api.PerconaXtraDBClusterStatus{ProxySQL: api.AppStatus{Status: api.AppStateError}},
			wantAppState:      api.AppStateError,
			wantConditionType: api.ClusterError,
		},
		"ProxySQL init": {
			status:            api.PerconaXtraDBClusterStatus{ProxySQL: api.AppStatus{Status: api.AppStateInit}},
			wantAppState:      api.AppStateInit,
			wantConditionType: api.ClusterInit,
		},
		"ProxySQL ready": {
			status: api.PerconaXtraDBClusterStatus{
				PXC:      api.AppStatus{Status: api.AppStateReady},
				ProxySQL: api.AppStatus{Status: api.AppStateReady},
			},
			wantAppState:      api.AppStateReady,
			wantConditionType: api.ClusterReady,
		},
	}

	for name, test := range tests {
		t.Run(name, func(tt *testing.T) {
			appState, condition := test.status.ClusterStatus()

			if appState != test.wantAppState {
				t.Errorf("AppState got %#v, want %#v", appState, test.wantAppState)
			}

			if condition.Type != test.wantConditionType {
				t.Errorf("ClusterCondition.Type got %#v, want %#v", condition.Type, test.wantConditionType)
			}
		})
	}
}