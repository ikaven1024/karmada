package configmanager

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
	"github.com/karmada-io/karmada/pkg/util/helper"
)

func TestConfigurableInterpreter_GetReplicas(t *testing.T) {
	type fields struct {
		script string
	}
	type args struct {
		object runtime.Object
	}
	tests := []struct {
		name         string
		fields       fields
		args         args
		wantReplicas int32
		wantRequire  *workv1alpha2.ReplicaRequirements
		wantEnabled  bool
		wantErr      bool
	}{
		{
			name: "script is empty",
			fields: fields{
				script: "",
			},
			args: args{
				object: &corev1.Pod{},
			},
			wantReplicas: 0,
			wantRequire:  nil,
			wantEnabled:  false,
			wantErr:      false,
		},
		{
			name: "script is wrong",
			fields: fields{
				script: "wrong script",
			},
			args: args{
				object: &corev1.Pod{},
			},
			wantReplicas: 0,
			wantRequire:  nil,
			wantEnabled:  true,
			wantErr:      true,
		},
		{
			name: "success",
			fields: fields{
				script: `
function GetReplicas(obj)
	replica = obj.spec.replicas
	requirement = {
		resourceRequest = obj.spec.template.spec.containers[1].resources.limits,
		nodeClaim = {
			nodeSelector = obj.spec.template.spec.nodeSelector,
			tolerations = obj.spec.template.spec.tolerations
		}
	}
    return replica, requirement
end`,
			},
			args: args{
				object: &appsv1.Deployment{
					TypeMeta: metav1.TypeMeta{
						APIVersion: appsv1.SchemeGroupVersion.String(),
						Kind:       "Deployment",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: pointer.Int32(1),
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											corev1.ResourceCPU: resource.MustParse("100"),
										},
									},
								}},
								NodeSelector: map[string]string{"foo": "foo1"},
								Tolerations: []corev1.Toleration{{
									Key: "foo", Operator: corev1.TolerationOpExists,
								}},
							},
						}}},
			},
			wantReplicas: 1,
			wantRequire: &workv1alpha2.ReplicaRequirements{
				NodeClaim: &workv1alpha2.NodeClaim{
					NodeSelector: map[string]string{"foo": "foo1"},
					Tolerations: []corev1.Toleration{{
						Key: "foo", Operator: corev1.TolerationOpExists,
					}},
				},
				ResourceRequest: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("100"),
				},
			},
			wantEnabled: true,
			wantErr:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := helper.ToUnstructured(tt.args.object)
			if err != nil {
				t.Fatal(err)
			}

			c := luaScriptInterpreter{
				Scripts{
					GetReplicas: tt.fields.script,
				},
			}

			gotReplicas, gotRequire, gotEnabled, err := c.GetReplicas(obj)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetReplicas() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotReplicas != tt.wantReplicas {
				t.Errorf("GetReplicas() gotReplicas = %v, want %v", gotReplicas, tt.wantReplicas)
			}
			if !reflect.DeepEqual(gotRequire, tt.wantRequire) {
				t.Errorf("GetReplicas() gotRequire = %#v, want %#v", gotRequire, tt.wantRequire)
			}
			if gotEnabled != tt.wantEnabled {
				t.Errorf("GetReplicas() gotEnabled = %v, want %v", gotEnabled, tt.wantEnabled)
			}
		})
	}
}

func TestConfigurableInterpreter_ReviseReplica(t *testing.T) {
	type fields struct {
		script string
	}
	type args struct {
		object  *unstructured.Unstructured
		replica int64
	}
	tests := []struct {
		name        string
		fields      fields
		args        args
		wantObj     *unstructured.Unstructured
		wantEnabled bool
		wantErr     bool
	}{
		{
			name: "script is empty",
			fields: fields{
				script: "",
			},
			args: args{
				object: &unstructured.Unstructured{},
			},
			wantEnabled: false,
		},
		{
			name: "script is wrong",
			fields: fields{
				script: "wrong script",
			},
			args: args{
				object: &unstructured.Unstructured{},
			},
			wantEnabled: true,
			wantErr:     true,
		},
		{
			name: "success",
			fields: fields{
				script: `
function ReviseReplica(obj, desiredReplica)
    obj.spec.replicas = desiredReplica
    return obj
end`,
			},
			args: args{
				object: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": appsv1.SchemeGroupVersion.String(),
						"kind":       "Deployment",
						"spec": map[string]interface{}{
							"replicas": 1,
						},
					},
				},
				replica: 2,
			},
			wantEnabled: true,
			wantObj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": appsv1.SchemeGroupVersion.String(),
					"kind":       "Deployment",
					"spec": map[string]interface{}{
						"replicas": int64(2), // convert to int64 in json unmarshal
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := luaScriptInterpreter{
				Scripts{
					ReviseReplica: tt.fields.script,
				},
			}
			gotObj, gotEnabled, err := c.ReviseReplica(tt.args.object, tt.args.replica)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReviseReplica() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotObj, tt.wantObj) {
				t.Errorf("ReviseReplica() gotObj = %#v, want %#v", gotObj, tt.wantObj)
			}
			if gotEnabled != tt.wantEnabled {
				t.Errorf("ReviseReplica() gotEnabled = %v, want %v", gotEnabled, tt.wantEnabled)
			}
		})
	}
}

func TestConfigurableInterpreter_Retain(t *testing.T) {
	type fields struct {
		script string
	}
	type args struct {
		desired  *unstructured.Unstructured
		observed *unstructured.Unstructured
	}
	tests := []struct {
		name         string
		fields       fields
		args         args
		wantRetained *unstructured.Unstructured
		wantEnabled  bool
		wantErr      bool
	}{
		{
			name: "script is empty",
			fields: fields{
				script: "",
			},
			args: args{
				desired: &unstructured.Unstructured{},
			},
			wantEnabled: false,
		},
		{
			name: "script is wrong",
			fields: fields{
				script: "wrong script",
			},
			args: args{
				desired: &unstructured.Unstructured{},
			},
			wantEnabled: true,
			wantErr:     true,
		},
		{
			name: "success",
			fields: fields{
				script: `
function Retain(desiredObj, runtimeObj)
    desiredObj.spec.fieldFoo = runtimeObj.spec.fieldFoo
    return desiredObj
end`,
			},
			args: args{
				desired: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": appsv1.SchemeGroupVersion.String(),
						"kind":       "Deployment",
						"spec": map[string]interface{}{
							"fieldFoo": "new",
						},
					},
				},
				observed: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": appsv1.SchemeGroupVersion.String(),
						"kind":       "Deployment",
						"spec": map[string]interface{}{
							"fieldFoo": "old",
						},
					},
				},
			},
			wantEnabled: true,
			wantRetained: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": appsv1.SchemeGroupVersion.String(),
					"kind":       "Deployment",
					"spec": map[string]interface{}{
						"fieldFoo": "old",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := luaScriptInterpreter{
				Scripts{
					Retain: tt.fields.script,
				},
			}
			gotObj, gotEnabled, err := c.Retain(tt.args.desired, tt.args.observed)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReviseReplica() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotObj, tt.wantRetained) {
				t.Errorf("ReviseReplica() gotObj = %#v, want %#v", gotObj, tt.wantRetained)
			}
			if gotEnabled != tt.wantEnabled {
				t.Errorf("ReviseReplica() gotEnabled = %v, want %v", gotEnabled, tt.wantEnabled)
			}
		})
	}
}

func TestConfigurableInterpreter_AggregateStatus(t *testing.T) {
	type fields struct {
		script string
	}
	type args struct {
		object                *unstructured.Unstructured
		aggregatedStatusItems []workv1alpha2.AggregatedStatusItem
	}
	tests := []struct {
		name        string
		fields      fields
		args        args
		wantObj     *unstructured.Unstructured
		wantEnabled bool
		wantErr     bool
	}{
		{
			name: "script is empty",
			fields: fields{
				script: "",
			},
			args: args{
				object: &unstructured.Unstructured{},
			},
			wantEnabled: false,
		},
		{
			name: "script is wrong",
			fields: fields{
				script: "wrong script",
			},
			args: args{
				object: &unstructured.Unstructured{},
			},
			wantEnabled: true,
			wantErr:     true,
		},
		{
			name: "success",
			fields: fields{
				script: `
function AggregateStatus(desiredObj, items)
	print(items)
    for i = 1, #items do
        desiredObj.status.readyReplicas = desiredObj.status.readyReplicas + items[i].status.readyReplicas
    end
    return desiredObj
end`,
			},
			args: args{
				object: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": appsv1.SchemeGroupVersion.String(),
						"kind":       "Deployment",
						"status": map[string]interface{}{
							"readyReplicas": 1,
						},
					},
				},
				aggregatedStatusItems: []workv1alpha2.AggregatedStatusItem{
					{Status: &runtime.RawExtension{Raw: []byte(`{"readyReplicas":2}`)}},
					{Status: &runtime.RawExtension{Raw: []byte(`{"readyReplicas":3}`)}},
				},
			},
			wantEnabled: true,
			wantObj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": appsv1.SchemeGroupVersion.String(),
					"kind":       "Deployment",
					"status": map[string]interface{}{
						"readyReplicas": int64(6),
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := luaScriptInterpreter{
				Scripts{
					AggregateStatus: tt.fields.script,
				},
			}
			gotObj, gotEnabled, err := c.AggregateStatus(tt.args.object, tt.args.aggregatedStatusItems)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReviseReplica() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotObj, tt.wantObj) {
				t.Errorf("ReviseReplica() gotObj = %#v, want %#v", gotObj, tt.wantObj)
			}
			if gotEnabled != tt.wantEnabled {
				t.Errorf("ReviseReplica() gotEnabled = %v, want %v", gotEnabled, tt.wantEnabled)
			}
		})
	}
}

func TestConfigurableInterpreter_GetDependencies(t *testing.T) {
	type fields struct {
		script string
	}
	type args struct {
		object *unstructured.Unstructured
	}
	tests := []struct {
		name        string
		fields      fields
		args        args
		wantObj     []configv1alpha1.DependentObjectReference
		wantEnabled bool
		wantErr     bool
	}{
		{
			name: "script is empty",
			fields: fields{
				script: "",
			},
			args: args{
				object: &unstructured.Unstructured{},
			},
			wantEnabled: false,
		},
		{
			name: "script is wrong",
			fields: fields{
				script: "wrong script",
			},
			args: args{
				object: &unstructured.Unstructured{},
			},
			wantEnabled: true,
			wantErr:     true,
		},
		{
			name: "success",
			fields: fields{
				script: `
function GetDependencies(desiredObj)
    dependencies = {}
    if desiredObj.spec.serviceAccountName ~= "" and desiredObj.spec.serviceAccountName ~= "default" then
        dependency = {}
        dependency.apiVersion = "v1"
        dependency.kind = "ServiceAccount"
        dependency.name = desiredObj.spec.serviceAccountName
        dependency.namespace = desiredObj.metadata.namespace
        dependencies[1] = {}
        dependencies[1] = dependency
    end
    return dependencies
end`,
			},
			args: args{
				object: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": appsv1.SchemeGroupVersion.String(),
						"kind":       "Deployment",
						"metadata": map[string]interface{}{
							"namespace": "ns",
						},
						"spec": map[string]interface{}{
							"serviceAccountName": "sa",
						},
					},
				},
			},
			wantEnabled: true,
			wantObj: []configv1alpha1.DependentObjectReference{
				{
					APIVersion: "v1",
					Kind:       "ServiceAccount",
					Namespace:  "ns",
					Name:       "sa",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := luaScriptInterpreter{
				Scripts{
					GetDependencies: tt.fields.script,
				},
			}
			gotObj, gotEnabled, err := c.GetDependencies(tt.args.object)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReviseReplica() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotObj, tt.wantObj) {
				t.Errorf("ReviseReplica() gotObj = %#v, want %#v", gotObj, tt.wantObj)
			}
			if gotEnabled != tt.wantEnabled {
				t.Errorf("ReviseReplica() gotEnabled = %v, want %v", gotEnabled, tt.wantEnabled)
			}
		})
	}
}

func TestConfigurableInterpreter_ReflectStatus(t *testing.T) {
	type fields struct {
		script string
	}
	type args struct {
		object *unstructured.Unstructured
	}
	tests := []struct {
		name        string
		fields      fields
		args        args
		wantObj     *runtime.RawExtension
		wantEnabled bool
		wantErr     bool
	}{
		{
			name: "script is empty",
			fields: fields{
				script: "",
			},
			args: args{
				object: &unstructured.Unstructured{},
			},
			wantEnabled: false,
		},
		{
			name: "script is wrong",
			fields: fields{
				script: "wrong script",
			},
			args: args{
				object: &unstructured.Unstructured{},
			},
			wantEnabled: true,
			wantErr:     true,
		},
		{
			name: "success",
			fields: fields{
				script: `
function ReflectStatus(observedObj)
    status = {}
    status.readyReplicas = observedObj.status.observedObj
    return status
end`,
			},
			args: args{
				object: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": appsv1.SchemeGroupVersion.String(),
						"kind":       "Deployment",
						"status": map[string]interface{}{
							"observedObj": map[string]interface{}{
								"replicas": 1,
							},
						},
					},
				},
			},
			wantEnabled: true,
			wantObj: &runtime.RawExtension{
				Raw: []byte(`{"readyReplicas":{"replicas":1}}`),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := luaScriptInterpreter{
				Scripts{
					ReflectStatus: tt.fields.script,
				},
			}
			gotObj, gotEnabled, err := c.ReflectStatus(tt.args.object)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReviseReplica() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotObj, tt.wantObj) {
				t.Errorf("ReviseReplica() gotObj = %#v, want %#v", gotObj, tt.wantObj)
			}
			if gotEnabled != tt.wantEnabled {
				t.Errorf("ReviseReplica() gotEnabled = %v, want %v", gotEnabled, tt.wantEnabled)
			}
		})
	}
}

func TestConfigurableInterpreter_InterpretHealth(t *testing.T) {
	type fields struct {
		script string
	}
	type args struct {
		object *unstructured.Unstructured
	}
	tests := []struct {
		name        string
		fields      fields
		args        args
		wantHealthy bool
		wantEnabled bool
		wantErr     bool
	}{
		{
			name: "script is empty",
			fields: fields{
				script: "",
			},
			args: args{
				object: &unstructured.Unstructured{},
			},
			wantEnabled: false,
		},
		{
			name: "script is wrong",
			fields: fields{
				script: "wrong script",
			},
			args: args{
				object: &unstructured.Unstructured{},
			},
			wantEnabled: true,
			wantErr:     true,
		},
		{
			name: "success",
			fields: fields{
				script: `
function InterpretHealth(observedObj)
    if observedObj.status.readyReplicas == observedObj.status.replicas then
        return true
    end
end`,
			},
			args: args{
				object: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": appsv1.SchemeGroupVersion.String(),
						"kind":       "Deployment",
						"status": map[string]interface{}{
							"replicas":      1,
							"readyReplicas": 1,
						},
					},
				},
			},
			wantEnabled: true,
			wantHealthy: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := luaScriptInterpreter{
				Scripts{
					InterpretHealth: tt.fields.script,
				},
			}

			gotHealthy, gotEnabled, err := c.InterpretHealth(tt.args.object)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReviseReplica() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotHealthy, tt.wantHealthy) {
				t.Errorf("ReviseReplica() gotObj = %#v, want %#v", gotHealthy, tt.wantHealthy)
			}
			if gotEnabled != tt.wantEnabled {
				t.Errorf("ReviseReplica() gotEnabled = %v, want %v", gotEnabled, tt.wantEnabled)
			}
		})
	}
}
