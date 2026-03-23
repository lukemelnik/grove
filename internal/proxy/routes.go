package proxy

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"github.com/lukemelnik/grove/internal/config"
	"github.com/lukemelnik/grove/internal/ports"
	"github.com/lukemelnik/grove/internal/worktree"
)

type Route struct {
	Hostname string
	Target   string
	Project  string
	Service  string
	Branch   string
}

type RouteTable struct {
	mu     sync.RWMutex
	routes map[string]Route
}

func NewRouteTable() *RouteTable {
	return &RouteTable{
		routes: make(map[string]Route),
	}
}

func (rt *RouteTable) Lookup(hostname string) (Route, bool) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	r, ok := rt.routes[hostname]
	return r, ok
}

func (rt *RouteTable) Update(routes []Route) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	m := make(map[string]Route, len(routes))
	for _, r := range routes {
		m[r.Hostname] = r
	}
	rt.routes = m
}

func (rt *RouteTable) All() []Route {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	result := make([]Route, 0, len(rt.routes))
	for _, r := range rt.routes {
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Hostname < result[j].Hostname
	})
	return result
}

func ComputeAllRoutes(entries []ProjectEntry) ([]Route, error) {
	var allRoutes []Route

	for _, entry := range entries {
		routes, err := computeProjectRoutes(entry)
		if err != nil {
			continue
		}
		allRoutes = append(allRoutes, routes...)
	}

	sort.Slice(allRoutes, func(i, j int) bool {
		return allRoutes[i].Hostname < allRoutes[j].Hostname
	})

	return allRoutes, nil
}

func computeProjectRoutes(entry ProjectEntry) ([]Route, error) {
	configPath := filepath.Join(entry.Path, config.ConfigFileName)
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config for %s: %w", entry.Path, err)
	}

	if cfg.Proxy == nil {
		return nil, nil
	}

	projectName := cfg.Proxy.Name
	if projectName == "" {
		projectName = entry.Name
	}

	git := worktree.NewGitRunner(entry.Path)
	wtMgr := worktree.NewManager(git, entry.Path, cfg.WorktreeDir)

	worktrees, err := wtMgr.List()
	if err != nil {
		return nil, fmt.Errorf("listing worktrees for %s: %w", entry.Path, err)
	}

	defaultBranch := wtMgr.DefaultBranch()

	var routes []Route
	for _, wt := range worktrees {
		if wt.IsBare || wt.Branch == "" {
			continue
		}

		if len(cfg.Services) == 0 {
			continue
		}

		assignment, err := ports.Assign(cfg.Services, wt.Branch, ports.DefaultMaxOffset, defaultBranch)
		if err != nil {
			continue
		}

		serviceNames := make([]string, 0, len(assignment.Ports))
		for name := range assignment.Ports {
			serviceNames = append(serviceNames, name)
		}
		sort.Strings(serviceNames)

		for _, svcName := range serviceNames {
			port := assignment.Ports[svcName]

			hostname, err := BuildHostname(svcName, wt.Branch, projectName, defaultBranch)
			if err != nil {
				continue
			}

			routes = append(routes, Route{
				Hostname: hostname,
				Target:   fmt.Sprintf("127.0.0.1:%d", port),
				Project:  projectName,
				Service:  svcName,
				Branch:   wt.Branch,
			})
		}
	}

	return routes, nil
}
