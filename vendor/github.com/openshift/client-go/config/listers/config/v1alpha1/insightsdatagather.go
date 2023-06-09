// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/openshift/api/config/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// InsightsDataGatherLister helps list InsightsDataGathers.
// All objects returned here must be treated as read-only.
type InsightsDataGatherLister interface {
	// List lists all InsightsDataGathers in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.InsightsDataGather, err error)
	// Get retrieves the InsightsDataGather from the index for a given name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.InsightsDataGather, error)
	InsightsDataGatherListerExpansion
}

// insightsDataGatherLister implements the InsightsDataGatherLister interface.
type insightsDataGatherLister struct {
	indexer cache.Indexer
}

// NewInsightsDataGatherLister returns a new InsightsDataGatherLister.
func NewInsightsDataGatherLister(indexer cache.Indexer) InsightsDataGatherLister {
	return &insightsDataGatherLister{indexer: indexer}
}

// List lists all InsightsDataGathers in the indexer.
func (s *insightsDataGatherLister) List(selector labels.Selector) (ret []*v1alpha1.InsightsDataGather, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.InsightsDataGather))
	})
	return ret, err
}

// Get retrieves the InsightsDataGather from the index for a given name.
func (s *insightsDataGatherLister) Get(name string) (*v1alpha1.InsightsDataGather, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("insightsdatagather"), name)
	}
	return obj.(*v1alpha1.InsightsDataGather), nil
}
