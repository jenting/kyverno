// Test for cloneList synchronization bug fix

package synchronization_test

import (
    "testing"
)

// Mock Resource Structure
type Resource struct {
    Name      string
    Namespace string
}

// Function to synchronize resources based on source deletion
func synchronizeResources(source []Resource, target []Resource, deleted Resource) []Resource {
    // create a result slice to hold the synchronized resources
    result := []Resource{}
    // iterate over the target resources
    for _, t := range target {
        // only add resource if it's not the one being deleted
        if t.Name != deleted.Name || t.Namespace != deleted.Namespace {
            result = append(result, t)
        }
    }
    return result
}

func TestSynchronizeResources(t *testing.T) {
    // Initial target resources setup
    targets := []Resource{{"secret1", "namespace1"}, {"secret2", "namespace1"}, {"secret3", "namespace1"}}
    // Source resource whose deletion triggers synchronization
    deletedSource := Resource{"secret2", "namespace1"}
    // Expected target resources after deletion
    expected := []Resource{{"secret1", "namespace1"}, {"secret3", "namespace1"}}
    // Call the synchronizeResources function
    result := synchronizeResources([]Resource{deletedSource}, targets, deletedSource)
    
    // Verify the result
    if len(result) != len(expected) {
        t.Errorf("Expected length %d, got %d", len(expected), len(result))
    }
    for i, r := range result {
        if r != expected[i] {
            t.Errorf("Expected %v, got %v", expected[i], r)
        }
    }
}