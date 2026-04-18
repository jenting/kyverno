// fetch function to handle cloneList resource deletions by source name matching
func fetch(sourceName string) error {
    // Get the downstream resources
    downstreamResources, err := getDownstreamResources()
    if err != nil {
        return err
    }

    // Only delete the downstream resource corresponding to the deleted source resource
    for _, resource := range downstreamResources {
        if resource.SourceName == sourceName {
            if err := deleteResource(resource); err != nil {
                return err
            }
        }
    }
    return nil
}