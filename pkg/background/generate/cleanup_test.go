package generate

import (
	"encoding/json"
	"testing"

	"github.com/go-logr/logr"
	"github.com/kyverno/kyverno/api/kyverno"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	kyvernov1beta1 "github.com/kyverno/kyverno/api/kyverno/v1beta1"
	"github.com/kyverno/kyverno/pkg/background/common"
	"github.com/kyverno/kyverno/pkg/clients/dclient"
	"gotest.tools/assert"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func newDownstreamConfigMap(name, namespace string, labels map[string]string) *unstructured.Unstructured {
	cm := &unstructured.Unstructured{}
	cm.SetAPIVersion("v1")
	cm.SetKind("ConfigMap")
	cm.SetName(name)
	cm.SetNamespace(namespace)
	cm.SetLabels(labels)
	return cm
}

// makeDownstreamLabels builds the label set that Kyverno attaches to a cloneList
// downstream resource. Each downstream carries both trigger-level labels (which
// Namespace originated the generation) and source-level labels (which ConfigMap
// in the source namespace was the template).
func makeDownstreamLabels(policyName, ruleName, triggerKind string, triggerUID types.UID, sourceUID types.UID) map[string]string {
	return map[string]string{
		common.GeneratePolicyLabel:          policyName,
		common.GeneratePolicyNamespaceLabel: "",
		common.GenerateRuleLabel:            ruleName,
		kyverno.LabelAppManagedBy:           kyverno.ValueKyvernoApp,
		common.GenerateTriggerUIDLabel:      string(triggerUID),
		common.GenerateTriggerNSLabel:       "",
		common.GenerateTriggerKindLabel:     triggerKind,
		common.GenerateTriggerGroupLabel:    "",
		common.GenerateTriggerVersionLabel:  "v1",
		// Source-level labels set by addSourceLabels in clone.go.
		common.GenerateSourceUIDLabel: string(sourceUID),
	}
}

func newFakeController(objs ...*unstructured.Unstructured) (*GenerateController, error) {
	scheme := runtime.NewScheme()
	gvrToListKind := map[schema.GroupVersionResource]string{
		{Group: "", Version: "v1", Resource: "configmaps"}: "ConfigMapList",
	}
	runtimeObjs := make([]runtime.Object, len(objs))
	for i, o := range objs {
		runtimeObjs[i] = o
	}
	fakeClient, err := dclient.NewFakeClient(scheme, gvrToListKind, runtimeObjs...)
	if err != nil {
		return nil, err
	}
	fakeClient.SetDiscovery(dclient.NewFakeDiscoveryClient(nil))
	return &GenerateController{
		client: fakeClient,
		log:    logr.Discard(),
	}, nil
}

// TestGetDownstreams_CloneList_TriggerDeletion verifies that when the trigger
// resource (e.g. a Namespace) is deleted, getDownstreams returns ALL downstream
// ConfigMaps that were cloned from that trigger, regardless of which source they
// came from.
func TestGetDownstreams_CloneList_TriggerDeletion(t *testing.T) {
	const (
		policyName      = "sync-configmaps"
		ruleName        = "clone-cms"
		triggerKind     = "Namespace"
		triggerName     = "test-namespace"
		targetNamespace = "target-ns"
	)

	triggerUID := types.UID("trigger-uid-abc")
	sourceUID1 := types.UID("source-uid-1")
	sourceUID2 := types.UID("source-uid-2")

	cm1 := newDownstreamConfigMap("source-cm-1", targetNamespace,
		makeDownstreamLabels(policyName, ruleName, triggerKind, triggerUID, sourceUID1))
	cm2 := newDownstreamConfigMap("source-cm-2", targetNamespace,
		makeDownstreamLabels(policyName, ruleName, triggerKind, triggerUID, sourceUID2))

	c, err := newFakeController(cm1, cm2)
	assert.NilError(t, err)

	ur := &kyvernov1beta1.UpdateRequest{
		Spec: kyvernov1beta1.UpdateRequestSpec{
			Policy:           policyName,
			Rule:             ruleName,
			DeleteDownstream: true,
			Resource: kyvernov1.ResourceSpec{
				APIVersion: "v1",
				Kind:       triggerKind,
				Name:       triggerName,
				UID:        triggerUID,
			},
			// No admission context: trigger deletion does not carry source info.
		},
	}

	rule := kyvernov1.Rule{
		Name: ruleName,
		Generation: kyvernov1.Generation{
			CloneList: kyvernov1.CloneList{
				Kinds: []string{"v1/ConfigMap"},
			},
		},
	}

	selector := map[string]string{
		common.GeneratePolicyLabel:          policyName,
		common.GeneratePolicyNamespaceLabel: "",
		common.GenerateRuleLabel:            ruleName,
		kyverno.LabelAppManagedBy:           kyverno.ValueKyvernoApp,
	}

	downstreams, err := c.getDownstreams(rule, selector, ur)
	assert.NilError(t, err)
	// Both downstreams must be returned so that all of them are cleaned up when
	// the trigger Namespace is gone.
	assert.Equal(t, 2, len(downstreams.Items))
}

// TestGetDownstreams_CloneList_SourceDeletion is the regression test for
// https://github.com/kyverno/kyverno/issues/9654.
//
// When a single clone source ConfigMap is deleted from a cloneList rule,
// getDownstreams must return ONLY the downstream that corresponds to that
// specific source, not all downstreams produced by the same trigger.
func TestGetDownstreams_CloneList_SourceDeletion(t *testing.T) {
	const (
		policyName      = "sync-configmaps"
		ruleName        = "clone-cms"
		triggerKind     = "Namespace"
		triggerName     = "test-namespace"
		targetNamespace = "target-ns"
		sourceNS        = "source-ns"
	)

	triggerUID := types.UID("trigger-uid-abc")
	sourceUID1 := types.UID("source-uid-1")
	sourceUID2 := types.UID("source-uid-2")

	// cm1 was cloned from source ConfigMap with UID=source-uid-1.
	// cm2 was cloned from a different source ConfigMap with UID=source-uid-2.
	// Both share the same trigger Namespace.
	cm1 := newDownstreamConfigMap("source-cm-1", targetNamespace,
		makeDownstreamLabels(policyName, ruleName, triggerKind, triggerUID, sourceUID1))
	cm2 := newDownstreamConfigMap("source-cm-2", targetNamespace,
		makeDownstreamLabels(policyName, ruleName, triggerKind, triggerUID, sourceUID2))

	c, err := newFakeController(cm1, cm2)
	assert.NilError(t, err)

	// Build the admission request that the webhook would have created when
	// source-cm-1 was deleted. The old object carries GenerateTypeCloneSourceLabel
	// (set by updateSourceLabel when the source was first cloned) plus its UID.
	deletedSource := &unstructured.Unstructured{}
	deletedSource.SetAPIVersion("v1")
	deletedSource.SetKind("ConfigMap")
	deletedSource.SetName("source-cm-1")
	deletedSource.SetNamespace(sourceNS)
	deletedSource.SetUID(sourceUID1)
	deletedSource.SetLabels(map[string]string{
		common.GenerateTypeCloneSourceLabel: "",
	})
	rawSource, _ := json.Marshal(deletedSource.Object)

	admReq := &admissionv1.AdmissionRequest{
		Operation: admissionv1.Delete,
		OldObject: runtime.RawExtension{Raw: rawSource},
		Resource: metav1.GroupVersionResource{
			Version:  "v1",
			Resource: "configmaps",
		},
	}

	ur := &kyvernov1beta1.UpdateRequest{
		Spec: kyvernov1beta1.UpdateRequestSpec{
			Policy:           policyName,
			Rule:             ruleName,
			DeleteDownstream: true,
			Resource: kyvernov1.ResourceSpec{
				APIVersion: "v1",
				Kind:       triggerKind,
				Name:       triggerName,
				UID:        triggerUID,
			},
			Context: kyvernov1beta1.UpdateRequestSpecContext{
				AdmissionRequestInfo: kyvernov1beta1.AdmissionRequestInfoObject{
					AdmissionRequest: admReq,
					Operation:        admissionv1.Delete,
				},
			},
		},
	}

	rule := kyvernov1.Rule{
		Name: ruleName,
		Generation: kyvernov1.Generation{
			CloneList: kyvernov1.CloneList{
				Kinds: []string{"v1/ConfigMap"},
			},
		},
	}

	selector := map[string]string{
		common.GeneratePolicyLabel:          policyName,
		common.GeneratePolicyNamespaceLabel: "",
		common.GenerateRuleLabel:            ruleName,
		kyverno.LabelAppManagedBy:           kyverno.ValueKyvernoApp,
	}

	downstreams, err := c.getDownstreams(rule, selector, ur)
	assert.NilError(t, err)
	// Only the downstream that corresponds to the deleted source (source-uid-1)
	// must be returned. The downstream from source-uid-2 must be left untouched.
	assert.Equal(t, 1, len(downstreams.Items))
	assert.Equal(t, "source-cm-1", downstreams.Items[0].GetName())
}
