package operator

import (
	"fmt"

	opv1 "github.com/openshift/api/operator/v1"
	operatorv1listers "github.com/openshift/client-go/operator/listers/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

// dynamicClusterCSIDriverLister adapts a cache.GenericLister backed by an
// *unstructured.Unstructured informer to the typed
// operatorv1listers.ClusterCSIDriverLister interface. This lets code read
// ClusterCSIDriver-specific spec fields (e.g. driverConfig) directly from the
// same dynamic informer/cache already used by the GenericOperatorClient,
// instead of standing up a second, independent typed informer/watch for the
// same singleton object.
type dynamicClusterCSIDriverLister struct {
	lister cache.GenericLister
}

var _ operatorv1listers.ClusterCSIDriverLister = &dynamicClusterCSIDriverLister{}

// newDynamicClusterCSIDriverLister wraps lister, which must list/get
// *unstructured.Unstructured ClusterCSIDriver objects, as a typed
// operatorv1listers.ClusterCSIDriverLister.
func newDynamicClusterCSIDriverLister(lister cache.GenericLister) operatorv1listers.ClusterCSIDriverLister {
	return &dynamicClusterCSIDriverLister{lister: lister}
}

// List lists all ClusterCSIDrivers in the indexer.
func (d *dynamicClusterCSIDriverLister) List(selector labels.Selector) ([]*opv1.ClusterCSIDriver, error) {
	objs, err := d.lister.List(selector)
	if err != nil {
		return nil, err
	}
	drivers := make([]*opv1.ClusterCSIDriver, 0, len(objs))
	for _, obj := range objs {
		driver, err := toClusterCSIDriver(obj)
		if err != nil {
			return nil, err
		}
		drivers = append(drivers, driver)
	}
	return drivers, nil
}

// Get retrieves the ClusterCSIDriver from the index for a given name.
func (d *dynamicClusterCSIDriverLister) Get(name string) (*opv1.ClusterCSIDriver, error) {
	obj, err := d.lister.Get(name)
	if err != nil {
		return nil, err
	}
	return toClusterCSIDriver(obj)
}

// toClusterCSIDriver converts an *unstructured.Unstructured ClusterCSIDriver,
// as returned by the dynamic informer's cache, into its typed representation.
func toClusterCSIDriver(obj runtime.Object) (*opv1.ClusterCSIDriver, error) {
	unstr, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("unexpected type %T for ClusterCSIDriver", obj)
	}
	driver := &opv1.ClusterCSIDriver{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstr.Object, driver); err != nil {
		return nil, fmt.Errorf("unable to convert to ClusterCSIDriver: %w", err)
	}
	return driver, nil
}
