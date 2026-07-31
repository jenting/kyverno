package generate

import (
	"encoding/json"
	"testing"

	"github.com/kyverno/kyverno/api/kyverno"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	kyvernov1beta1 "github.com/kyverno/kyverno/api/kyverno/v1beta1"
	"github.com/kyverno/kyverno/pkg/background/common"
	"github.com/kyverno/kyverno/pkg/clients/dclient"
	"github.com/kyverno/kyverno/pkg/logging"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func downstreamSecret(name, namespace, sourceUID string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"labels": map[string]interface{}{
					common.GeneratePolicyLabel:          "sync-secrets",
					common.GeneratePolicyNamespaceLabel: "",
					common.GenerateRuleLabel:            "sync-secret",
					kyverno.LabelAppManagedBy:           kyverno.ValueKyvernoApp,
					// same trigger (the target Namespace) for both downstreams
					common.GenerateTriggerGroupLabel:   "",
					common.GenerateTriggerVersionLabel: "v1",
					common.GenerateTriggerKindLabel:    "Namespace",
					common.GenerateTriggerNSLabel:      "",
					common.GenerateTriggerUIDLabel:     "trigger-uid-123",
					// each downstream references a distinct source secret
					common.GenerateSourceUIDLabel: sourceUID,
				},
			},
		},
	}
}

// TestGetDownstreamsCloneListScopedToDeletedSource verifies that when a single
// source secret is deleted from the source namespace, only the downstream cloned
// from that source is selected for deletion, not every downstream sharing the
// same trigger.
func TestGetDownstreamsCloneListScopedToDeletedSource(t *testing.T) {
	// two downstream secrets in the same target namespace, generated from the
	// same trigger namespace but cloned from two different source secrets.
	ds1 := downstreamSecret("dummy-secret-1", "certs-replicated", "source-uid-1")
	ds2 := downstreamSecret("dummy-secret-2", "certs-replicated", "source-uid-2")

	scheme := runtime.NewScheme()
	gvrToListKind := map[schema.GroupVersionResource]string{
		{Group: "", Version: "v1", Resource: "secrets"}:    "SecretList",
		{Group: "", Version: "v1", Resource: "namespaces"}: "NamespaceList",
	}
	client, err := dclient.NewFakeClient(scheme, gvrToListKind, ds1, ds2)
	if err != nil {
		t.Fatal(err)
	}
	client.SetDiscovery(dclient.NewFakeDiscoveryClient(nil))

	c := &GenerateController{
		client: client,
		log:    logging.GlobalLogger(),
	}

	rule := kyvernov1.Rule{
		Name: "sync-secret",
		Generation: kyvernov1.Generation{
			Synchronize: true,
			CloneList: kyvernov1.CloneList{
				Namespace: "certs",
				Kinds:     []string{"v1/Secret"},
			},
		},
	}

	// the deleted source secret (dummy-secret-1) carries the clone-source tag
	deletedSource := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]interface{}{
			"name":      "dummy-secret-1",
			"namespace": "certs",
			"uid":       "source-uid-1",
			"labels": map[string]interface{}{
				common.GenerateTypeCloneSourceLabel: "",
			},
		},
	}
	raw, err := json.Marshal(deletedSource)
	if err != nil {
		t.Fatal(err)
	}

	ur := &kyvernov1beta1.UpdateRequest{
		Spec: kyvernov1beta1.UpdateRequestSpec{
			Rule:             "sync-secret",
			DeleteDownstream: true,
			Resource: kyvernov1.ResourceSpec{
				APIVersion: "v1",
				Kind:       "Namespace",
				Name:       "certs-replicated",
				UID:        "trigger-uid-123",
			},
			Context: kyvernov1beta1.UpdateRequestSpecContext{
				AdmissionRequestInfo: kyvernov1beta1.AdmissionRequestInfoObject{
					Operation: admissionv1.Delete,
					AdmissionRequest: &admissionv1.AdmissionRequest{
						Operation: admissionv1.Delete,
						Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"},
						Namespace: "certs",
						OldObject: runtime.RawExtension{Raw: raw},
					},
				},
			},
		},
	}

	selector := map[string]string{
		common.GeneratePolicyLabel:          "sync-secrets",
		common.GeneratePolicyNamespaceLabel: "",
		common.GenerateRuleLabel:            "sync-secret",
		kyverno.LabelAppManagedBy:           kyverno.ValueKyvernoApp,
	}

	downstreams, err := c.getDownstreams(rule, selector, ur)
	if err != nil {
		t.Fatal(err)
	}

	if len(downstreams.Items) != 1 {
		names := make([]string, 0, len(downstreams.Items))
		for _, item := range downstreams.Items {
			names = append(names, item.GetName())
		}
		t.Fatalf("expected only the downstream cloned from the deleted source to be selected, got %d: %v", len(downstreams.Items), names)
	}
	if got := downstreams.Items[0].GetName(); got != "dummy-secret-1" {
		t.Fatalf("expected downstream dummy-secret-1 to be selected, got %s", got)
	}
}
