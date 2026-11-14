// Package sim generates seeded synthetic incidents: a service topology, traffic,
// logs and spans, with a fault injected into one service.
//
// The simulator exists so the analysis can be measured. It is the benchmark's
// ground truth: for every scenario it records which service was really broken
// and when, so a ranking can be scored as right or wrong.
//
// Determinism: every random decision comes from one rand.Rand seeded by the
// scenario seed, consumed in a fixed order. No map iteration feeds the generator,
// no wall clock, no floats formatted by the platform.
package sim

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/RocketifyLabs/traceline/internal/model"
)

// Fault types. The first eight are the spec's required set; the ninth is the
// optional slow-burn leak.
const (
	FaultCrashLoop      = "crash_loop"
	FaultLatency        = "latency"
	FaultTimeoutCascade = "timeout_cascade"
	FaultPoolExhaustion = "pool_exhaustion"
	FaultBadDeploy      = "bad_deploy"
	FaultRetryStorm     = "retry_storm"
	FaultNoisyNeighbour = "noisy_neighbour"
	FaultPartition      = "partition"
	FaultResourceLeak   = "resource_leak"
)

// AllFaults is the ordered list used when a seed does not name a fault.
//
// Order matters and is fixed: it is the mapping from seed to fault, so inserting
// a fault in the middle would silently change every past benchmark number. New
// faults are appended.
var AllFaults = []string{
	FaultCrashLoop,
	FaultLatency,
	FaultTimeoutCascade,
	FaultPoolExhaustion,
	FaultBadDeploy,
	FaultRetryStorm,
	FaultNoisyNeighbour,
	FaultPartition,
	FaultResourceLeak,
}

// Scenario is a generated incident plus its ground truth.
type Scenario struct {
	Seed      int64
	Fault     string
	TrueCause string
	// FaultStart is when the injected fault began. The benchmark compares the
	// detected incident start against this.
	FaultStart time.Time
	FaultEnd   time.Time
	Logs       []model.LogLine
	Spans      []model.Span
	Services   []string
}

// Config bounds a generated scenario.
type Config struct {
	// Duration is the total timeline length.
	Duration time.Duration
	// BaselineFraction is the share of the timeline before the fault.
	BaselineFraction float64
	// RequestsPerSecond is the total arrival rate across all services.
	RequestsPerSecond int
	// MaxServices bounds the topology size.
	MaxServices int
	// BackgroundNoise is the per-service probability of an unrelated error log
	// each second, independent of the fault.
	//
	// Without it every service is clean before the fault, which makes the
	// earliest anomaly trivially the cause and flatters the benchmark: a real
	// capture always has some services that were already flaky.
	BackgroundNoise float64
	// NoiseBurst makes a random healthy service produce a short burst of errors
	// somewhere in the baseline, imitating a brief unrelated wobble.
	NoiseBurst bool
}

// DefaultConfig produces a scenario small enough to run 20 of them in a test.
func DefaultConfig() Config {
	return Config{
		Duration:          6 * time.Minute,
		BaselineFraction:  0.55,
		RequestsPerSecond: 6,
		MaxServices:       6,
		BackgroundNoise:   0.06,
		NoiseBurst:        true,
	}
}

// EasyConfig disables the realistic noise, for tests that need a clean signal.
func EasyConfig() Config {
	return Config{
		Duration:          6 * time.Minute,
		BaselineFraction:  0.55,
		RequestsPerSecond: 6,
		MaxServices:       6,
		BackgroundNoise:   0,
		NoiseBurst:        false,
	}
}

// Generate builds one scenario.
//
// The same seed and fault always produce the same scenario, byte for byte.
func Generate(seed int64, fault string, cfg Config) Scenario {
	if cfg.Duration <= 0 {
		cfg = DefaultConfig()
	}
	if fault == "" {
		// Seed-derived, not random: the mapping must be reproducible.
		fault = AllFaults[abs(seed)%int64(len(AllFaults))]
	}

	rng := rand.New(rand.NewSource(seed))
	topology := buildTopology(rng, cfg.MaxServices)
	start := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)

	sc := Scenario{
		Seed:     seed,
		Fault:    fault,
		Services: topology.services,
	}
	total := cfg.Duration
	faultStart := start.Add(time.Duration(cfg.BaselineFraction * float64(total)))
	faultEnd := start.Add(total)
	sc.FaultStart = faultStart
	sc.FaultEnd = faultEnd

	// Pick the fault target. Deterministic: the rng sequence is fixed.
	target := topology.services[rng.Intn(len(topology.services))]

	// The noisy-neighbour scenario needs TWO roles, and getting them confused
	// made the scenario unsolvable:
	//
	//   target  - the service that actually breaks (gets the fault profile)
	//   noisy   - a different service that is loud in the logs but healthy
	//
	// An earlier version treated the LOUD service as the target and picked the
	// "true cause" from the leftovers, so the true cause received no fault at
	// all and only inherited collateral damage. The scenario then had no correct
	// answer, and the analysis was scored against a wrong label.
	realCause := target
	noisy := ""
	if fault == FaultNoisyNeighbour {
		noisy = topology.services[rng.Intn(len(topology.services))]
		if noisy == target {
			noisy = topology.services[(indexOf(topology.services, target)+1)%len(topology.services)]
		}
	}
	sc.TrueCause = realCause

	prof := profileFor(fault)
	gen := generator{
		rng:        rng,
		cfg:        cfg,
		topology:   topology,
		start:      start,
		faultStart: faultStart,
		faultEnd:   faultEnd,
		fault:      fault,
		target:     target,
		noisy:      noisy,
		realCause:  realCause,
		prof:       prof,
	}
	gen.hops = gen.hopsToTarget()

	// A short unrelated error burst somewhere in the baseline. Deterministic
	// from the same rng, so the scenario stays reproducible.
	if cfg.NoiseBurst && len(topology.services) > 1 {
		burstService := topology.services[rng.Intn(len(topology.services))]
		if burstService == target {
			burstService = topology.services[(indexOf(topology.services, target)+1)%len(topology.services)]
		}
		burstAt := start.Add(time.Duration(float64(faultStart.Sub(start)) * (0.3 + 0.4*rng.Float64())))
		for i := 0; i < 12; i++ {
			sc.Logs = append(sc.Logs, model.LogLine{
				ID:      "burst-" + itoa(i),
				TS:      burstAt.Add(time.Duration(i) * 200 * time.Millisecond),
				Service: burstService,
				Level:   model.LevelError,
				Msg:     "transient upstream reset",
			})
		}
	}

	gen.run(&sc)

	// Sorted output: the analysis must not depend on generation order, and a
	// golden file needs a stable sequence.
	sort.SliceStable(sc.Logs, func(i, j int) bool {
		if !sc.Logs[i].TS.Equal(sc.Logs[j].TS) {
			return sc.Logs[i].TS.Before(sc.Logs[j].TS)
		}
		if sc.Logs[i].Service != sc.Logs[j].Service {
			return sc.Logs[i].Service < sc.Logs[j].Service
		}
		return sc.Logs[i].ID < sc.Logs[j].ID
	})
	sort.SliceStable(sc.Spans, func(i, j int) bool {
		if !sc.Spans[i].Start.Equal(sc.Spans[j].Start) {
			return sc.Spans[i].Start.Before(sc.Spans[j].Start)
		}
		return sc.Spans[i].SpanID < sc.Spans[j].SpanID
	})
	return sc
}

// profile is how a fault changes behaviour.
type profile struct {
	// errRate and latency multipliers applied to the target service.
	errorBase   float64
	errorFault  float64
	latencyBase time.Duration
	latencyMult float64
	// crash makes the service vanish and emit restart lines.
	crash bool
	// deployEvent emits a deploy line right before the fault.
	deployEvent bool
	// retryAmplifies multiplies call volume on the target after the fault.
	retryAmplifies bool
	// partitionFails silently drops a share of calls.
	partitionFails float64
	// leakRamps makes latency grow linearly through the fault window.
	leakRamps bool
}

func profileFor(fault string) profile {
	base := profile{
		errorBase:   0.01,
		errorFault:  0.02,
		latencyBase: 40 * time.Millisecond,
		latencyMult: 1.0,
	}
	switch fault {
	case FaultCrashLoop:
		base.errorFault = 0.95
		base.crash = true
	case FaultLatency:
		base.errorFault = 0.05
		base.latencyMult = 8.0
	case FaultTimeoutCascade:
		base.errorFault = 0.55
		base.latencyMult = 5.0
	case FaultPoolExhaustion:
		base.errorFault = 0.70
		base.latencyMult = 3.0
	case FaultBadDeploy:
		base.errorFault = 0.45
		base.deployEvent = true
	case FaultRetryStorm:
		base.errorFault = 0.35
		base.retryAmplifies = true
		base.latencyMult = 2.0
	case FaultNoisyNeighbour:
		// A distractor scenario needs a REAL fault plus a loud unrelated one.
		// Giving the distractor no behavioural effect made the scenario
		// unsolvable: the "true cause" produced no error-rate or latency change
		// at all, so no correct answer existed. The real fault is a latency
		// injection; the noise is added separately, on a different service.
		base.errorFault = 0.30
		base.latencyMult = 4.0
	case FaultPartition:
		base.errorFault = 0.40
		base.partitionFails = 0.5
		base.latencyMult = 2.5
	case FaultResourceLeak:
		base.errorFault = 0.25
		base.latencyMult = 1.5
		base.leakRamps = true
	}
	return base
}

type topology struct {
	services []string
	// edges is a fixed call graph: index i calls all indices in edges[i].
	edges [][]int
}

// buildTopology creates a small layered call graph, deterministically.
//
// A backbone i → i+1 is always created before any random extras. Without it the
// graph can be disconnected, and since the walk starts at the entry service, a
// service that nothing reaches emits no telemetry at all — it then can never be
// the true cause of anything, because it does not exist in the data.
func buildTopology(rng *rand.Rand, max int) topology {
	if max < 3 {
		max = 3
	}
	n := 3 + rng.Intn(max-2) // 3..max
	names := make([]string, n)
	// A fixed name pool with a stable suffix, so tests can reason about names.
	pool := []string{"edge", "auth", "orders", "payments", "dbproxy", "cache", "search", "notify"}
	for i := 0; i < n; i++ {
		names[i] = pool[i%len(pool)]
		if i >= len(pool) {
			names[i] = fmt.Sprintf("%s%d", pool[i%len(pool)], i)
		}
	}

	// Backbone: every service is reachable from the entry service.
	edges := make([][]int, n)
	for i := 0; i < n-1; i++ {
		edges[i] = append(edges[i], i+1)
	}
	// Random extras make the graph more interesting than a bare chain.
	for i := 0; i < n; i++ {
		for j := i + 2; j < n; j++ {
			if rng.Float64() < 0.4 {
				edges[i] = append(edges[i], j)
			}
		}
	}
	return topology{services: names, edges: edges}
}

type generator struct {
	rng        *rand.Rand
	cfg        Config
	topology   topology
	start      time.Time
	faultStart time.Time
	faultEnd   time.Time
	fault      string
	target     string
	noisy      string
	realCause  string
	prof       profile
	seq        int
	hops       []int
}

func (g *generator) run(sc *Scenario) {
	step := time.Second / time.Duration(g.cfg.RequestsPerSecond)
	if step <= 0 {
		step = time.Millisecond
	}

	// Deploy event immediately before the fault, for the bad-deploy scenario.
	if g.prof.deployEvent {
		sc.Logs = append(sc.Logs, model.LogLine{
			ID: g.nextID("deploy"), TS: g.faultStart.Add(-2 * time.Second),
			Service: g.target, Level: model.LevelInfo,
			Msg:    "deploy started version v" + itoa(int(g.rng.Intn(90)+10)),
			Fields: map[string]any{"event": "deploy", "version": "v" + itoa(int(g.rng.Intn(90)+10))},
		})
	}

	for at := g.start; at.Before(g.start.Add(g.cfg.Duration)); at = at.Add(step) {
		inFault := !at.Before(g.faultStart) && at.Before(g.faultEnd)

		// One root request per tick, walking the call graph.
		g.emitTrace(sc, at, inFault)

		// Background noise: unrelated errors on healthy services, spread evenly
		// through the WHOLE timeline, incident window included.
		//
		// Applying it only before the fault barely changes anything: a detector
		// that compares baseline against incident sees the noise on both sides
		// and cancels it. Noise during the window is what actually competes with
		// the signal, and it is what a real capture looks like.
		if g.cfg.BackgroundNoise > 0 {
			for i, svc := range g.topology.services {
				if svc == g.target {
					continue
				}
				// One draw per service per tick, in a fixed order, so the rng
				// sequence does not depend on map iteration.
				roll := g.rng.Float64()
				if roll < g.cfg.BackgroundNoise {
					sc.Logs = append(sc.Logs, g.log(svc, at, model.LevelError,
						"unrelated transient failure "+itoa(i)))
				}
			}
		}

		// The noisy neighbour is loud for the whole fault window. It emits WARN
		// lines only: a genuine distractor is alarming in the logs without
		// actually failing. Giving it errors too made it indistinguishable from
		// the real cause, which is unfair to any ranking method rather than a
		// test of discrimination.
		if g.noisy != "" && inFault {
			sc.Logs = append(sc.Logs, g.log(g.noisy, at, model.LevelWarn,
				"elevated queue depth "+itoa(g.rng.Intn(9000)+1000)))
			sc.Logs = append(sc.Logs, g.log(g.noisy, at, model.LevelWarn,
				"slow query detected, retrying"))
		}
	}
}

// emitTrace walks the call graph from the entry service, emitting a span per hop
// and a log per service.
func (g *generator) emitTrace(sc *Scenario, at time.Time, inFault bool) {
	traceID := g.nextID("trace")
	// Volume multiplier for the retry-storm fault.
	reps := 1
	if inFault && g.prof.retryAmplifies {
		reps = 3
	}
	for r := 0; r < reps; r++ {
		g.walk(sc, at.Add(time.Duration(r)*time.Millisecond), traceID, 0, "", inFault, 0)
	}
}

func (g *generator) walk(sc *Scenario, at time.Time, traceID string, idx int, parent string, inFault bool, depth int) {
	if idx >= len(g.topology.services) {
		return
	}
	svc := g.topology.services[idx]

	// Every service in the chain emits: the dependency graph is only meaningful
	// if callers produce spans too. Only the target gets the injected fault.
	isTarget := svc == g.target
	errRate := g.prof.errorBase
	latency := g.prof.latencyBase

	// Symptoms lag the cause by one hop per level of distance FROM THE TARGET.
	// Using the walk depth instead would make the entry service the earliest
	// symptom, and therefore the top-ranked "cause".
	hops := 0
	if idx < len(g.hops) && g.hops[idx] > 0 {
		hops = g.hops[idx]
	}
	symptomFrom := g.faultStart.Add(g.propagationDelay(hops))

	if inFault && !at.Before(symptomFrom) {
		switch {
		case isTarget:
			errRate = g.prof.errorFault
			latency = time.Duration(float64(g.prof.latencyBase) * g.prof.latencyMult)
			if g.prof.leakRamps {
				progress := at.Sub(g.faultStart)
				if progress < 0 {
					progress = 0
				}
				frac := float64(progress) / float64(g.faultEnd.Sub(g.faultStart))
				latency = time.Duration(float64(latency) * (1 + 3*frac))
			}
		default:
			// A caller inherits a share of its callee's trouble: this is what
			// makes the cascade visible in the caller's own telemetry, and it is
			// why a ranking by "first anomaly" is not trivially correct.
			if g.callsFailingTarget(idx) {
				errRate = g.prof.errorBase + (g.prof.errorFault-g.prof.errorBase)*0.35
				latency = time.Duration(float64(g.prof.latencyBase) * (1 + (g.prof.latencyMult-1)*0.3))
			}
		}
	}

	spanID := g.nextID("span")
	status := model.StatusOK
	if g.rng.Float64() < errRate {
		status = model.StatusError
	}
	if inFault && isTarget && g.prof.partitionFails > 0 && g.rng.Float64() < g.prof.partitionFails {
		status = model.StatusError
	}

	latency = g.jitter(latency)
	start := at
	end := start.Add(latency)
	sc.Spans = append(sc.Spans, model.Span{
		ID: spanID, TraceID: traceID, SpanID: spanID, ParentSpanID: parent,
		Service: svc, Name: "call " + svc, Start: start, End: end, Status: status,
	})

	level := model.LevelInfo
	msg := "request handled"
	if status == model.StatusError {
		level = model.LevelError
		msg = "request failed: " + g.errorPhrase()
	}
	// Only a fraction of successful requests log, so logs are not a mirror of
	// spans. An error always logs.
	if status == model.StatusError || g.rng.Float64() < 0.25 {
		sc.Logs = append(sc.Logs, model.LogLine{
			ID: g.nextID("log"), TS: start, Service: svc, Level: level,
			Msg: msg, TraceID: traceID,
		})
	}

	if inFault && isTarget && g.prof.crash {
		sc.Logs = append(sc.Logs, g.log(svc, start, model.LevelError, "process exited with signal 9"))
		sc.Logs = append(sc.Logs, g.log(svc, start.Add(time.Millisecond), model.LevelWarn, "restarting"))
	}

	for _, next := range g.topology.edges[idx] {
		g.walk(sc, start, traceID, next, spanID, inFault, depth+1)
	}
}

// jitter applies a deterministic multiplicative spread to an expected latency.
//
// Without jitter every span of a service has the identical duration, MAD
// collapses to zero, and any change scores an astronomical z. Real telemetry
// always has spread; the simulator must too, or the detectors are measured
// against an input no real system could produce.
func (g *generator) jitter(d time.Duration) time.Duration {
	// ±30%, uniform, from the seeded rng in a fixed draw order.
	factor := 0.7 + 0.6*g.rng.Float64()
	return time.Duration(float64(d) * factor)
}

// propagationDelay is how long after the cause a service shows symptoms.
//
// hops is the distance from the FAULT TARGET, not from the entry service. The
// target fails at faultStart; its direct callers notice a moment later, and so
// on upstream. Getting this direction wrong makes the entry service look like
// the earliest symptom and therefore the cause — which is exactly backwards.
//
// The delay is sub-second on purpose. With a one-second delay and a request
// every ~170ms, every service's first error lands in the same bin and the onset
// factor becomes a four-way tie, so "not explained by a dependency" can never
// distinguish a cause from its victim.
func (g *generator) propagationDelay(hops int) time.Duration {
	if hops <= 0 {
		return 0
	}
	return time.Duration(hops) * 400 * time.Millisecond
}

// hopsToTarget computes each service's graph distance from the fault target.
//
// The walk is backwards along edges (a caller is one hop from its callee), so
// the target is 0 and every service that (transitively) calls it is > 0.
func (g *generator) hopsToTarget() []int {
	n := len(g.topology.services)
	hops := make([]int, n)
	for i := range hops {
		hops[i] = -1
	}
	targetIdx := -1
	for i, name := range g.topology.services {
		if name == g.target {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return hops
	}
	hops[targetIdx] = 0

	// Reverse adjacency: for each node, who calls it.
	callers := make([][]int, n)
	for from, tos := range g.topology.edges {
		for _, to := range tos {
			callers[to] = append(callers[to], from)
		}
	}
	queue := []int{targetIdx}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, caller := range callers[cur] {
			if hops[caller] == -1 {
				hops[caller] = hops[cur] + 1
				queue = append(queue, caller)
			}
		}
	}
	return hops
}
func (g *generator) callsFailingTarget(idx int) bool {
	if idx >= len(g.topology.edges) {
		return false
	}
	for _, next := range g.topology.edges[idx] {
		if g.topology.services[next] == g.target {
			return true
		}
		if g.callsFailingTarget(next) {
			return true
		}
	}
	return false
}

func (g *generator) errorPhrase() string {
	phrases := []string{
		"connection pool exhausted",
		"upstream timeout",
		"connection refused",
		"read deadline exceeded",
		"too many retries",
	}
	return phrases[g.rng.Intn(len(phrases))]
}

func (g *generator) log(svc string, at time.Time, level model.Level, msg string) model.LogLine {
	return model.LogLine{ID: g.nextID("log"), TS: at, Service: svc, Level: level, Msg: msg}
}

// nextID produces a deterministic id. The sequence number is part of the id so
// that identical messages in the same millisecond stay distinct, and it is
// generated in a fixed order so the sequence itself is reproducible.
func (g *generator) nextID(kind string) string {
	g.seq++
	return fmt.Sprintf("%s-%06d", kind, g.seq)
}

func indexOf(xs []string, v string) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// String renders a scenario's ground truth for logs and test output.
func (s Scenario) String() string {
	return strings.Join([]string{
		"seed=" + itoa(int(s.Seed)),
		"fault=" + s.Fault,
		"true_cause=" + s.TrueCause,
		"fault_start=" + s.FaultStart.Format(time.RFC3339),
		"logs=" + itoa(len(s.Logs)),
		"spans=" + itoa(len(s.Spans)),
	}, " ")
}
