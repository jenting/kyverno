package generate

import (
	"testing"
	
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/apimachinery/pkg/util/intstr"
	"github.com/stretchr/testify/assert"
)

func TestHandleNonPolicyChanges_CloneList_OnlyDeleteMatchingSource(t *testing.T) {
	// Setup
	client := fake.NewSimpleClientset()

	// Create two downstream secrets based on source name
	secret1 := &v1.Secret{ /* fill in details for secret 1 */ }
	secret2 := &v1.Secret{ /* fill in details for secret 2 */ }

	// Add secrets to the client
	client.CoreV1().Secrets("namespace").Create(ctx, secret1, metav1.CreateOptions{})
	client.CoreV1().Secrets("namespace").Create(ctx, secret2, metav1.CreateOptions{})

	// Action: Delete one source secret
	err := client.CoreV1().Secrets("namespace").Delete(ctx, "source-secret-1", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete source secret: %v", err)
	}

	// Verification: Check remaining secrets
	remainingSecrets, err := client.CoreV1().Secrets("namespace").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list secrets: %v", err)
	}

	assert.Equal(t, 1, len(remainingSecrets.Items))
	assert.Equal(t, "secret2", remainingSecrets.Items[0].Name)
}