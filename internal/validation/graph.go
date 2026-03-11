// Package validation provides pipeline graph validation (acyclicity, reference resolution).
package validation

import "fmt"

// PipelineDef represents a pipeline for graph validation purposes.
type PipelineDef struct {
	Name           string
	SourcePipeline string
	SinkPipelines  []string
}

// ValidateGraph checks that the pipeline graph is acyclic and all references resolve.
func ValidateGraph(pipelines []PipelineDef) error {
	names := map[string]bool{}
	for _, p := range pipelines {
		if names[p.Name] {
			return fmt.Errorf("duplicate pipeline: %s", p.Name)
		}
		names[p.Name] = true
	}
	for _, p := range pipelines {
		if p.SourcePipeline != "" && !names[p.SourcePipeline] {
			return fmt.Errorf("%q: unknown source %q", p.Name, p.SourcePipeline)
		}
		for _, s := range p.SinkPipelines {
			if !names[s] {
				return fmt.Errorf("%q: unknown sink %q", p.Name, s)
			}
		}
	}
	adj := map[string][]string{}
	for _, p := range pipelines {
		adj[p.Name] = append(adj[p.Name], p.SinkPipelines...)
	}
	color := map[string]int{}
	var dfs func(string) error
	dfs = func(n string) error {
		color[n] = 1
		for _, next := range adj[n] {
			if color[next] == 1 {
				return fmt.Errorf("cycle: %s -> %s", n, next)
			}
			if color[next] == 0 {
				if err := dfs(next); err != nil {
					return err
				}
			}
		}
		color[n] = 2
		return nil
	}
	for _, p := range pipelines {
		if color[p.Name] == 0 {
			if err := dfs(p.Name); err != nil {
				return err
			}
		}
	}
	return nil
}
