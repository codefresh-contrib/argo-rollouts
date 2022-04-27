package gatewayapi

import (
	"context"
	"errors"
	"strings"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/record"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Type holds this controller type
const Type = "GatewayAPI"
const httpRoutes = "httproutes"

var (
	apiGroupToResource = map[string]string{
		defaults.DefaultGatewayAPIGroup: httpRoutes,
	}
)

type ReconcilerConfig struct {
	Rollout  *v1alpha1.Rollout
	Client   ClientInterface
	Recorder *record.EventRecorder
}

type Reconciler struct {
	Rollout  *v1alpha1.Rollout
	Client   ClientInterface
	Recorder *record.EventRecorder
}

type ClientInterface interface {
	Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error)
	Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error)
}

func NewDynamicClient(di dynamic.Interface, namespace string) dynamic.ResourceInterface {
	return di.Resource(GetMappingGVR()).Namespace(namespace)
}

func GetMappingGVR() schema.GroupVersionResource {
	return toMappingGVR(defaults.GetGatewayAPIGroupVersion())
}

func toMappingGVR(apiVersion string) schema.GroupVersionResource {
	parts := strings.Split(apiVersion, "/")
	group := parts[0]
	resourcename := apiGroupToResource[group]
	version := parts[len(parts)-1]
	return schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resourcename,
	}
}

func NewReconciler(cfg *ReconcilerConfig) *Reconciler {
	reconciler := Reconciler{
		Rollout: cfg.Rollout,
		Client:  cfg.Client,
	}
	return &reconciler
}

func (r *Reconciler) UpdateHash(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
	return nil
}

func (r *Reconciler) SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
	ctx := context.TODO()
	rollout := r.Rollout
	httpRouteName := rollout.Spec.Strategy.Canary.TrafficRouting.GatewayAPI.HTTPRoute
	httpRoute, err := r.Client.Get(ctx, httpRouteName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	canaryServiceName := rollout.Spec.Strategy.Canary.CanaryService
	stableServiceName := rollout.Spec.Strategy.Canary.StableService
	rules, isFound, err := unstructured.NestedSlice(httpRoute.Object, "spec", "rules")
	if err != nil {
		return err
	}
	if !isFound {
		return errors.New("spec.rules field was not found in httpRoute")
	}
	backendRefs, err := getBackendRefs(rules)
	if err != nil {
		return err
	}
	if backendRefs == nil {
		return errors.New("spec.rules.backendRefs field was not found in httpRoute")
	}
	canaryService, err := getService(canaryServiceName, backendRefs)
	if err != nil {
		return err
	}
	if canaryService == nil {
		return errors.New("canaryService was not found in httpRoute")
	}
	err = unstructured.SetNestedField(canaryService, int64(desiredWeight), "weight")
	if err != nil {
		return err
	}
	stableService, err := getService(stableServiceName, backendRefs)
	if err != nil {
		return err
	}
	if stableService == nil {
		return errors.New("stableService was not found in httpRoute")
	}
	err = unstructured.SetNestedField(stableService, int64(100-desiredWeight), "weight")
	if err != nil {
		return err
	}
	rules, err = mergeBackendRefs(rules, backendRefs)
	if err != nil {
		return err
	}
	err = unstructured.SetNestedSlice(httpRoute.Object, rules, "spec", "rules")
	if err != nil {
		return err
	}
	_, err = r.Client.Update(ctx, httpRoute, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func getService(serviceName string, services []interface{}) (map[string]interface{}, error) {
	var selectedService map[string]interface{}
	for _, service := range services {
		typedService, ok := service.(map[string]interface{})
		if !ok {
			return nil, errors.New("Failed type assertion setting weight for traefik service")
		}
		nameOfCurrentService, isFound, err := unstructured.NestedString(typedService, "name")
		if err != nil {
			return nil, err
		}
		if !isFound {
			return nil, errors.New("name field was not found in service")
		}
		if nameOfCurrentService == serviceName {
			selectedService = typedService
			break
		}
	}
	return selectedService, nil
}

func getBackendRefs(rules []interface{}) ([]interface{}, error) {
	for _, rule := range rules {
		typedRule, ok := rule.(map[string]interface{})
		if !ok {
			return nil, errors.New("Failed type assertion setting rule for http route")
		}
		backendRefs, isFound, err := unstructured.NestedSlice(typedRule, "backendRefs")
		if err != nil {
			return nil, err
		}
		if !isFound {
			continue
		}
		return backendRefs, nil
	}
	return nil, nil
}

func mergeBackendRefs(rules, backendRefs []interface{}) ([]interface{}, error) {
	for _, rule := range rules {
		typedRule, ok := rule.(map[string]interface{})
		if !ok {
			return nil, errors.New("Failed type assertion setting rule for http route")
		}
		_, isFound, err := unstructured.NestedSlice(typedRule, "backendRefs")
		if err != nil {
			return nil, err
		}
		if !isFound {
			continue
		}
		err = unstructured.SetNestedSlice(typedRule, backendRefs, "backendRefs")
		if err != nil {
			return nil, err
		}
		return rules, nil
	}
	return rules, nil
}

func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	return nil, nil
}

func (r *Reconciler) Type() string {
	return Type
}