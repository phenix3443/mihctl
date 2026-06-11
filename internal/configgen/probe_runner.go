package configgen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultProbeTimeout = 5 * time.Second
	latencyProbeTimeout = 2 * time.Second
)

var execLookPath = exec.LookPath
var startLocalProxyFunc = startLocalProxy
var requestViaProxyAddrFunc = httpRequestViaProxyAddr
var startMihomoProbeRuntimeFunc = startMihomoProbeRuntime
var runtimeHTTPRequestViaNodeFunc = func(r *mihomoProbeRuntime, nodeName, method, requestURL string, headers map[string]string, body io.Reader, timeout time.Duration) (*http.Response, []byte, error) {
	return r.httpRequestViaNode(nodeName, method, requestURL, headers, body, timeout)
}
var runtimeNumCPU = runtime.NumCPU
var probeProgressReporter = func(ProbeProgressEvent) {}

const (
	ProbeModeDefault = ""
	ProbeModeService = "service"
)

type ProbeScope struct {
	Providers        []string
	Services         []string
	PolicyPath       string
	StopOnMinSuccess bool
	LatencyBaselines map[string]int
	Mode             string
}

type ProbeRunServiceSummary struct {
	Service    string
	Candidates int
	Success    int
	Failed     int
	Skipped    int
}

type ProbeRunSummary struct {
	Mode     string
	Services []ProbeRunServiceSummary
}

type ProbeProgressEvent struct {
	Profile   string
	Group     string
	Service   string
	Completed int
	Total     int
	Success   int
	Failed    int
	Skipped   int
}

type probeRuntimeConfig struct {
	GroupName string
	Rule      string
	Mode      string
	LogLevel  string
	AllowLAN  bool
	DNSEnable bool
}

var defaultProbeRuntimeConfig = probeRuntimeConfig{
	GroupName: "__probe__",
	Rule:      "MATCH,__probe__",
	Mode:      "rule",
	LogLevel:  "silent",
	AllowLAN:  false,
	DNSEnable: false,
}

type groupProbePlan struct {
	profileName          string
	groupName            string
	groupSpec            ServiceGroupSpec
	groupDigest          string
	probeDigest          string
	candidates           []GroupCandidate
	candidatesByProvider map[string][]GroupCandidate
	groupState           GroupProbeState
	completedCount       int
	successCount         int
	failedCount          int
}

func SetProbeProgressReporter(reporter func(ProbeProgressEvent)) func() {
	previous := probeProgressReporter
	if reporter == nil {
		probeProgressReporter = func(ProbeProgressEvent) {}
	} else {
		probeProgressReporter = reporter
	}
	return func() {
		probeProgressReporter = previous
	}
}

type probeTask struct {
	nodeName    string
	serviceName string
	runtime     *mihomoProbeRuntime
	proxyConfig map[string]any
}

func serviceCandidateNodeNames(serviceCandidates map[string][]ParsedProxy) map[string]map[string]struct{} {
	names := make(map[string]map[string]struct{}, len(serviceCandidates))
	for serviceName, proxies := range serviceCandidates {
		nodes := make(map[string]struct{}, len(proxies))
		for _, proxy := range proxies {
			nodes[proxy.Name] = struct{}{}
		}
		names[serviceName] = nodes
	}
	return names
}

func reportProbeProgress(profileName, groupName, serviceName string, completed, total, success, failed, skipped int) {
	probeProgressReporter(ProbeProgressEvent{
		Profile:   profileName,
		Group:     groupName,
		Service:   serviceName,
		Completed: completed,
		Total:     total,
		Success:   success,
		Failed:    failed,
		Skipped:   skipped,
	})
}

func appendUniqueServiceCandidate(target map[string]map[string][]ParsedProxy, seen map[string]map[string]map[string]struct{}, providerName, serviceName string, proxy ParsedProxy) {
	serviceCandidates, ok := target[providerName]
	if !ok {
		serviceCandidates = map[string][]ParsedProxy{}
		target[providerName] = serviceCandidates
	}
	serviceSeen, ok := seen[providerName]
	if !ok {
		serviceSeen = map[string]map[string]struct{}{}
		seen[providerName] = serviceSeen
	}
	nodeSeen, ok := serviceSeen[serviceName]
	if !ok {
		nodeSeen = map[string]struct{}{}
		serviceSeen[serviceName] = nodeSeen
	}
	if _, ok := nodeSeen[proxy.Name]; ok {
		return
	}
	nodeSeen[proxy.Name] = struct{}{}
	serviceCandidates[serviceName] = append(serviceCandidates[serviceName], proxy)
}

func applyProviderProbeResults(providerState ProviderProbeState, serviceCandidates map[string][]ParsedProxy, results map[string]map[string]ServiceProbeState) ProviderProbeState {
	if providerState.Nodes == nil {
		providerState.Nodes = map[string]NodeProbeState{}
	}
	candidateNodesByService := serviceCandidateNodeNames(serviceCandidates)
	proxiesByName := map[string]ParsedProxy{}
	for _, proxies := range serviceCandidates {
		for _, proxy := range proxies {
			proxiesByName[proxy.Name] = proxy
		}
	}
	for nodeName, proxy := range proxiesByName {
		nodeState := providerState.Nodes[nodeName]
		nodeState.NodeName = proxy.Name
		if nodeState.Services == nil {
			nodeState.Services = map[string]ServiceProbeState{}
		}
		for serviceName, candidateNodes := range candidateNodesByService {
			if _, ok := candidateNodes[nodeName]; !ok {
				continue
			}
			serviceState, ok := results[nodeName][serviceName]
			if !ok {
				delete(nodeState.Services, serviceName)
				continue
			}
			nodeState.Services[serviceName] = serviceState
		}
		if len(nodeState.Services) == 0 {
			delete(providerState.Nodes, nodeName)
			continue
		}
		providerState.Nodes[nodeName] = nodeState
	}
	return providerState
}

func RunProbe(repoRoot, statePath string, cfg *GenerationConfig, scope ProbeScope) error {
	selectedProviderSpecs, err := selectProviderSpecs(cfg.ProxyProviders, scope.Providers)
	if err != nil {
		return err
	}
	catalog, err := LoadProviderCatalog(repoRoot, selectedProviderSpecs)
	if err != nil {
		return err
	}
	state, err := LoadProbeState(statePath)
	if err != nil {
		return err
	}

	selectedProviders := scope.Providers
	if len(selectedProviders) == 0 {
		for name := range catalog.Providers {
			selectedProviders = append(selectedProviders, name)
		}
	}
	selectedProviders = sortUniqueStrings(selectedProviders)

	selectedServices := sortUniqueStrings(scope.Services)
	now := time.Now().UTC()
	serviceDigests, err := BuildProbeServiceDigests(cfg)
	if err != nil {
		return err
	}
	_ = selectedProviders
	_ = selectedServices
	return runGroupProbe(statePath, cfg, catalog, state, serviceDigests, now, scope)
}

func runGroupProbe(statePath string, cfg *GenerationConfig, catalog *ProviderCatalog, state *ProbeState, serviceDigests map[string]string, now time.Time, scope ProbeScope) error {
	if state.Groups == nil {
		state.Groups = map[string]GroupProbeState{}
	}
	targetGroups, err := resolveTargetGroups(cfg, scope.Services)
	if err != nil {
		return err
	}
	selectedProviders := map[string]struct{}{}
	for _, providerName := range scope.Providers {
		selectedProviders[providerName] = struct{}{}
	}
	providerStates := map[string]ProviderProbeState{}
	providerServiceCandidates := map[string]map[string][]ParsedProxy{}
	providerServiceSeen := map[string]map[string]map[string]struct{}{}
	plans := []groupProbePlan{}
	planIndexesByProvider := map[string][]int{}

	for _, profileName := range probeProfileNames(cfg) {
		for _, groupName := range targetGroups {
			groupSpec, ok := cfg.ServiceGroups[groupName]
			if !ok {
				continue
			}
			candidates, groupProfile, err := groupCandidatesForProfile(profileName, groupName, groupSpec, cfg, catalog, selectedProviders)
			if err != nil {
				return err
			}
			if len(candidates) == 0 {
				delete(state.Groups, groupStateKey(profileName, groupName))
				continue
			}
			probeService := groupProbeService(groupSpec)
			probeDigest, ok := serviceDigests[probeService]
			if !ok {
				return fmt.Errorf("probe digest for service %q is not defined", probeService)
			}
			groupDigest := groupProbeDigest(profileName, groupName, groupSpec, groupProfile, probeDigest)
			groupedCandidates := groupCandidatesByProvider(candidates)
			for _, candidate := range candidates {
				providerState, ok := providerStates[candidate.Provider]
				if !ok {
					providerState, ok = state.Providers[candidate.Provider]
					if !ok || providerState.SubscriptionDigest != candidate.SubscriptionDigest {
						providerState = ProviderProbeState{
							Provider:           candidate.Provider,
							SubscriptionDigest: candidate.SubscriptionDigest,
							Nodes:              map[string]NodeProbeState{},
						}
					}
					if providerState.Nodes == nil {
						providerState.Nodes = map[string]NodeProbeState{}
					}
					providerStates[candidate.Provider] = providerState
				}
				appendUniqueServiceCandidate(providerServiceCandidates, providerServiceSeen, candidate.Provider, probeService, candidate.Proxy)
			}
			plan := groupProbePlan{
				profileName:          profileName,
				groupName:            groupName,
				groupSpec:            groupSpec,
				groupDigest:          groupDigest,
				probeDigest:          probeDigest,
				candidates:           candidates,
				candidatesByProvider: groupedCandidates,
				groupState: GroupProbeState{
					Group:           groupName,
					Profile:         profileName,
					Probe:           probeService,
					Digest:          groupDigest,
					ProviderDigests: map[string]string{},
					Nodes:           map[string]GroupNodeState{},
				},
			}
			for _, candidate := range candidates {
				plan.groupState.ProviderDigests[candidate.Provider] = candidate.SubscriptionDigest
			}
			reportProbeProgress(profileName, groupName, probeService, 0, len(candidates), 0, 0, 0)
			plans = append(plans, plan)
			planIndex := len(plans) - 1
			for providerName := range groupedCandidates {
				planIndexesByProvider[providerName] = append(planIndexesByProvider[providerName], planIndex)
			}
		}
	}

	for _, providerName := range orderedProviderNames(cfg) {
		serviceCandidates := providerServiceCandidates[providerName]
		if len(serviceCandidates) == 0 {
			continue
		}
		snapshot, ok := catalog.Providers[providerName]
		if !ok {
			return fmt.Errorf("unknown provider for probe: %s", providerName)
		}
		allowedNodes := map[string]struct{}{}
		for _, proxies := range serviceCandidates {
			for _, proxy := range proxies {
				allowedNodes[proxy.Name] = struct{}{}
			}
		}
		subset := snapshot
		subset.Proxies = make([]ParsedProxy, 0, len(allowedNodes))
		for _, proxy := range snapshot.Proxies {
			if _, ok := allowedNodes[proxy.Name]; !ok {
				continue
			}
			subset.Proxies = append(subset.Proxies, proxy)
		}
		results, err := probeProviderSnapshot(subset, serviceCandidates, providerStates[providerName], cfg, serviceDigests, now, ProbeScope{
			Mode: ProbeModeService,
		})
		if err != nil {
			return err
		}
		providerState := applyProviderProbeResults(providerStates[providerName], serviceCandidates, results)
		providerStates[providerName] = providerState

		for _, planIndex := range planIndexesByProvider[providerName] {
			plan := &plans[planIndex]
			for _, candidate := range plan.candidatesByProvider[providerName] {
				plan.completedCount++
				probeService := groupProbeService(plan.groupSpec)
				serviceState, ok := results[candidate.Proxy.Name][probeService]
				if !ok {
					serviceState = failedProbeState(fmt.Errorf("missing probe result for %s", candidate.Proxy.Name), plan.probeDigest, now)
				}
				if serviceState.OK {
					plan.groupState.Nodes[candidate.Proxy.Name] = GroupNodeState{
						Provider: providerName,
						NodeName: candidate.Proxy.Name,
						Service:  serviceState,
					}
					plan.successCount++
				} else {
					plan.failedCount++
				}
			}
			reportProbeProgress(plan.profileName, plan.groupName, groupProbeService(plan.groupSpec), plan.completedCount, len(plan.candidates), plan.successCount, plan.failedCount, 0)
		}
	}

	for _, plan := range plans {
		state.Groups[groupStateKey(plan.profileName, plan.groupName)] = plan.groupState
	}
	for providerName, providerState := range providerStates {
		state.Providers[providerName] = providerState
	}
	return SaveProbeState(statePath, state)
}

func SummarizeProbeRun(repoRoot, statePath string, cfg *GenerationConfig, scope ProbeScope) (ProbeRunSummary, error) {
	selectedProviderSpecs, err := selectProviderSpecs(cfg.ProxyProviders, scope.Providers)
	if err != nil {
		return ProbeRunSummary{}, err
	}
	catalog, err := LoadProviderCatalog(repoRoot, selectedProviderSpecs)
	if err != nil {
		return ProbeRunSummary{}, err
	}
	state, err := LoadProbeState(statePath)
	if err != nil {
		return ProbeRunSummary{}, err
	}
	serviceDigests, err := BuildProbeServiceDigests(cfg)
	if err != nil {
		return ProbeRunSummary{}, err
	}
	return summarizeGroupProbeRun(catalog, state, cfg, serviceDigests, scope)
}

func summarizeGroupProbeRun(catalog *ProviderCatalog, state *ProbeState, cfg *GenerationConfig, serviceDigests map[string]string, scope ProbeScope) (ProbeRunSummary, error) {
	targetGroups, err := resolveTargetGroups(cfg, scope.Services)
	if err != nil {
		return ProbeRunSummary{}, err
	}
	selectedProviders := map[string]struct{}{}
	for _, providerName := range scope.Providers {
		selectedProviders[providerName] = struct{}{}
	}
	serviceTotals := map[string]*ProbeRunServiceSummary{}

	for _, profileName := range probeProfileNames(cfg) {
		for _, groupName := range targetGroups {
			groupSpec, ok := cfg.ServiceGroups[groupName]
			if !ok {
				continue
			}
			candidates, groupProfile, err := groupCandidatesForProfile(profileName, groupName, groupSpec, cfg, catalog, selectedProviders)
			if err != nil {
				return ProbeRunSummary{}, err
			}
			if len(candidates) == 0 {
				continue
			}
			probeService := groupProbeService(groupSpec)
			probeDigest, ok := serviceDigests[probeService]
			if !ok {
				return ProbeRunSummary{}, fmt.Errorf("probe digest for service %q is not defined", probeService)
			}
			groupDigest := groupProbeDigest(profileName, groupName, groupSpec, groupProfile, probeDigest)
			groupState, ok := state.LookupGroup(profileName, groupName, groupDigest)
			if !ok {
				return ProbeRunSummary{}, fmt.Errorf(
					"missing fresh group probe state for %s on %s (probe=%s)",
					groupName,
					profileName,
					probeService,
				)
			}

			total := serviceTotals[probeService]
			if total == nil {
				total = &ProbeRunServiceSummary{Service: probeService}
				serviceTotals[probeService] = total
			}
			total.Candidates += len(candidates)
			success := 0
			for _, candidate := range candidates {
				selectedNode, ok := groupState.Nodes[candidate.Proxy.Name]
				if ok && selectedNode.Provider == candidate.Provider && selectedNode.Service.OK {
					success++
				}
			}
			total.Success += success
			total.Failed += len(candidates) - success
		}
	}

	serviceNames := make([]string, 0, len(serviceTotals))
	for serviceName := range serviceTotals {
		serviceNames = append(serviceNames, serviceName)
	}
	sort.Strings(serviceNames)
	summary := ProbeRunSummary{Mode: ProbeModeService, Services: make([]ProbeRunServiceSummary, 0, len(serviceNames))}
	for _, serviceName := range serviceNames {
		summary.Services = append(summary.Services, *serviceTotals[serviceName])
	}
	return summary, nil
}

func filterProbeSnapshotForServices(snapshot ProviderSnapshot, providerName string, services []string, cfg *GenerationConfig) (ProviderSnapshot, error) {
	filtered := snapshot
	proxies := make([]ParsedProxy, 0, len(snapshot.Proxies))
	for _, proxy := range snapshot.Proxies {
		include := len(services) == 0
		for _, serviceName := range services {
			matched, err := proxyEligibleForService(providerName, proxy.Name, serviceName, cfg)
			if err != nil {
				return ProviderSnapshot{}, err
			}
			if matched {
				include = true
				break
			}
		}
		if include {
			proxies = append(proxies, proxy)
		}
	}
	filtered.Proxies = proxies
	return filtered, nil
}

func proxyEligibleForService(providerName, proxyName, serviceName string, cfg *GenerationConfig) (bool, error) {
	groupSpec, ok := cfg.ServiceGroups[serviceName]
	if !ok {
		return true, nil
	}

	allowedProvider := false
	for _, profileName := range cfg.ProfileOrder {
		profileSpec, ok := cfg.Profiles[profileName]
		if !ok {
			continue
		}
		_ = profileSpec
		groupProfile, ok := resolveServiceGroupProfile(groupSpec, profileName)
		if !ok {
			continue
		}
		if len(groupProfile.Providers) > 0 && !containsString(groupProfile.Providers, providerName) {
			continue
		}

		matchers, excluders, err := providerRegexps(groupProfile, providerName)
		if err != nil {
			return false, fmt.Errorf("compile provider filters for service %s/%s: %w", serviceName, providerName, err)
		}
		groupMatchers, err := compileRegexps(groupSpec.Match)
		if err != nil {
			return false, fmt.Errorf("compile group match regex for %s: %w", serviceName, err)
		}
		groupExcluders, err := compileRegexps(groupSpec.Exclude)
		if err != nil {
			return false, fmt.Errorf("compile group exclude regex for %s: %w", serviceName, err)
		}
		if !matchesProxyName(proxyName, groupMatchers, groupExcluders) {
			continue
		}
		if !matchesProxyName(proxyName, matchers, excluders) {
			continue
		}
		allowedProvider = true
		break
	}
	return allowedProvider, nil
}

func allProbeServices(cfg *GenerationConfig) []string {
	services := make([]string, 0, len(cfg.Probe.Services))
	for name := range cfg.Probe.Services {
		services = append(services, name)
	}
	return sortUniqueStrings(services)
}

func probeProviderSnapshot(snapshot ProviderSnapshot, serviceCandidates map[string][]ParsedProxy, providerState ProviderProbeState, cfg *GenerationConfig, serviceDigests map[string]string, now time.Time, scope ProbeScope) (map[string]map[string]ServiceProbeState, error) {
	_ = providerState
	_ = scope
	results := make(map[string]map[string]ServiceProbeState, len(snapshot.Proxies))
	serviceNames := make([]string, 0, len(serviceCandidates))
	for serviceName := range serviceCandidates {
		serviceNames = append(serviceNames, serviceName)
	}
	sort.Strings(serviceNames)

	serviceSpecs := make(map[string]ProbeServiceSpec, len(serviceNames))
	needsTempRuntime := len(serviceNames) > 0
	for _, serviceName := range serviceNames {
		spec, ok := cfg.Probe.Services[serviceName]
		if !ok {
			return nil, fmt.Errorf("probe service %q is not defined", serviceName)
		}
		if _, ok := serviceDigests[serviceName]; !ok {
			return nil, fmt.Errorf("probe digest for service %q is not defined", serviceName)
		}
		serviceSpecs[serviceName] = spec
		if _, err := url.Parse(spec.URI); err != nil {
			return nil, err
		}
	}

	var tempRuntime *mihomoProbeRuntime
	tempRuntimes := []*mihomoProbeRuntime{}
	var err error
	if needsTempRuntime {
		tempRuntime, err = startMihomoProbeRuntimeFunc(snapshot)
	}
	if needsTempRuntime && err != nil {
		for _, proxy := range snapshot.Proxies {
			nodeResults := map[string]ServiceProbeState{}
			for _, serviceName := range serviceNames {
				nodeResults[serviceName] = failedProbeState(err, serviceDigests[serviceName], now)
			}
			results[proxy.Name] = nodeResults
		}
		return results, nil
	}
	if tempRuntime != nil {
		tempRuntimes = append(tempRuntimes, tempRuntime)
	}
	defer func() {
		for _, runtime := range tempRuntimes {
			runtime.Close()
		}
	}()

	runtimeWorkerCount := 0
	if len(snapshot.Proxies) > 0 && needsTempRuntime {
		runtimeWorkerCount = directProbeWorkerCount(len(snapshot.Proxies))
	}
	for len(tempRuntimes) < runtimeWorkerCount {
		runtime, runtimeErr := startMihomoProbeRuntimeFunc(snapshot)
		if runtimeErr != nil {
			break
		}
		tempRuntimes = append(tempRuntimes, runtime)
	}

	for _, proxy := range snapshot.Proxies {
		results[proxy.Name] = map[string]ServiceProbeState{}
	}
	for _, serviceName := range serviceNames {
		candidates := serviceCandidates[serviceName]
		batchTasks := make([]probeTask, 0, len(candidates))
		for _, proxy := range candidates {
			if tempRuntime == nil {
				results[proxy.Name][serviceName] = failedProbeState(fmt.Errorf("no probe runtime available for service %q", serviceName), serviceDigests[serviceName], now)
				continue
			}
			if _, ok := tempRuntime.availableNodes[proxy.Name]; !ok {
				results[proxy.Name][serviceName] = failedProbeState(fmt.Errorf("node %q not found in temp mihomo probe runtime", proxy.Name), serviceDigests[serviceName], now)
				continue
			}
			batchTasks = append(batchTasks, probeTask{
				nodeName:    proxy.Name,
				serviceName: serviceName,
				runtime:     tempRuntime,
			})
		}
		if err := executeProbeBatch(results, batchTasks, serviceSpecs, serviceDigests, tempRuntimes, now); err != nil {
			return nil, err
		}
	}

	return results, nil
}

func executeProbeBatch(results map[string]map[string]ServiceProbeState, tasks []probeTask, serviceSpecs map[string]ProbeServiceSpec, serviceDigests map[string]string, tempRuntimes []*mihomoProbeRuntime, now time.Time) error {
	if len(tasks) == 0 {
		return nil
	}
	if len(tempRuntimes) == 0 {
		return fmt.Errorf("no probe runtime available for service probes")
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	taskCh := make(chan probeTask)
	for _, runtime := range tempRuntimes {
		wg.Add(1)
		go func(runtime *mihomoProbeRuntime) {
			defer wg.Done()
			for task := range taskCh {
				serviceState := probeNodeService(runtime, task.nodeName, serviceSpecs[task.serviceName].URI, serviceDigests[task.serviceName], now)
				mu.Lock()
				results[task.nodeName][task.serviceName] = serviceState
				mu.Unlock()
			}
		}(runtime)
	}
	for _, task := range tasks {
		taskCh <- task
	}
	close(taskCh)
	wg.Wait()
	return nil
}

func directProbeWorkerCount(taskCount int) int {
	if taskCount <= 0 {
		return 0
	}
	workerCount := runtimeNumCPU()
	if workerCount < 4 {
		workerCount = 4
	}
	if workerCount > 8 {
		workerCount = 8
	}
	if taskCount < workerCount {
		return taskCount
	}
	return workerCount
}

func probeNodeService(runtime *mihomoProbeRuntime, nodeName, uri, probeDigest string, now time.Time) ServiceProbeState {
	state := ServiceProbeState{
		OK:          false,
		ProbeDigest: probeDigest,
		ProbedAt:    now.Format(time.RFC3339),
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		state.Error = err.Error()
		return state
	}

	started := time.Now()
	switch strings.ToLower(parsed.Scheme) {
	case "ssh", "tcp":
		if err := runtime.selectNode(nodeName); err != nil {
			state.Error = err.Error()
			return state
		}
		latency, err := probeSocketThroughSOCKS5(runtime.mixedProxyAddr, parsed.Hostname(), resolvePort(parsed), strings.ToLower(parsed.Scheme) == "ssh")
		if err != nil {
			state.Error = err.Error()
			return state
		}
		state.LatencyMillis = latency
	default:
		return probeNodeServiceViaRuntimeRequest(runtime, nodeName, uri, probeDigest, now, defaultProbeTimeout)
	}

	state.OK = true
	if state.LatencyMillis == 0 {
		state.LatencyMillis = int(time.Since(started).Milliseconds())
	}
	return state
}

func probeNodeServiceViaRuntimeRequest(runtime *mihomoProbeRuntime, nodeName, uri, probeDigest string, now time.Time, timeout time.Duration) ServiceProbeState {
	state := baseProbeState(probeDigest, now)
	parsed, err := url.Parse(uri)
	if err != nil {
		state.Error = err.Error()
		return state
	}
	started := time.Now()
	resp, _, err := runtimeHTTPRequestViaNodeFunc(runtime, nodeName, http.MethodGet, parsed.String(), nil, nil, timeout)
	if err != nil {
		state.Error = err.Error()
		return state
	}
	defer resp.Body.Close()
	return classifyGenericHTTPProbeResult(resp.StatusCode, probeDigest, now, int(time.Since(started).Milliseconds()))
}

func probeNodeServiceDirect(proxyConfig map[string]any, uri, probeDigest string, now time.Time) ServiceProbeState {
	if strings.EqualFold(stringValue(proxyConfig["type"]), "direct") {
		return probeNodeServiceViaProxyAddr("", uri, probeDigest, now, defaultProbeTimeout)
	}

	localPort, err := allocateLocalPort()
	state := baseProbeState(probeDigest, now)
	if err != nil {
		state.Error = err.Error()
		return state
	}
	proc, err := startLocalProxyFunc(proxyConfig, localPort)
	if err != nil {
		state.Error = err.Error()
		return state
	}
	defer proc.Close()

	proxyAddr := fmt.Sprintf("127.0.0.1:%d", localPort)
	return probeNodeServiceViaProxyAddr(proxyAddr, uri, probeDigest, now, defaultProbeTimeout)
}

func probeNodeServiceViaProxyAddr(proxyAddr, uri, probeDigest string, now time.Time, timeout time.Duration) ServiceProbeState {
	state := baseProbeState(probeDigest, now)
	if _, err := url.Parse(uri); err != nil {
		state.Error = err.Error()
		return state
	}
	started := time.Now()
	resp, body, err := requestViaProxyAddrFunc(proxyAddr, http.MethodGet, uri, nil, nil, timeout)
	if err != nil {
		state.Error = err.Error()
		return state
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		_ = body
		return classifyGenericHTTPProbeResult(resp.StatusCode, probeDigest, now, int(time.Since(started).Milliseconds()))
	}
	_ = body
	return classifyGenericHTTPProbeResult(resp.StatusCode, probeDigest, now, int(time.Since(started).Milliseconds()))
}

func baseProbeState(probeDigest string, now time.Time) ServiceProbeState {
	return ServiceProbeState{
		OK:          false,
		ProbeDigest: probeDigest,
		ProbedAt:    now.Format(time.RFC3339),
	}
}

func classifyGenericHTTPProbeResult(statusCode int, probeDigest string, now time.Time, latencyMillis int) ServiceProbeState {
	state := baseProbeState(probeDigest, now)
	state.LatencyMillis = maxProbeLatency(latencyMillis)
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		state.OK = true
		state.Reason = "service-reachable-auth-required"
		return state
	}
	if statusCode >= 200 && statusCode < 400 {
		state.OK = true
		state.Reason = "service-reachable"
		return state
	}
	state.Error = fmt.Sprintf("unexpected http status: %d", statusCode)
	return state
}

func maxProbeLatency(value int) int {
	if value > 0 {
		return value
	}
	return 1
}

func failedProbeState(err error, probeDigest string, now time.Time) ServiceProbeState {
	state := ServiceProbeState{
		OK:          false,
		ProbeDigest: probeDigest,
		ProbedAt:    now.Format(time.RFC3339),
	}
	if err != nil {
		state.Error = err.Error()
	}
	return state
}

type mihomoProbeRuntime struct {
	command        *exec.Cmd
	dir            string
	controllerURL  string
	mixedProxyAddr string
	groupName      string
	secret         string
	availableNodes map[string]struct{}
	stderrBuffer   *bytes.Buffer
	selectMu       sync.Mutex
}

func (r *mihomoProbeRuntime) Close() {
	if r == nil {
		return
	}
	if r.command != nil && r.command.Process != nil {
		_ = r.command.Process.Kill()
		_, _ = r.command.Process.Wait()
	}
	if r.dir != "" {
		_ = os.RemoveAll(r.dir)
	}
}

func (r *mihomoProbeRuntime) checkDelay(nodeName, targetURL string) (int, error) {
	encodedNode := url.PathEscape(nodeName)
	reqURL := fmt.Sprintf("%s/proxies/%s/delay?timeout=5000&url=%s", r.controllerURL, encodedNode, url.QueryEscape(targetURL))
	resp, err := r.doRequest(http.MethodGet, reqURL, nil, defaultProbeTimeout+5*time.Second)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("delay api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Delay int `json:"delay"`
		Mean  int `json:"mean"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, err
	}
	if payload.Delay > 0 {
		return payload.Delay, nil
	}
	if payload.Mean > 0 {
		return payload.Mean, nil
	}
	return 0, fmt.Errorf("delay api returned no latency")
}

func (r *mihomoProbeRuntime) selectNode(nodeName string) error {
	if strings.TrimSpace(r.groupName) == "" {
		return fmt.Errorf("probe runtime has no selectable group for node %q", nodeName)
	}
	r.selectMu.Lock()
	defer r.selectMu.Unlock()

	payload := bytes.NewBuffer(nil)
	if err := json.NewEncoder(payload).Encode(map[string]string{"name": nodeName}); err != nil {
		return err
	}
	resp, err := r.doRequest(http.MethodPut, r.controllerURL+"/proxies/"+url.PathEscape(r.groupName), payload, defaultProbeTimeout)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("set group selection status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (r *mihomoProbeRuntime) listGroupNodes() ([]string, error) {
	resp, err := r.doRequest(http.MethodGet, r.controllerURL+"/proxies/"+url.PathEscape(r.groupName), nil, defaultProbeTimeout)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("get group nodes status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		All     []string `json:"all"`
		Proxies []struct {
			Name string `json:"name"`
		} `json:"proxies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if len(payload.All) > 0 {
		return payload.All, nil
	}
	names := make([]string, 0, len(payload.Proxies))
	for _, proxy := range payload.Proxies {
		if strings.TrimSpace(proxy.Name) != "" {
			names = append(names, proxy.Name)
		}
	}
	return names, nil
}

func (r *mihomoProbeRuntime) httpRequestViaNode(nodeName, method, requestURL string, headers map[string]string, body io.Reader, timeout time.Duration) (*http.Response, []byte, error) {
	if err := r.selectNode(nodeName); err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequest(method, requestURL, body)
	if err != nil {
		return nil, nil, err
	}
	for key, value := range headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Mozilla/5.0")
	}

	dialer := socks5Dialer{address: r.mixedProxyAddr, timeout: timeout}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: timeout,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		resp.Body.Close()
		return nil, nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	return resp, respBody, nil
}

func (r *mihomoProbeRuntime) doRequest(method, requestURL string, body io.Reader, timeout time.Duration) (*http.Response, error) {
	req, err := http.NewRequest(method, requestURL, body)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(r.secret) != "" {
		req.Header.Set("Authorization", "Bearer "+r.secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: timeout}
	return client.Do(req)
}

type liveMihomoConfig struct {
	ControllerURL  string
	MixedProxyAddr string
	Secret         string
}

func startLiveProbeRuntime(snapshot ProviderSnapshot) (*mihomoProbeRuntime, error) {
	for _, cfg := range discoverLiveMihomoConfigs() {
		runtime := &mihomoProbeRuntime{
			controllerURL:  cfg.ControllerURL,
			mixedProxyAddr: cfg.MixedProxyAddr,
			secret:         cfg.Secret,
		}
		proxies, err := runtime.listAllProxies()
		if err != nil {
			continue
		}
		availableNodes := map[string]struct{}{}
		for _, proxy := range snapshot.Proxies {
			if _, ok := proxies[proxy.Name]; ok {
				availableNodes[proxy.Name] = struct{}{}
			}
		}
		if len(availableNodes) == 0 {
			continue
		}
		runtime.availableNodes = availableNodes
		runtime.groupName = chooseSelectableGroup(proxies, availableNodes)
		return runtime, nil
	}
	return nil, fmt.Errorf("no live mihomo runtime exposes provider %s nodes", snapshot.Name)
}

func (r *mihomoProbeRuntime) listAllProxies() (map[string]proxyAPIEntry, error) {
	resp, err := r.doRequest(http.MethodGet, r.controllerURL+"/proxies", nil, defaultProbeTimeout)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("list proxies status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Proxies map[string]proxyAPIEntry `json:"proxies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Proxies == nil {
		payload.Proxies = map[string]proxyAPIEntry{}
	}
	return payload.Proxies, nil
}

type proxyAPIEntry struct {
	Type string   `json:"type"`
	All  []string `json:"all"`
}

func chooseSelectableGroup(proxies map[string]proxyAPIEntry, targetNodes map[string]struct{}) string {
	bestName := ""
	bestScore := 0
	for name, entry := range proxies {
		typ := strings.ToLower(strings.TrimSpace(entry.Type))
		if typ != "selector" && typ != "select" {
			continue
		}
		score := 0
		for _, nodeName := range entry.All {
			if _, ok := targetNodes[nodeName]; ok {
				score++
			}
		}
		if score != len(targetNodes) {
			continue
		}
		if score > bestScore {
			bestScore = score
			bestName = name
		}
	}
	return bestName
}

func discoverLiveMihomoConfigs() []liveMihomoConfig {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	candidates := []string{
		filepath.Join(home, "Library", "Application Support", "io.github.clash-verge-rev.clash-verge-rev", "config.yaml"),
		filepath.Join(home, ".config", "mihomo", "config.yaml"),
	}
	configs := []liveMihomoConfig{}
	for _, path := range candidates {
		cfg, err := loadLiveMihomoConfig(path)
		if err == nil {
			configs = append(configs, cfg)
		}
	}
	return configs
}

func loadLiveMihomoConfig(path string) (liveMihomoConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return liveMihomoConfig{}, err
	}
	var payload struct {
		ExternalController string `yaml:"external-controller"`
		Secret             string `yaml:"secret"`
		MixedPort          int    `yaml:"mixed-port"`
		SocksPort          int    `yaml:"socks-port"`
	}
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return liveMihomoConfig{}, err
	}
	controllerURL := strings.TrimSpace(payload.ExternalController)
	if controllerURL == "" {
		return liveMihomoConfig{}, fmt.Errorf("missing external-controller in %s", path)
	}
	if !strings.Contains(controllerURL, "://") {
		controllerURL = "http://" + controllerURL
	}
	port := payload.MixedPort
	if port == 0 {
		port = payload.SocksPort
	}
	if port == 0 {
		return liveMihomoConfig{}, fmt.Errorf("missing mixed-port/socks-port in %s", path)
	}
	return liveMihomoConfig{
		ControllerURL:  strings.TrimRight(controllerURL, "/"),
		MixedProxyAddr: fmt.Sprintf("127.0.0.1:%d", port),
		Secret:         payload.Secret,
	}, nil
}

func startMihomoProbeRuntime(snapshot ProviderSnapshot) (*mihomoProbeRuntime, error) {
	mihomoBin, err := execLookPath("mihomo")
	if err != nil {
		return nil, err
	}
	controllerPort, err := allocateLocalPort()
	if err != nil {
		return nil, err
	}
	mixedPort, err := allocateLocalPort()
	if err != nil {
		return nil, err
	}
	tempDir, err := newMihomoProbeDir()
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(tempDir, "config.yaml")
	providerPath := filepath.Join(tempDir, "provider.yaml")
	if err := os.WriteFile(providerPath, snapshot.Raw, 0o644); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, err
	}
	if err := os.WriteFile(configPath, buildMihomoProbeConfig(snapshot, controllerPort, mixedPort, defaultProbeRuntimeConfig), 0o644); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, err
	}

	cmd := exec.Command(mihomoBin, "-d", tempDir, "-f", configPath)
	cmd.Stdout = nil
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, err
	}

	runtime := &mihomoProbeRuntime{
		command:        cmd,
		dir:            tempDir,
		controllerURL:  fmt.Sprintf("http://127.0.0.1:%d", controllerPort),
		mixedProxyAddr: fmt.Sprintf("127.0.0.1:%d", mixedPort),
		groupName:      defaultProbeRuntimeConfig.GroupName,
		stderrBuffer:   stderr,
	}
	if err := waitForMihomoReady(runtime); err != nil {
		runtime.Close()
		return nil, err
	}
	groupNodes, err := runtime.listGroupNodes()
	if err != nil {
		runtime.Close()
		return nil, err
	}
	runtime.availableNodes = map[string]struct{}{}
	for _, nodeName := range groupNodes {
		runtime.availableNodes[nodeName] = struct{}{}
	}
	return runtime, nil
}

func newMihomoProbeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	baseDir := filepath.Join(home, ".config", "mihomo", "probe-tmp")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(baseDir, "provider-")
}

func buildMihomoProbeConfig(snapshot ProviderSnapshot, controllerPort, mixedPort int, runtimeConfig probeRuntimeConfig) []byte {
	payload := map[string]any{
		"mixed-port":          mixedPort,
		"allow-lan":           runtimeConfig.AllowLAN,
		"mode":                runtimeConfig.Mode,
		"log-level":           runtimeConfig.LogLevel,
		"external-controller": fmt.Sprintf("127.0.0.1:%d", controllerPort),
		"proxy-providers": map[string]any{
			snapshot.Name: map[string]any{
				"type": "file",
				"path": "./provider.yaml",
			},
		},
		"proxy-groups": []any{
			map[string]any{
				"name": runtimeConfig.GroupName,
				"type": "select",
				"use":  []string{snapshot.Name},
			},
		},
		"dns": map[string]any{
			"enable": runtimeConfig.DNSEnable,
		},
		"rules": []string{runtimeConfig.Rule},
	}
	data, err := yaml.Marshal(payload)
	if err != nil {
		return []byte("proxies: []\n")
	}
	return data
}

func waitForMihomoReady(runtime *mihomoProbeRuntime) error {
	deadline := time.Now().Add(8 * time.Second)
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		if err := ensureProcessRunning(runtime.command, runtime.stderrBuffer, "mihomo"); err != nil {
			return err
		}
		resp, err := client.Get(runtime.controllerURL + "/version")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				nodes, listErr := runtime.listGroupNodes()
				if listErr == nil && len(nodes) > 0 && canDialTCP(runtime.mixedProxyAddr, 500*time.Millisecond) {
					return nil
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("mihomo probe runtime not ready: %s", strings.TrimSpace(runtime.stderrBuffer.String()))
}

func canDialTCP(addr string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

type localProxyProcess struct {
	command      *exec.Cmd
	tempFile     string
	stderrBuffer *bytes.Buffer
}

func (p *localProxyProcess) Close() {
	if p == nil {
		return
	}
	if p.command != nil && p.command.Process != nil {
		_ = p.command.Process.Kill()
		_, _ = p.command.Process.Wait()
	}
	if p.tempFile != "" {
		_ = os.Remove(p.tempFile)
	}
}

func startLocalProxy(proxyConfig map[string]any, localPort int) (*localProxyProcess, error) {
	typ, _ := proxyConfig["type"].(string)
	switch strings.ToLower(typ) {
	case "ss", "shadowsocks":
		if proc, err := startSSLocalProxy(proxyConfig, localPort); err == nil {
			return proc, nil
		} else if !isExecutableNotFound(err, "ss-local") && !strings.HasPrefix(err.Error(), "missing plugin binary:") {
			return nil, err
		}
		return startSingBoxProxy(proxyConfig, localPort)
	case "vmess", "vless", "trojan", "hysteria", "hysteria2", "anytls":
		return startSingBoxProxy(proxyConfig, localPort)
	default:
		return nil, fmt.Errorf("unsupported proxy type for probe: %s", typ)
	}
}

func startSSLocalProxy(proxyConfig map[string]any, localPort int) (*localProxyProcess, error) {
	if _, err := execLookPath("ss-local"); err != nil {
		return nil, err
	}
	server, _ := proxyConfig["server"].(string)
	password, _ := proxyConfig["password"].(string)
	cipher, _ := proxyConfig["cipher"].(string)
	port, ok := intValue(proxyConfig["port"])
	if !ok || server == "" || password == "" {
		return nil, fmt.Errorf("incomplete ss proxy config")
	}

	configFile, err := os.CreateTemp("", "ss-local-*.json")
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"server":        server,
		"server_port":   port,
		"password":      password,
		"method":        cipher,
		"local_address": "127.0.0.1",
		"local_port":    localPort,
	}
	if plugin := stringValue(proxyConfig["plugin"]); plugin != "" {
		pluginBinary := resolveSSPluginBinary(plugin)
		if _, err := execLookPath(pluginBinary); err != nil {
			return nil, fmt.Errorf("missing plugin binary: %s", pluginBinary)
		}
		payload["plugin"] = pluginBinary
		if pluginOpts, ok := proxyConfig["plugin-opts"].(map[string]any); ok {
			payload["plugin_opts"] = encodeSSPluginOpts(pluginBinary, pluginOpts)
		}
	}
	if err := json.NewEncoder(configFile).Encode(payload); err != nil {
		configFile.Close()
		return nil, err
	}
	configFile.Close()

	cmd := exec.Command("ss-local", "-c", configFile.Name(), "-l", strconv.Itoa(localPort))
	cmd.Stdout = nil
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if err := ensureProcessRunning(cmd, stderr, "ss-local"); err != nil {
		return nil, err
	}
	if err := waitForLocalPort(localPort, 8*time.Second); err != nil {
		return nil, err
	}
	return &localProxyProcess{command: cmd, tempFile: configFile.Name(), stderrBuffer: stderr}, nil
}

func startSingBoxProxy(proxyConfig map[string]any, localPort int) (*localProxyProcess, error) {
	if _, err := execLookPath("sing-box"); err != nil {
		return nil, err
	}
	outbound, err := buildSingBoxOutbound(proxyConfig)
	if err != nil {
		return nil, err
	}
	configFile, err := os.CreateTemp("", "sing-box-*.json")
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"log": map[string]any{
			"level": "error",
		},
		"inbounds": []any{
			map[string]any{
				"type":        "mixed",
				"tag":         "in",
				"listen":      "127.0.0.1",
				"listen_port": localPort,
			},
		},
		"outbounds": []any{
			outbound,
			map[string]any{"type": "direct", "tag": "direct"},
		},
		"route": map[string]any{
			"final": "out",
			"rules": []any{},
		},
	}
	if err := json.NewEncoder(configFile).Encode(payload); err != nil {
		configFile.Close()
		return nil, err
	}
	configFile.Close()

	cmd := exec.Command("sing-box", "run", "-c", configFile.Name())
	cmd.Stdout = nil
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if err := ensureProcessRunning(cmd, stderr, "sing-box"); err != nil {
		return nil, err
	}
	if err := waitForLocalPort(localPort, 8*time.Second); err != nil {
		return nil, err
	}
	return &localProxyProcess{command: cmd, tempFile: configFile.Name(), stderrBuffer: stderr}, nil
}

func ensureProcessRunning(cmd *exec.Cmd, stderr *bytes.Buffer, processName string) error {
	if cmd == nil || cmd.Process == nil {
		return fmt.Errorf("%s did not start", processName)
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
		return nil
	}
	_ = cmd.Wait()
	message := strings.TrimSpace(stderr.String())
	if message == "" {
		message = processName + " exited immediately"
	}
	return fmt.Errorf("%s", message)
}

func resolveSSPluginBinary(plugin string) string {
	switch strings.ToLower(strings.TrimSpace(plugin)) {
	case "obfs":
		return "obfs-local"
	default:
		return plugin
	}
}

func buildSingBoxOutbound(proxyConfig map[string]any) (map[string]any, error) {
	typ, _ := proxyConfig["type"].(string)
	server, _ := proxyConfig["server"].(string)
	port, ok := intValue(proxyConfig["port"])
	if server == "" || !ok {
		return nil, fmt.Errorf("proxy missing server/port")
	}
	switch strings.ToLower(typ) {
	case "ss", "shadowsocks":
		password, _ := proxyConfig["password"].(string)
		cipher, _ := proxyConfig["cipher"].(string)
		out := map[string]any{
			"type":        "shadowsocks",
			"tag":         "out",
			"server":      server,
			"server_port": port,
			"method":      cipher,
			"password":    password,
		}
		if plugin := stringValue(proxyConfig["plugin"]); plugin != "" {
			pluginBinary := resolveSSPluginBinary(plugin)
			out["plugin"] = pluginBinary
			if pluginOpts, ok := proxyConfig["plugin-opts"].(map[string]any); ok {
				out["plugin_opts"] = encodeSSPluginOpts(pluginBinary, pluginOpts)
			}
		}
		return out, nil
	case "vless":
		out := map[string]any{
			"type":        "vless",
			"tag":         "out",
			"server":      server,
			"server_port": port,
			"uuid":        stringValue(proxyConfig["uuid"]),
		}
		if flow := stringValue(proxyConfig["flow"]); flow != "" {
			out["flow"] = flow
		}
		tlsEnabled := boolValue(proxyConfig["tls"])
		tlsConfig := map[string]any{"enabled": tlsEnabled}
		if serverName := nonEmpty(stringValue(proxyConfig["servername"]), stringValue(proxyConfig["sni"])); serverName != "" {
			tlsConfig["server_name"] = serverName
		}
		if boolValue(proxyConfig["skip-cert-verify"]) {
			tlsConfig["insecure"] = true
		}
		if reality, ok := proxyConfig["reality-opts"].(map[string]any); ok {
			tlsConfig["enabled"] = true
			tlsConfig["reality"] = map[string]any{
				"enabled":    true,
				"public_key": stringValue(reality["public-key"]),
				"short_id":   stringValue(reality["short-id"]),
			}
		}
		out["tls"] = tlsConfig
		if transport := buildSingBoxTransport(proxyConfig); len(transport) > 0 {
			out["transport"] = transport
		}
		return out, nil
	case "vmess":
		out := map[string]any{
			"type":        "vmess",
			"tag":         "out",
			"server":      server,
			"server_port": port,
			"uuid":        stringValue(proxyConfig["uuid"]),
			"alter_id":    intOrZero(proxyConfig["alterId"]),
			"security":    nonEmpty(stringValue(proxyConfig["cipher"]), "auto"),
		}
		if transport := buildSingBoxTransport(proxyConfig); len(transport) > 0 {
			out["transport"] = transport
		}
		return out, nil
	case "trojan":
		return map[string]any{
			"type":        "trojan",
			"tag":         "out",
			"server":      server,
			"server_port": port,
			"password":    stringValue(proxyConfig["password"]),
			"tls": map[string]any{
				"enabled":     true,
				"server_name": nonEmpty(stringValue(proxyConfig["sni"]), server),
				"insecure":    boolValue(proxyConfig["skip-cert-verify"]),
			},
		}, nil
	case "anytls":
		return map[string]any{
			"type":        "anytls",
			"tag":         "out",
			"server":      server,
			"server_port": port,
			"password":    stringValue(proxyConfig["password"]),
			"tls": map[string]any{
				"enabled":     true,
				"server_name": nonEmpty(stringValue(proxyConfig["sni"]), stringValue(proxyConfig["servername"]), server),
				"insecure":    boolValue(proxyConfig["skip-cert-verify"]),
			},
		}, nil
	case "hysteria", "hysteria2":
		out := map[string]any{
			"type":        "hysteria2",
			"tag":         "out",
			"server":      server,
			"server_port": port,
			"password":    stringValue(proxyConfig["password"]),
			"tls": map[string]any{
				"enabled":     true,
				"server_name": nonEmpty(stringValue(proxyConfig["sni"]), server),
				"insecure":    boolValue(proxyConfig["skip-cert-verify"]),
			},
		}
		if obfs := stringValue(proxyConfig["obfs"]); obfs != "" {
			out["obfs"] = map[string]any{
				"type":     obfs,
				"password": stringValue(proxyConfig["obfs-password"]),
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported sing-box outbound type: %s", typ)
	}
}

func buildSingBoxTransport(proxyConfig map[string]any) map[string]any {
	switch strings.ToLower(stringValue(proxyConfig["network"])) {
	case "ws":
		transport := map[string]any{
			"type": "ws",
		}
		if wsOpts, ok := proxyConfig["ws-opts"].(map[string]any); ok {
			if path := stringValue(wsOpts["path"]); path != "" {
				transport["path"] = path
			}
			if headers, ok := wsOpts["headers"].(map[string]any); ok && len(headers) > 0 {
				transport["headers"] = headers
			}
		}
		return transport
	default:
		return nil
	}
}

func selectProviderSpecs(specs map[string]ProxyProviderSpec, selected []string) (map[string]ProxyProviderSpec, error) {
	if len(selected) == 0 {
		return specs, nil
	}
	filtered := make(map[string]ProxyProviderSpec, len(selected))
	for _, name := range sortUniqueStrings(selected) {
		spec, ok := specs[name]
		if !ok {
			return nil, fmt.Errorf("unknown provider for probe: %s", name)
		}
		filtered[name] = spec
	}
	return filtered, nil
}

func isExecutableNotFound(err error, executable string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	message := err.Error()
	return strings.Contains(message, executable) && strings.Contains(message, "executable file not found")
}

func allocateLocalPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func probeHTTPThroughSOCKS5(proxyAddr, target string) error {
	dialer := socks5Dialer{address: proxyAddr, timeout: defaultProbeTimeout}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: defaultProbeTimeout,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   defaultProbeTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200, 204, 301, 302, 307, 308, 401, 403:
		return nil
	default:
		return fmt.Errorf("unexpected http status: %d", resp.StatusCode)
	}
}

func httpRequestViaProxyAddr(proxyAddr, method, requestURL string, headers map[string]string, body io.Reader, timeout time.Duration) (*http.Response, []byte, error) {
	req, err := http.NewRequest(method, requestURL, body)
	if err != nil {
		return nil, nil, err
	}
	for key, value := range headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Mozilla/5.0")
	}

	transport := &http.Transport{
		Proxy:               nil,
		TLSHandshakeTimeout: timeout,
	}
	if strings.TrimSpace(proxyAddr) != "" {
		dialer := socks5Dialer{address: proxyAddr, timeout: timeout}
		transport.DialContext = dialer.DialContext
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		resp.Body.Close()
		return nil, nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	return resp, respBody, nil
}

func waitForLocalPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		if canDialTCP(addr, 200*time.Millisecond) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("local proxy port %d not ready", port)
}

func probeSocketThroughSOCKS5(proxyAddr, host string, port int, expectSSHBanner bool) (int, error) {
	started := time.Now()
	dialer := socks5Dialer{address: proxyAddr, timeout: defaultProbeTimeout}
	conn, err := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if expectSSHBanner {
		if err := conn.SetReadDeadline(time.Now().Add(defaultProbeTimeout)); err != nil {
			return 0, err
		}
		line, err := readProbeLine(conn, 256)
		if err != nil {
			return 0, err
		}
		if !strings.HasPrefix(line, "SSH-") {
			return 0, fmt.Errorf("unexpected ssh banner: %q", line)
		}
	}
	return int(time.Since(started).Milliseconds()), nil
}

func readProbeLine(r io.Reader, maxBytes int) (string, error) {
	buf := make([]byte, 0, 64)
	single := make([]byte, 1)
	for len(buf) < maxBytes {
		if _, err := r.Read(single); err != nil {
			return "", err
		}
		buf = append(buf, single[0])
		if single[0] == '\n' {
			return strings.TrimRight(string(buf), "\r\n"), nil
		}
	}
	return "", fmt.Errorf("probe response exceeded %d bytes", maxBytes)
}

type socks5Dialer struct {
	address string
	timeout time.Duration
}

func (d socks5Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	var netDialer net.Dialer
	netDialer.Timeout = d.timeout
	conn, err := netDialer.DialContext(ctx, "tcp", d.address)
	if err != nil {
		return nil, err
	}

	if err := conn.SetDeadline(time.Now().Add(d.timeout)); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		conn.Close()
		return nil, err
	}
	buf := make([]byte, 2)
	if _, err := ioReadFull(conn, buf); err != nil {
		conn.Close()
		return nil, err
	}
	if buf[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("socks5 auth negotiation failed")
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		conn.Close()
		return nil, err
	}
	portValue, err := strconv.Atoi(port)
	if err != nil {
		conn.Close()
		return nil, err
	}

	req := []byte{0x05, 0x01, 0x00}
	ip := net.ParseIP(host)
	switch {
	case ip != nil && ip.To4() != nil:
		req = append(req, 0x01)
		req = append(req, ip.To4()...)
	case ip != nil:
		req = append(req, 0x04)
		req = append(req, ip.To16()...)
	default:
		req = append(req, 0x03, byte(len(host)))
		req = append(req, host...)
	}
	req = append(req, byte(portValue>>8), byte(portValue))
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}

	head := make([]byte, 4)
	if _, err := ioReadFull(conn, head); err != nil {
		conn.Close()
		return nil, err
	}
	if head[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("socks5 connect failed: %d", head[1])
	}
	addrLen := 0
	switch head[3] {
	case 0x01:
		addrLen = 4
	case 0x04:
		addrLen = 16
	case 0x03:
		size := make([]byte, 1)
		if _, err := ioReadFull(conn, size); err != nil {
			conn.Close()
			return nil, err
		}
		addrLen = int(size[0])
	default:
		conn.Close()
		return nil, fmt.Errorf("unsupported socks5 atyp: %d", head[3])
	}
	if addrLen > 0 {
		skip := make([]byte, addrLen+2)
		if _, err := ioReadFull(conn, skip); err != nil {
			conn.Close()
			return nil, err
		}
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func ioReadFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func resolvePort(parsed *url.URL) int {
	if parsed.Port() != "" {
		if port, err := strconv.Atoi(parsed.Port()); err == nil {
			return port
		}
	}
	if strings.EqualFold(parsed.Scheme, "ssh") {
		return 22
	}
	return 0
}

func intValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		return 0, false
	}
}

func intOrZero(value any) int {
	parsed, _ := intValue(value)
	return parsed
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "true" || typed == "1"
	default:
		return false
	}
}

func encodeSSPluginOpts(pluginBinary string, opts map[string]any) string {
	parts := []string{}
	if strings.EqualFold(pluginBinary, "obfs-local") {
		if mode := stringValue(opts["mode"]); mode != "" {
			parts = append(parts, "obfs="+mode)
		}
		if host := stringValue(opts["host"]); host != "" {
			parts = append(parts, "obfs-host="+host)
		}
	} else {
		if mode := stringValue(opts["mode"]); mode != "" {
			parts = append(parts, "mode="+mode)
		}
		if host := stringValue(opts["host"]); host != "" {
			parts = append(parts, "host="+host)
		}
	}
	keys := make([]string, 0, len(opts))
	for key := range opts {
		if key == "mode" || key == "host" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key+"="+stringValue(opts[key]))
	}
	return strings.Join(parts, ";")
}
