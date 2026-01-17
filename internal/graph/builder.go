package graph

import (
	"fmt"
	"strconv"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/render"
)

// BuildFromConfig builds a topology from kbox configuration
func BuildFromConfig(cfg *config.AppConfig) (*Topology, error) {
	renderer := render.New(cfg)
	bundle, err := renderer.Render()
	if err != nil {
		return nil, fmt.Errorf("failed to render: %w", err)
	}
	return BuildFromBundle(cfg.Metadata.Name, renderer.Namespace(), bundle, nil)
}

// BuildFromMultiConfig builds a topology from multi-service configuration
func BuildFromMultiConfig(cfg *config.MultiServiceConfig) (*Topology, error) {
	renderer := render.NewMultiService(cfg)
	bundle, err := renderer.Render()
	if err != nil {
		return nil, fmt.Errorf("failed to render: %w", err)
	}
	return BuildFromBundle(cfg.Metadata.Name, renderer.Namespace(), bundle, cfg)
}

// BuildFromBundle builds a topology from a rendered Bundle
func BuildFromBundle(appName, namespace string, bundle *render.Bundle, multiCfg *config.MultiServiceConfig) (*Topology, error) {
	t := NewTopology(appName, namespace)

	// Add deployment nodes
	deployments := bundle.Deployments
	if len(deployments) == 0 && bundle.Deployment != nil {
		deployments = append(deployments, bundle.Deployment)
	}

	for _, dep := range deployments {
		image := ""
		if len(dep.Spec.Template.Spec.Containers) > 0 {
			image = dep.Spec.Template.Spec.Containers[0].Image
		}

		replicas := int32(1)
		if dep.Spec.Replicas != nil {
			replicas = *dep.Spec.Replicas
		}

		node := &Node{
			ID:   fmt.Sprintf("deployment/%s", dep.Name),
			Name: dep.Name,
			Type: NodeTypeDeployment,
			Labels: map[string]string{
				"app": dep.Name,
			},
			Metadata: map[string]string{
				"image":    image,
				"replicas": strconv.Itoa(int(replicas)),
			},
		}
		t.AddNode(node)
	}

	// Add service nodes and connect to deployments
	for _, svc := range bundle.Services {
		port := ""
		if len(svc.Spec.Ports) > 0 {
			port = strconv.Itoa(int(svc.Spec.Ports[0].Port))
		}

		node := &Node{
			ID:   fmt.Sprintf("service/%s", svc.Name),
			Name: svc.Name,
			Type: NodeTypeService,
			Labels: map[string]string{
				"app": svc.Name,
			},
			Metadata: map[string]string{
				"port": port,
				"type": string(svc.Spec.Type),
			},
		}
		t.AddNode(node)

		// Connect service to deployment via selector
		if appLabel, ok := svc.Spec.Selector["app"]; ok {
			depID := fmt.Sprintf("deployment/%s", appLabel)
			if t.HasNode(depID) {
				t.AddEdge(node.ID, depID, EdgeTypeExposes, port)
			}
		}
	}

	// Add ingress nodes and connect to services
	for _, ing := range bundle.Ingresses {
		hosts := ""
		if len(ing.Spec.Rules) > 0 {
			hosts = ing.Spec.Rules[0].Host
		}

		node := &Node{
			ID:   fmt.Sprintf("ingress/%s", ing.Name),
			Name: ing.Name,
			Type: NodeTypeIngress,
			Metadata: map[string]string{
				"host": hosts,
			},
		}
		t.AddNode(node)

		// Connect ingress to service
		for _, rule := range ing.Spec.Rules {
			if rule.HTTP != nil {
				for _, path := range rule.HTTP.Paths {
					if path.Backend.Service != nil {
						svcID := fmt.Sprintf("service/%s", path.Backend.Service.Name)
						if t.HasNode(svcID) {
							label := rule.Host
							if path.Path != "/" && path.Path != "" {
								label = rule.Host + path.Path
							}
							t.AddEdge(node.ID, svcID, EdgeTypeRoutes, label)
						}
					}
				}
			}
		}
	}

	// Add StatefulSet nodes (dependencies like postgres, redis)
	for _, ss := range bundle.StatefulSets {
		depType := ""
		if labels := ss.Labels; labels != nil {
			depType = labels["kbox.dev/dependency"]
		}

		image := ""
		if len(ss.Spec.Template.Spec.Containers) > 0 {
			image = ss.Spec.Template.Spec.Containers[0].Image
		}

		node := &Node{
			ID:   fmt.Sprintf("statefulset/%s", ss.Name),
			Name: ss.Name,
			Type: NodeTypeStatefulSet,
			Labels: map[string]string{
				"kbox.dev/dependency": depType,
			},
			Metadata: map[string]string{
				"type":  depType,
				"image": image,
			},
		}
		t.AddNode(node)

		// Connect main deployment(s) to this dependency
		for _, dep := range deployments {
			depID := fmt.Sprintf("deployment/%s", dep.Name)
			t.AddEdge(depID, node.ID, EdgeTypeUses, depType)
		}
	}

	// Add PVC nodes
	for _, pvc := range bundle.PersistentVolumeClaims {
		storage := ""
		if pvc.Spec.Resources.Requests != nil {
			if qty, ok := pvc.Spec.Resources.Requests["storage"]; ok {
				storage = qty.String()
			}
		}

		node := &Node{
			ID:   fmt.Sprintf("pvc/%s", pvc.Name),
			Name: pvc.Name,
			Type: NodeTypePVC,
			Metadata: map[string]string{
				"storage": storage,
			},
		}
		t.AddNode(node)
	}

	// Add Job nodes
	for _, job := range bundle.Jobs {
		node := &Node{
			ID:   fmt.Sprintf("job/%s", job.Name),
			Name: job.Name,
			Type: NodeTypeJob,
		}
		t.AddNode(node)
	}

	// Add CronJob nodes
	for _, cj := range bundle.CronJobs {
		node := &Node{
			ID:   fmt.Sprintf("cronjob/%s", cj.Name),
			Name: cj.Name,
			Type: NodeTypeCronJob,
			Metadata: map[string]string{
				"schedule": cj.Spec.Schedule,
			},
		}
		t.AddNode(node)
	}

	// Handle multi-service DependsOn relationships
	if multiCfg != nil {
		for svcName, svcSpec := range multiCfg.Services {
			fromID := fmt.Sprintf("deployment/%s-%s", appName, svcName)
			for _, depName := range svcSpec.DependsOn {
				toID := fmt.Sprintf("deployment/%s-%s", appName, depName)
				if t.HasNode(fromID) && t.HasNode(toID) {
					t.AddEdge(fromID, toID, EdgeTypeDependsOn, "")
				}
			}
		}
	}

	// Compute layers for rendering
	t.ComputeLayers()

	return t, nil
}
