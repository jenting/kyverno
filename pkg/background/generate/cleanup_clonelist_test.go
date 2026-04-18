package generate

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	kyvernov2 "github.com/kyverno/kyverno/api/kyverno/v2"
	"github.com/kyverno/kyverno/pkg/background/common"
	"github.com/kyverno/kyverno/pkg/clients/dclient"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// fakeCloneListClient mocks the client to track deleted resources
type fakeCloneListClient struct {
	dclient.Interface
	deletedResources []string
	resources        map[string]unstructured.Unstructured
}

func (f *fakeCloneListClient) DeleteResource(ctx context.Context, apiVersion, kind, namespace, name string, _ bool, _ metav1.DeleteOptions) error {
	f.deletedResources = append(f.deletedResources, name)
	delete(f.resources, name)
	return nil
}

func (f *fakeCloneListClient) ListResource(ctx context.Context, apiVersion, kind, namespace string, selector *metav1.LabelSelector) (*unstructured.UnstructuredList, error) {
	var items []unstructured.Unstructured
	for _, resource := range f.resources {
		items = append(items, resource)
	}
	return &unstructured.UnstructuredList{Items: items}, nil
}

// TestHandleNonPolicyChanges_CloneList_OnlyDeleteMatchingSource verifies that when a source resource
// is deleted, only the downstream resource with matching source name is deleted,
// while other cloned resources remain intact.
func TestHandleNonPolicyChanges_CloneList_OnlyDeleteMatchingSource(t *testing.T) {
	// Setup: Create two downstream secrets that were cloned from different sources
downstreamSecret1 := unstructured.Unstructured{}
downstreamSecret1.SetAPIVersion("v1")
downstreamSecret1.SetKind("Secret")
downstreamSecret1.SetNamespace("certs-replicated")
downstreamSecret1.SetName("replicated-secret-1")
downstreamSecret1.SetLabels(map[string]string{
	common.GeneratePolicyLabel:           "sync-secrets",
	common.GeneratePolicyNamespaceLabel:  "",
	common.GenerateRuleLabel:             "sync-rule",
	common.GenerateSourceNameLabel:       "source-secret-1",
	common.GenerateSourceNSLabel:         "source-ns",
	common.GenerateSourceKindLabel:       "Secret",
	common.GenerateTriggerUIDLabel:       "trigger-uid-1",
	common.GenerateTriggerNSLabel:        "certs-replicated",
	common.GenerateTriggerKindLabel:      "Namespace",
})

downstreamSecret2 := unstructured.Unstructured{}
downstreamSecret2.SetAPIVersion("v1")
downstreamSecret2.SetKind("Secret")
downstreamSecret2.SetNamespace("certs-replicated")
downstreamSecret2.SetName("replicated-secret-2")
downstreamSecret2.SetLabels(map[string]string{
	common.GeneratePolicyLabel:           "sync-secrets",
	common.GeneratePolicyNamespaceLabel:  "",
	common.GenerateRuleLabel:             "sync-rule",
	common.GenerateSourceNameLabel:       "source-secret-2",
	common.GenerateSourceNSLabel:         "source-ns",
	common.GenerateSourceKindLabel:       "Secret",
	common.GenerateTriggerUIDLabel:       "trigger-uid-1",
	common.GenerateTriggerNSLabel:        "certs-replicated",
	common.GenerateTriggerKindLabel:      "Namespace",
})

	fakeClient := &fakeCloneListClient{
		Interface:        dclient.NewEmptyFakeClient(),
		deletedResources: []string{},
		resources: map[string]unstructured.Unstructured{
			"replicated-secret-1": downstreamSecret1,
			"replicated-secret-2": downstreamSecret2,
		},
	}

	controller := &GenerateController{
		client: fakeClient,
		log:    logr.Discard(),
	}

	policy := &kyvernov1.ClusterPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "sync-secrets"},
		Spec: kyvernov1.Spec{
			Rules: []kyvernov1.Rule{
				{
					Name: "sync-rule",
					Generation: &kyvernov1.Generation{
						GeneratePattern: kyvernov1.GeneratePattern{
							CloneList: kyvernov1.CloneList{
								Namespace: "source-ns",
								Kinds:     []string{"v1/Secret"},
							},
						},
					},
				},
			},
		},
	}

	ruleContext := kyvernov2.RuleContext{
		Rule: "sync-rule",
		Trigger: kyvernov1.ResourceSpec{
			Name:      "source-secret-1",
			Namespace: "source-ns",
			Kind:      "Secret",
			UID:       "trigger-uid-1",
		},
	}

	ur := &kyvernov2.UpdateRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ur"},
	}

	// Execute: Call handleNonPolicyChanges
	err := controller.handleNonPolicyChanges(policy, ruleContext, ur)

	// Assert: No error occurred
	assert.NoError(t, err)

	// Assert: Only replicated-secret-1 was deleted
	assert.Len(t, fakeClient.deletedResources, 1)
	assert.Contains(t, fakeClient.deletedResources, "replicated-secret-1")
	assert.NotContains(t, fakeClient.deletedResources, "replicated-secret-2")

	// Assert: replicated-secret-2 still exists in the resources map
	assert.Contains(t, fakeClient.resources, "replicated-secret-2")
	assert.NotContains(t, fakeClient.resources, "replicated-secret-1")
}

// TestFetch_CloneList_FiltersResourcesBySourceName verifies that the fetch function
// correctly filters cloneList resources by source name
func TestFetch_CloneList_FiltersResourcesBySourceName(t *testing.T) {
	// Setup: Create three downstream secrets with different source names
	secrets := map[string]unstructured.Unstructured{}
	for i := 1; i <= 3; i++ {
		secret := unstructured.Unstructured{}
		secret.SetAPIVersion("v1")
		secret.SetKind("Secret")
		secret.SetNamespace("target-ns")
		sourceName := "source-secret-" + string(rune('0'+i))
		secret.SetName("target-secret-" + string(rune('0'+i)))
		secret.SetLabels(map[string]string{
			common.GenerateSourceNameLabel: sourceName,
			common.GenerateSourceNSLabel:   "source-ns",
			common.GenerateSourceKindLabel: "Secret",
		})
		secrets["target-secret-"+string(rune('0'+i))] = secret
	}

	fakeClient := &fakeCloneListClient{
		Interface:        dclient.NewEmptyFakeClient(),
		deletedResources: []string{},
		resources:        secrets,
	}

	controller := &GenerateController{
		client: fakeClient,
		log:    logr.Discard(),
	}

	// Create a GeneratePattern for cloneList
	generatePattern := kyvernov1.GeneratePattern{
		CloneList: kyvernov1.CloneList{
			Namespace: "source-ns",
			Kinds:     []string{"v1/Secret"},
		},
	}

	// Create selector
	selector := map[string]string{
		common.GenerateTriggerNameLabel: "source-secret-2",
	}

	// Create ruleContext with source-secret-2 as the trigger
	ruleContext := &kyvernov2.RuleContext{
		Trigger: kyvernov1.ResourceSpec{
			Name: "source-secret-2",
		},
	}

	// Execute: Call fetch
	result, err := controller.fetch(generatePattern, selector, ruleContext)

	// Assert: No error and only one resource should be returned (source-secret-2)
	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "target-secret-2", result[0].GetName())
}

// TestFetch_CloneList_FallbackForMissingLabel verifies backward compatibility
// when source-name label is missing
func TestFetch_CloneList_FallbackForMissingLabel(t *testing.T) {
	// Setup: Create a secret without source-name label (old behavior)
	secret := unstructured.Unstructured{}
	secret.SetAPIVersion("v1")
	secret.SetKind("Secret")
	secret.SetNamespace("target-ns")
	secret.SetName("unlabeled-secret")
	// Deliberately not setting source-name label to test fallback behavior

	fakeClient := &fakeCloneListClient{
		Interface:        dclient.NewEmptyFakeClient(),
		deletedResources: []string{},
		resources: map[string]unstructured.Unstructured{
			"unlabeled-secret": secret,
		},
	}

	controller := &GenerateController{
		client: fakeClient,
		log:    logr.Discard(),
	}

	generatePattern := kyvernov1.GeneratePattern{
		CloneList: kyvernov1.CloneList{
			Namespace: "source-ns",
			Kinds:     []string{"v1/Secret"},
		},
	}

	selector := map[string]string{
		common.GenerateTriggerNameLabel: "some-source",
	}

	ruleContext := &kyvernov2.RuleContext{
		Trigger: kyvernov1.ResourceSpec{
			Name: "some-source",
		},
	}

	// Execute: Call fetch
	result, err := controller.fetch(generatePattern, selector, ruleContext)

	// Assert: The resource should be included as fallback (backward compatibility)
	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "unlabeled-secret", result[0].GetName())
}